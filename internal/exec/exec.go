package exec

import (
	"crypto/md5"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func (e *Executor) Execute(mountPoint string, command string, args []string, interactive, tty bool, nameservers []string) error {
	if _, err := os.Stat(mountPoint); err != nil {
		return fmt.Errorf("mount point not found: %w", err)
	}

	if err := e.setupMountNamespace(mountPoint); err != nil {
		return fmt.Errorf("failed to setup mount namespace: %w", err)
	}

	// Backup and setup resolv.conf
	if err := e.backupAndSetupResolvConf(mountPoint, nameservers); err != nil {
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

	chrootCmd.Stdout = os.Stdout
	chrootCmd.Stderr = os.Stderr

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

func (e *Executor) getBackupSymlinkPath(mountPoint string) string {
	// Create a unique backup filename for symlink info
	hash := md5.Sum([]byte(mountPoint))
	backupName := fmt.Sprintf("resolv_conf_symlink_%x", hash[:8])
	return filepath.Join("/tmp/qimi/files", backupName)
}

func (e *Executor) backupAndSetupResolvConf(mountPoint string, nameservers []string) error {
	target := mountPoint + "/etc/resolv.conf"
	etcDir := mountPoint + "/etc"
	backupPath := e.getBackupPath(mountPoint)
	symlinkBackupPath := e.getBackupSymlinkPath(mountPoint)

	// Ensure backup directory exists
	if err := os.MkdirAll("/tmp/qimi/files", 0755); err != nil {
		return err
	}

	// Check if /etc directory exists
	if _, err := os.Stat(etcDir); os.IsNotExist(err) {
		// If /etc doesn't exist, create it
		if err := os.MkdirAll(etcDir, 0755); err != nil {
			return fmt.Errorf("failed to create /etc directory: %w", err)
		}
	}

	// Only backup if we haven't already (first time for this mount point)
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		// Check if target exists
		if info, err := os.Lstat(target); err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				// It's a symlink, backup where it was pointing
				symlinkTarget, err := os.Readlink(target)
				if err != nil {
					return err
				}
				if err := os.WriteFile(symlinkBackupPath, []byte(symlinkTarget), 0644); err != nil {
					return err
				}
			} else {
				// Regular file, backup the contents
				if data, err := os.ReadFile(target); err == nil {
					if err := os.WriteFile(backupPath, data, 0644); err != nil {
						return err
					}
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

	var resolvContent []byte
	
	if len(nameservers) > 0 {
		// Validate nameservers and create custom resolv.conf
		var validNameservers []string
		for _, ns := range nameservers {
			if ip := net.ParseIP(ns); ip != nil {
				validNameservers = append(validNameservers, ns)
			} else {
				return fmt.Errorf("invalid nameserver IP address: %s", ns)
			}
		}
		
		// Create custom resolv.conf content
		var resolvLines []string
		for _, ns := range validNameservers {
			resolvLines = append(resolvLines, fmt.Sprintf("nameserver %s", ns))
		}
		resolvContent = []byte(strings.Join(resolvLines, "\n") + "\n")
	} else {
		// Read host resolv.conf, following symlinks
		var err error
		realPath, err := filepath.EvalSymlinks("/etc/resolv.conf")
		if err != nil {
			// Fallback to direct read if symlink resolution fails
			resolvContent, err = os.ReadFile("/etc/resolv.conf")
		} else {
			resolvContent, err = os.ReadFile(realPath)
		}
		if err != nil {
			return err
		}
	}

	// Write resolv.conf to chroot
	return os.WriteFile(target, resolvContent, 0644)
}

func (e *Executor) restoreResolvConf(mountPoint string) error {
	target := mountPoint + "/etc/resolv.conf"
	backupPath := e.getBackupPath(mountPoint)
	symlinkBackupPath := e.getBackupSymlinkPath(mountPoint)

	// Check if there was a symlink backup
	if symlinkTarget, err := os.ReadFile(symlinkBackupPath); err == nil && len(symlinkTarget) > 0 {
		// Remove current file and recreate symlink
		os.Remove(target)
		return os.Symlink(string(symlinkTarget), target)
	}

	// Read regular backup
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
	// Validate mountPoint to prevent accidentally unmounting host directories
	if mountPoint == "" || mountPoint == "/" || mountPoint == "/dev" || mountPoint == "/proc" || mountPoint == "/sys" {
		return fmt.Errorf("invalid mount point for cleanup: %s. THIS PROBABLY IS A BUG!!", mountPoint)
	}
	
	// Additional safety check - mount point should be an absolute path under /tmp or similar
	if !strings.HasPrefix(mountPoint, "/tmp/") && !strings.HasPrefix(mountPoint, "/mnt/") {
		return fmt.Errorf("unsafe mount point for cleanup: %s. THIS PROBABLY IS A BUG!!", mountPoint)
	}
	
	// Ensure mountPoint ends with proper path separator for safe concatenation
	if !strings.HasSuffix(mountPoint, "/") {
		mountPoint = mountPoint + "/"
	}

	for i := len(MountNamespaces) - 1; i >= 0; i-- {
		target := mountPoint + strings.TrimPrefix(MountNamespaces[i].target, "/")
		
		// Double-check that target is within the mount point to prevent host unmounting
		if !strings.HasPrefix(target, mountPoint) {
			fmt.Printf("Warning: skipping unsafe unmount target: %s. THIS PROBABLY IS A BUG!!\n", target)
			continue
		}
		
		// Check if target is actually mounted before attempting unmount
		if !e.isMounted(target) {
			continue
		}
		
		cmd := exec.Command("umount", target)
		err := cmd.Run()
		if err != nil {
			// try lazy mode
			cmd = exec.Command("umount", "-l", target)
			err = cmd.Run()
			if err != nil {
				fmt.Printf("Warning: failed to unmount %s: %v\n", target, err)
			}
		}
	}

	return nil
}

// isMounted checks if a path is currently mounted by reading /proc/mounts
func (e *Executor) isMounted(path string) bool {
	mounts, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return false
	}
	
	lines := strings.Split(string(mounts), "\n")
	for _, line := range lines {
		if strings.Contains(line, " "+path+" ") {
			return true
		}
	}
	return false
}

// CleanupBackupFiles removes backup files for a mount point
func (e *Executor) CleanupBackupFiles(mountPoint string) error {
	backupPath := e.getBackupPath(mountPoint)
	symlinkBackupPath := e.getBackupSymlinkPath(mountPoint)
	os.Remove(backupPath)
	os.Remove(symlinkBackupPath)
	return nil
}
