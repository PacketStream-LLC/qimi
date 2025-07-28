package mount

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	qimiexec "github.com/packetstream-llc/qimi/internal/exec"
	"github.com/packetstream-llc/qimi/internal/logger"
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
	logger.Debug("mounting image: %s, readOnly: %t, partitionNum: %d", imagePath, readOnly, partitionNum)
	absPath, err := filepath.Abs(imagePath)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	if _, err := os.Stat(absPath); err != nil {
		return "", fmt.Errorf("image file not found: %w", err)
	}

	logger.Debug("absolute path of image: %s", absPath)
	logger.Debug("creating mount point in: %s", m.mountDir)
	mountPoint := filepath.Join(m.mountDir, filepath.Base(absPath)+".mount")
	if err := os.MkdirAll(mountPoint, 0755); err != nil {
		return "", fmt.Errorf("failed to create mount point: %w", err)
	}

	logger.Debug("mount point created: %s", mountPoint)
	if err := m.mountQemuImage(absPath, mountPoint, readOnly, partitionNum); err != nil {
		os.RemoveAll(mountPoint)
		return "", err
	}

	return mountPoint, nil
}

func (m *Mounter) Unmount(mountPoint string) error {
	logger.Debug("unmounting mount point: %s", mountPoint)

	// Try to unmount, but don't fail if already unmounted
	cmd := exec.Command("umount", mountPoint)
	cmd.Run() // Ignore error as it might already be unmounted

	// Try to disconnect NBD if info exists
	m.disconnectNBD(mountPoint) // Ignore error

	// Clean up any backup files
	executor := qimiexec.New()
	executor.CleanupBackupFiles(mountPoint) // Ignore error

	// check if directory is empty before removing
	if entries, err := os.ReadDir(mountPoint); err != nil {
		return fmt.Errorf("failed to read mount point directory: %w", err)
	} else if len(entries) == 0 {
		if err := os.RemoveAll(mountPoint); err != nil {
			return fmt.Errorf("failed to remove mount point directory: %w", err)
		}
	} else {
		logger.Warn("Mount point %s is not empty, skipping removal", mountPoint)
	}

	return nil
}

func (m *Mounter) mountQemuImage(imagePath, mountPoint string, readOnly bool, partitionNum int) error {
	logger.Debug("mounting QEMU image: %s to %s, readOnly: %t, partitionNum: %d", imagePath, mountPoint, readOnly, partitionNum)
	nbdDevice, err := nbd.FindFreeNBDDevice()
	if err != nil {
		return err
	}

	logger.Debug("Found free NBD Device! Using NBD device: %s", nbdDevice)
	logger.Debug("Connecting image %s to NBD device %s", imagePath, nbdDevice)
	if err := nbd.ConnectImage(imagePath, nbdDevice, readOnly); err != nil {
		m.disconnectNBD(nbdDevice)
		return err
	}

	logger.Debug("Probing partitions on NBD device %s", nbdDevice)
	if err := nbd.ProbePartitions(nbdDevice); err != nil {
		m.disconnectNBD(nbdDevice)
		return err
	}

	logger.Debug("Getting partition device for partition number %d on NBD device %s", partitionNum, nbdDevice)
	partition, err := nbd.GetPartitionDevice(nbdDevice, partitionNum)
	if err != nil {
		m.disconnectNBD(nbdDevice)
		return err
	}

	// Build mount options
	logger.Debug("Mounting partition %s to mount point %s", partition, mountPoint)
	mountOpts := []string{}
	if readOnly {
		logger.Debug("Mounting in read-only mode")
		mountOpts = append(mountOpts, "-r")
	}
	mountOpts = append(mountOpts, partition, mountPoint)

	logger.Debug("Executing mount command: %s", strings.Join(mountOpts, " "))
	cmd := exec.Command("mount", mountOpts...)
	if output, err := cmd.CombinedOutput(); err != nil {
		m.disconnectNBD(nbdDevice)
		return fmt.Errorf("failed to mount %s to %s: %w\nOutput: %s", partition, mountPoint, err, string(output))
	}

	// Store NBD metadata outside the mount point
	nbdFile := filepath.Join(m.metadataDir, filepath.Base(mountPoint)+".nbd")
	if err := os.WriteFile(nbdFile, []byte(nbdDevice), 0644); err != nil {
		m.Unmount(mountPoint) // Attempt to unmount if saving metadata fails
		return fmt.Errorf("failed to save nbd info: %w", err)
	}

	return nil
}

func (m *Mounter) disconnectNBD(mountPoint string) error {
	nbdFile := filepath.Join(m.metadataDir, filepath.Base(mountPoint)+".nbd")
	data, err := os.ReadFile(nbdFile)
	if err != nil {
		if os.IsNotExist(err) {
			// The NBD doesn't exist. let's check lsblk.
			logger.Warn("NBD metadata file not found, please run lsblk for check which NBD device is used and unmount it via qemu-nbd --disconnect")
			return errors.New("NBD metadata file mismatch")
		}

		return nil
	}

	nbdDevice := strings.TrimSpace(string(data))
	err = nbd.DisconnectDevice(nbdDevice)

	// Clean up metadata file
	os.Remove(nbdFile)

	return err
}
