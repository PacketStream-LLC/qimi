package mount

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	qimiexec "github.com/packetstream-llc/qimi/internal/exec"
	"github.com/packetstream-llc/qimi/internal/nbd"
)

type Mounter struct {
	mountDir    string
	metadataDir string
}

func New() (*Mounter, error) {
	// Check system dependencies first
	if err := nbd.CheckSystemDependencies(); err != nil {
		return nil, fmt.Errorf("system dependencies not met: %w", err)
	}

	// Use /tmp/qimi/mounts for temporary mounts
	mountDir := "/tmp/qimi/mounts"
	if err := os.MkdirAll(mountDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create mount directory: %w", err)
	}

	// Use /tmp/qimi/metadata for NBD metadata
	metadataDir := "/tmp/qimi/metadata"
	if err := os.MkdirAll(metadataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create metadata directory: %w", err)
	}

	return &Mounter{
		mountDir:    mountDir,
		metadataDir: metadataDir,
	}, nil
}

func (m *Mounter) Mount(imagePath string, readOnly bool) (string, error) {
	return m.MountWithPartition(imagePath, readOnly, 0)
}

func (m *Mounter) MountWithPartition(imagePath string, readOnly bool, partitionNum int) (string, error) {
	absPath, err := filepath.Abs(imagePath)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	if _, err := os.Stat(absPath); err != nil {
		return "", fmt.Errorf("image file not found: %w", err)
	}

	mountPoint := filepath.Join(m.mountDir, filepath.Base(absPath)+".mount")
	if err := os.MkdirAll(mountPoint, 0755); err != nil {
		return "", fmt.Errorf("failed to create mount point: %w", err)
	}

	if err := m.mountQemuImage(absPath, mountPoint, readOnly, partitionNum); err != nil {
		os.RemoveAll(mountPoint)
		return "", err
	}

	return mountPoint, nil
}

func (m *Mounter) Unmount(mountPoint string) error {
	// Try to unmount, but don't fail if already unmounted
	cmd := exec.Command("umount", mountPoint)
	cmd.Run() // Ignore error as it might already be unmounted

	// Try to disconnect NBD if info exists
	m.disconnectNBD(mountPoint) // Ignore error

	// Clean up any backup files
	executor := qimiexec.New()
	executor.CleanupBackupFiles(mountPoint) // Ignore error

	// Always remove the mount point directory
	return os.RemoveAll(mountPoint)
}

func (m *Mounter) mountQemuImage(imagePath, mountPoint string, readOnly bool, partitionNum int) error {
	nbdDevice, err := nbd.FindFreeNBDDevice()
	if err != nil {
		return err
	}

	if err := nbd.ConnectImage(imagePath, nbdDevice, readOnly); err != nil {
		return err
	}

	if err := nbd.ProbePartitions(nbdDevice); err != nil {
		nbd.DisconnectDevice(nbdDevice)
		return err
	}

	partition, err := nbd.GetPartitionDevice(nbdDevice, partitionNum)
	if err != nil {
		nbd.DisconnectDevice(nbdDevice)
		return err
	}

	// Build mount options
	mountOpts := []string{}
	if readOnly {
		mountOpts = append(mountOpts, "-r")
	}
	mountOpts = append(mountOpts, partition, mountPoint)

	cmd := exec.Command("mount", mountOpts...)
	if output, err := cmd.CombinedOutput(); err != nil {
		nbd.DisconnectDevice(nbdDevice)
		return fmt.Errorf("failed to mount %s to %s: %w\nOutput: %s", partition, mountPoint, err, string(output))
	}

	// Store NBD metadata outside the mount point
	nbdFile := filepath.Join(m.metadataDir, filepath.Base(mountPoint)+".nbd")
	if err := os.WriteFile(nbdFile, []byte(nbdDevice), 0644); err != nil {
		exec.Command("umount", mountPoint).Run()
		nbd.DisconnectDevice(nbdDevice)
		return fmt.Errorf("failed to save nbd info: %w", err)
	}

	return nil
}

func (m *Mounter) disconnectNBD(mountPoint string) error {
	nbdFile := filepath.Join(m.metadataDir, filepath.Base(mountPoint)+".nbd")
	data, err := os.ReadFile(nbdFile)
	if err != nil {
		return nil
	}

	nbdDevice := strings.TrimSpace(string(data))
	err = nbd.DisconnectDevice(nbdDevice)

	// Clean up metadata file
	os.Remove(nbdFile)

	return err
}
