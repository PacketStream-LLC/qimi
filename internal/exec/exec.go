package exec

import (
	"crypto/md5"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/packetstream-llc/qimi/internal/logger"
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
	{source: "/dev", target: "/dev", fstype: "rbind", flags: 0},
	{source: "tmpfs", target: "/tmp", fstype: "tmpfs", flags: 0},
}

func New() *Executor {
	return &Executor{}
}

func (e *Executor) Execute(mountPoint string, command string, args []string, interactive, tty bool, nameservers []string) error {
	logger.Debug("starting execution: command=%s, args=%v, interactive=%t, tty=%t", command, args, interactive, tty)
	logger.Debug("mount point: %s", mountPoint)
	logger.Debug("nameservers: %v", nameservers)

	if _, err := os.Stat(mountPoint); err != nil {
		logger.Error("mount point validation failed: %s", mountPoint)
		return fmt.Errorf("mount point not found: %w", err)
	}
	logger.Debug("mount point validation successful")

	logger.Debug("setting up mount namespace")
	if err := e.setupMountNamespace(mountPoint); err != nil {
		logger.Error("mount namespace setup failed: %v", err)
		return fmt.Errorf("failed to setup mount namespace: %w", err)
	}
	logger.Debug("mount namespace setup completed")

	// Backup and setup resolv.conf
	logger.Debug("setting up resolv.conf")
	if err := e.backupAndSetupResolvConf(mountPoint, nameservers); err != nil {
		logger.Warn("failed to setup resolv.conf: %v", err)
	} else {
		logger.Debug("resolv.conf setup completed")
	}

	// Ensure cleanup happens even if command fails
	defer func() {
		logger.Debug("restoring resolv.conf")
		e.restoreResolvConf(mountPoint)
	}()

	fullCmd := append([]string{mountPoint, command}, args...)
	logger.Debug("executing command in chroot: chroot %s", strings.Join(fullCmd, " "))
	chrootCmd := exec.Command("chroot", fullCmd...)

	if interactive {
		logger.Debug("enabling interactive mode (stdin)")
		chrootCmd.Stdin = os.Stdin
	}

	logger.Debug("redirecting stdout/stderr")
	chrootCmd.Stdout = os.Stdout
	chrootCmd.Stderr = os.Stderr

	logger.Debug("starting command execution")
	err := chrootCmd.Run()
	if err != nil {
		logger.Error("command execution failed: %v", err)
	} else {
		logger.Debug("command execution completed successfully")
	}
	return err
}

