package exec

import (
	"crypto/md5"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

type Executor struct{}

type MountNamespace struct {
	source string
	target string
	fstype string
	flags  uintptr
}

var MountNamespaces = []MountNamespace{
	{source: "none", target: "/proc", fstype: "proc", flags: 0},
	{source: "none", target: "/sys", fstype: "sysfs", flags: 0},
	{source: "/dev", target: "/dev", fstype: "bind", flags: 0},
	{source: "none", target: "/dev/pts", fstype: "devpts", flags: 0},
	{source: "tmpfs", target: "/tmp", fstype: "tmpfs", flags: 0},
}

func New() *Executor {
	return &Executor{}
}

func (e *Executor) Execute(mountPoint string, command string, args []string, interactive, tty bool) error {
	if _, err := os.Stat(mountPoint); err != nil {
		return fmt.Errorf("mount point not found: %w", err)
	}

	if err := e.setupMountNamespace(mountPoint); err != nil {
		return fmt.Errorf("failed to setup mount namespace: %w", err)
	}

	// Backup and setup resolv.conf
	if err := e.backupAndSetupResolvConf(mountPoint); err != nil {
		fmt.Printf("Warning: failed to setup resolv.conf: %v\n", err)
	}

	// Ensure cleanup happens even if command fails
	defer func() {
		e.restoreResolvConf(mountPoint)
	}()

	chrootCmd := exec.Command("chroot", append([]string{mountPoint, command}, args...)...)

	if interactive {
		chrootCmd.Stdin = os.Stdin
	}

	if tty {
		chrootCmd.Stdout = os.Stdout
		chrootCmd.Stderr = os.Stderr
	}

	return chrootCmd.Run()
}

func (e *Executor) setupMountNamespace(mountPoint string) error {
	for _, m := range MountNamespaces {
		target := mountPoint + m.target
		if err := os.MkdirAll(target, 0755); err != nil {
			continue
		}

		var cmd *exec.Cmd
		if m.fstype == "bind" {
			cmd = exec.Command("mount", "-o", "bind", m.source, target)
		} else {
			cmd = exec.Command("mount", "-t", m.fstype, m.source, target)
		}

		// fmt.Println(strings.Join(cmd.Args, " "))
		cmd.Run()
	}

	return nil
}

func (e *Executor) getBackupPath(mountPoint string) string {
	// Create a unique backup filename based on mount point hash
	hash := md5.Sum([]byte(mountPoint))
	backupName := fmt.Sprintf("resolv_conf_backup_%x", hash[:8])
	return filepath.Join("/tmp/qimi/files", backupName)
}

func (e *Executor) backupAndSetupResolvConf(mountPoint string) error {
	target := mountPoint + "/etc/resolv.conf"
	etcDir := mountPoint + "/etc"
	backupPath := e.getBackupPath(mountPoint)

	// Ensure backup directory exists
	if err := os.MkdirAll("/tmp/qimi/files", 0755); err != nil {
		return err
	}

	// Check if /etc directory exists
	if _, err := os.Stat(etcDir); os.IsNotExist(err) {
		// If /etc doesn't exist, we can't set up resolv.conf
		return nil
	}

	// Only backup if we haven't already (first time for this mount point)
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		// Check if target exists
		if _, err := os.Stat(target); err == nil {
			// Backup existing resolv.conf
			if data, err := os.ReadFile(target); err == nil {
				if err := os.WriteFile(backupPath, data, 0644); err != nil {
					return err
				}
			}
		} else if os.IsNotExist(err) {
			// Create empty backup file to indicate there was no original
			if err := os.WriteFile(backupPath, []byte{}, 0644); err != nil {
				return err
			}
		} else {
			// Some other error (permission denied, etc.)
			return err
		}
	}

	// Read host resolv.conf
	hostResolv, err := os.ReadFile("/etc/resolv.conf")
	if err != nil {
		return err
	}

	// Write host resolv.conf to chroot
	return os.WriteFile(target, hostResolv, 0644)
}

func (e *Executor) restoreResolvConf(mountPoint string) error {
	target := mountPoint + "/etc/resolv.conf"
	backupPath := e.getBackupPath(mountPoint)

	// Read backup
	backup, err := os.ReadFile(backupPath)
	if err != nil {
		// If no backup exists, just remove the current file
		os.Remove(target)
		return nil
	}

	if len(backup) == 0 {
		// Empty backup means there was no original file
		os.Remove(target)
		return nil
	}

	// Restore original resolv.conf
	return os.WriteFile(target, backup, 0644)
}

func (e *Executor) CleanupMountNamespace(mountPoint string) error {
	for i := len(MountNamespaces) - 1; i >= 0; i-- {
		target := mountPoint + MountNamespaces[i].target
		cmd := exec.Command("umount", target)
		err := cmd.Run()
		if err != nil {
			// try lazy mode
			cmd = exec.Command("umount", "-l", target)
			err = cmd.Run()
		}
	}

	return nil
}

// CleanupBackupFiles removes backup files for a mount point
func (e *Executor) CleanupBackupFiles(mountPoint string) error {
	backupPath := e.getBackupPath(mountPoint)
	return os.Remove(backupPath)
}