func (e *Executor) setupMountNamespace(mountPoint string) error {
	logger.Debug("setting up mount namespaces for %d filesystems", len(MountNamespaces))
	
	for i, m := range MountNamespaces {
		target := mountPoint + m.target
		logger.Debug("mount %d/%d: preparing %s -> %s (type: %s)", i+1, len(MountNamespaces), m.source, target, m.fstype)
		
		if err := os.MkdirAll(target, 0755); err != nil {
			logger.Debug("failed to create directory %s: %v, skipping", target, err)
			continue
		}

		var cmd *exec.Cmd
		if m.fstype == "bind" {
			cmd = exec.Command("mount", "--bind", m.source, target)
			logger.Debug("executing bind mount: %s", strings.Join(cmd.Args, " "))
		} else if m.fstype == "rbind" {
			cmd = exec.Command("mount", "--rbind", m.source, target)
			logger.Debug("executing rbind mount: %s", strings.Join(cmd.Args, " "))
		} else {
			cmd = exec.Command("mount", "-t", m.fstype, m.source, target)
			logger.Debug("executing filesystem mount: %s", strings.Join(cmd.Args, " "))
		}

		if err := cmd.Run(); err != nil {
			logger.Debug("mount command failed: %v", err)
		} else {
			logger.Debug("mount successful: %s", target)
		}
	}

	logger.Debug("mount namespace setup completed")
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

	logger.Debug("resolv.conf setup: target=%s", target)
	logger.Debug("backup paths: content=%s, symlink=%s", backupPath, symlinkBackupPath)

	// Ensure backup directory exists
	logger.Debug("creating backup directory: /tmp/qimi/files")
	if err := os.MkdirAll("/tmp/qimi/files", 0755); err != nil {
		logger.Error("failed to create backup directory: %v", err)
		return err
	}

	// Check if /etc directory exists
	logger.Debug("checking /etc directory: %s", etcDir)
	if _, err := os.Stat(etcDir); os.IsNotExist(err) {
		logger.Debug("/etc directory doesn't exist, creating it")
		if err := os.MkdirAll(etcDir, 0755); err != nil {
			logger.Error("failed to create /etc directory: %v", err)
			return fmt.Errorf("failed to create /etc directory: %w", err)
		}
	} else {
		logger.Debug("/etc directory exists")
	}

	// Only backup if we haven't already (first time for this mount point)
	logger.Debug("checking if backup already exists: %s", backupPath)
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		logger.Debug("no existing backup, creating new backup")

		// Check if target exists
		if info, err := os.Lstat(target); err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				// It's a symlink, backup where it was pointing
				symlinkTarget, err := os.Readlink(target)
				if err != nil {
					logger.Error("failed to read symlink target: %v", err)
					return err
				}
				logger.Debug("backing up symlink target: %s -> %s", target, symlinkTarget)
				if err := os.WriteFile(symlinkBackupPath, []byte(symlinkTarget), 0644); err != nil {
					logger.Error("failed to write symlink backup: %v", err)
					return err
				}
				logger.Debug("symlink backup created successfully")
			} else {
				// Regular file, backup the contents
				logger.Debug("backing up regular file contents: %s", target)
				if data, err := os.ReadFile(target); err == nil {
					if err := os.WriteFile(backupPath, data, 0644); err != nil {
						logger.Error("failed to write content backup: %v", err)
						return err
					}
					logger.Debug("content backup created successfully (%d bytes)", len(data))
				} else {
					logger.Warn("failed to read file for backup: %v", err)
				}
			}
		} else if os.IsNotExist(err) {
			// Create empty backup file to indicate there was no original
			logger.Debug("original file doesn't exist, creating empty backup marker")
			if err := os.WriteFile(backupPath, []byte{}, 0644); err != nil {
				logger.Error("failed to create empty backup marker: %v", err)
				return err
			}
		} else {
			// Some other error (permission denied, etc.)
			logger.Error("error accessing original file: %v", err)
			return err
		}
	} else {
		logger.Debug("backup already exists, skipping backup creation")
	}

	var resolvContent []byte
	
	if len(nameservers) > 0 {
		logger.Debug("using custom nameservers: %v", nameservers)
		// Validate nameservers and create custom resolv.conf
		var validNameservers []string
		for _, ns := range nameservers {
			if ip := net.ParseIP(ns); ip != nil {
				validNameservers = append(validNameservers, ns)
				logger.Debug("nameserver validated: %s", ns)
			} else {
				logger.Error("invalid nameserver IP address: %s", ns)
				return fmt.Errorf("invalid nameserver IP address: %s", ns)
			}
		}
		
		// Create custom resolv.conf content
		var resolvLines []string
		for _, ns := range validNameservers {
			resolvLines = append(resolvLines, fmt.Sprintf("nameserver %s", ns))
		}
		resolvContent = []byte(strings.Join(resolvLines, "\n") + "\n")
		logger.Debug("generated custom resolv.conf content (%d bytes)", len(resolvContent))
	} else {
		logger.Debug("using host resolv.conf")
		// Read host resolv.conf, following symlinks
		var err error
		realPath, err := filepath.EvalSymlinks("/etc/resolv.conf")
		if err != nil {
			logger.Debug("symlink resolution failed, falling back to direct read: %v", err)
			// Fallback to direct read if symlink resolution fails
			resolvContent, err = os.ReadFile("/etc/resolv.conf")
		} else {
			logger.Debug("resolved symlink: /etc/resolv.conf -> %s", realPath)
			resolvContent, err = os.ReadFile(realPath)
		}
		if err != nil {
			logger.Error("failed to read host resolv.conf: %v", err)
			return err
		}
		logger.Debug("read host resolv.conf content (%d bytes)", len(resolvContent))
	}

	// if file exists, remove it before writing new content
	if _, err := os.Stat(target); err == nil {
		logger.Debug("removing existing resolv.conf: %s", target)
		if err := os.Remove(target); err != nil {
			logger.Error("failed to remove existing resolv.conf: %v", err)
			return err
		}
		logger.Debug("existing resolv.conf removed successfully")
	}

	// Write resolv.conf to chroot
	logger.Debug("writing resolv.conf to chroot: %s (%d bytes)", target, len(resolvContent))

	if err := os.WriteFile(target, resolvContent, 0644); err != nil {
		logger.Error("failed to write resolv.conf: %v", err)
		return err
	}
	logger.Debug("resolv.conf written successfully")
	return nil
}

func (e *Executor) restoreResolvConf(mountPoint string) error {
	target := mountPoint + "/etc/resolv.conf"
	backupPath := e.getBackupPath(mountPoint)
	symlinkBackupPath := e.getBackupSymlinkPath(mountPoint)

	logger.Debug("restoring resolv.conf: target=%s", target)
	logger.Debug("backup paths: content=%s, symlink=%s", backupPath, symlinkBackupPath)

	// Check if there was a symlink backup
	if symlinkTarget, err := os.ReadFile(symlinkBackupPath); err == nil && len(symlinkTarget) > 0 {
		logger.Debug("found symlink backup, restoring symlink: %s -> %s", target, string(symlinkTarget))
		// Remove current file and recreate symlink
		os.Remove(target)
		if err := os.Symlink(string(symlinkTarget), target); err != nil {
			logger.Error("failed to restore symlink: %v", err)
			return err
		}
		logger.Debug("symlink restored successfully")
		return nil
	}

	// Read regular backup
	logger.Debug("checking for regular file backup")
	backup, err := os.ReadFile(backupPath)
	if err != nil {
		logger.Debug("no backup found, removing current file: %v", err)
		// If no backup exists, just remove the current file
		os.Remove(target)
		return nil
	}

	if len(backup) == 0 {
		logger.Debug("empty backup found (original didn't exist), removing current file")
		// Empty backup means there was no original file
		os.Remove(target)
		return nil
	}

	// Restore original resolv.conf
	logger.Debug("restoring original file content (%d bytes)", len(backup))
	if err := os.WriteFile(target, backup, 0644); err != nil {
		logger.Error("failed to restore file content: %v", err)
		return err
	}
	logger.Debug("file content restored successfully")
	return nil
}

func (e *Executor) CleanupMountNamespace(mountPoint string) error {
	logger.Debug("starting mount namespace cleanup: %s", mountPoint)

	// Validate mountPoint to prevent accidentally unmounting host directories
	if mountPoint == "" || mountPoint == "/" || mountPoint == "/dev" || mountPoint == "/proc" || mountPoint == "/sys" {
		err := fmt.Errorf("invalid mount point for cleanup: %s. THIS PROBABLY IS A BUG!!", mountPoint)
		logger.Error("%v", err)
		return err
	}
	
	// Additional safety check - mount point should be an absolute path under /tmp or similar
	if !strings.HasPrefix(mountPoint, "/tmp/") && !strings.HasPrefix(mountPoint, "/mnt/") {
		err := fmt.Errorf("unsafe mount point for cleanup: %s. THIS PROBABLY IS A BUG!!", mountPoint)
		logger.Error("%v", err)
		return err
	}
	logger.Debug("mount point validation passed")
	
	// Ensure mountPoint ends with proper path separator for safe concatenation
	if !strings.HasSuffix(mountPoint, "/") {
		mountPoint = mountPoint + "/"
	}

	logger.Debug("cleaning up %d mount namespaces in reverse order", len(MountNamespaces))
	for i := len(MountNamespaces) - 1; i >= 0; i-- {
		target := mountPoint + strings.TrimPrefix(MountNamespaces[i].target, "/")
		logger.Debug("cleanup %d/%d: checking %s", len(MountNamespaces)-i, len(MountNamespaces), target)
		
		// Double-check that target is within the mount point to prevent host unmounting
		if !strings.HasPrefix(target, mountPoint) {
			logger.Warn("skipping unsafe unmount target: %s. THIS PROBABLY IS A BUG!!", target)
			continue
		}
		
		// Check if target is actually mounted before attempting unmount
		if !e.isMounted(target) {
			logger.Debug("target not mounted, skipping: %s", target)
			continue
		}
		
		logger.Debug("unmounting: %s", target)
		cmd := exec.Command("umount", target)
		err := cmd.Run()
		if err != nil {
			logger.Debug("standard unmount failed, trying lazy unmount: %v", err)
			// try lazy mode
			cmd = exec.Command("umount", "-l", target)
			err = cmd.Run()
			if err != nil {
				logger.Warn("failed to unmount %s: %v", target, err)
			} else {
				logger.Debug("lazy unmount successful: %s", target)
			}
		} else {
			logger.Debug("unmount successful: %s", target)
		}
	}

	logger.Debug("mount namespace cleanup completed")
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
