package nbd

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// CheckSystemDependencies verifies that required tools and modules are available
func CheckSystemDependencies() error {
	// Check if nbd module is loaded
	if err := checkNBDModule(); err != nil {
		return fmt.Errorf("nbd module not available: %w", err)
	}

	// Check if qemu-nbd is available
	if _, err := exec.LookPath("qemu-nbd"); err != nil {
		return fmt.Errorf("qemu-nbd not found: %w", err)
	}

	// Check if partprobe is available
	if _, err := exec.LookPath("partprobe"); err != nil {
		return fmt.Errorf("partprobe not found: %w", err)
	}

	return nil
}

// checkNBDModule checks if the nbd kernel module is loaded
func checkNBDModule() error {
	data, err := os.ReadFile("/proc/modules")
	if err != nil {
		return fmt.Errorf("failed to read /proc/modules: %w", err)
	}

	if !strings.Contains(string(data), "nbd ") {
		// Try to load the module
		cmd := exec.Command("modprobe", "nbd")
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to load nbd module: %w", err)
		}
	}

	return nil
}

// FindFreeNBDDevice finds an available NBD device
func FindFreeNBDDevice() (string, error) {
	for i := 0; i < 16; i++ {
		nbd := fmt.Sprintf("/dev/nbd%d", i)
		if isNBDFree(nbd) {
			return nbd, nil
		}
	}
	return "", fmt.Errorf("no free NBD device found")
}

// isNBDFree checks if an NBD device is free
func isNBDFree(nbd string) bool {
	// Extract NBD number from device path
	deviceName := strings.TrimPrefix(nbd, "/dev/")
	pidFile := fmt.Sprintf("/sys/devices/virtual/block/%s/pid", deviceName)
	
	// If pid file doesn't exist, device is free
	if _, err := os.Stat(pidFile); os.IsNotExist(err) {
		return true
	}
	
	// Read the pid file
	pidData, err := os.ReadFile(pidFile)
	if err != nil {
		// If we can't read the file, assume device is in use
		return false
	}
	
	// Check if the file is empty or contains just whitespace
	pidStr := strings.TrimSpace(string(pidData))
	if pidStr == "" {
		// Empty pid file means device is free
		return true
	}
	
	// Parse the PID
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		// Invalid PID format, assume device is in use
		return false
	}
	
	// Check if the process is still running
	// On Linux, we can check if /proc/PID exists
	procPath := fmt.Sprintf("/proc/%d", pid)
	if _, err := os.Stat(procPath); os.IsNotExist(err) {
		// Process doesn't exist, device is free
		return true
	}
	
	// Process exists, device is in use
	return false
}

// ConnectImage connects a QEMU image to an NBD device
func ConnectImage(imagePath, nbd string, readOnly bool) error {
	args := []string{"--connect", nbd, imagePath}
	if readOnly {
		args = append(args, "--read-only")
	}

	cmd := exec.Command("qemu-nbd", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to connect %s to %s: %w", imagePath, nbd, err)
	}

	return nil
}

// DisconnectDevice disconnects an NBD device
func DisconnectDevice(nbd string) error {
	cmd := exec.Command("qemu-nbd", "--disconnect", nbd)
	return cmd.Run()
}

// ProbePartitions runs partprobe on an NBD device
func ProbePartitions(nbd string) error {
	cmd := exec.Command("partprobe", nbd)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to probe partitions on %s: %w", nbd, err)
	}
	
	// Give the kernel time to create partition devices
	time.Sleep(500 * time.Millisecond)
	
	return nil
}

// GetPartitionDevice returns the best partition device to mount
// If partitionNum is specified (> 0), use that partition
// Otherwise, try to find the most suitable partition (preferring common filesystems)
func GetPartitionDevice(nbd string, partitionNum int) (string, error) {
	if partitionNum > 0 {
		// User specified a partition number
		partition := fmt.Sprintf("%sp%d", nbd, partitionNum)
		if _, err := os.Stat(partition); err != nil {
			return "", fmt.Errorf("partition %d not found on %s", partitionNum, nbd)
		}
		return partition, nil
	}

	// Auto-detect the best partition
	return detectBestPartition(nbd)
}

// GetPartitionNumber extracts partition number from a partition specifier
// Examples: "1" -> 1, "p2" -> 2, "partition3" -> 3
func GetPartitionNumber(partSpec string) int {
	if partSpec == "" {
		return 0
	}

	// Try to parse as direct number
	if num, err := strconv.Atoi(partSpec); err == nil {
		return num
	}

	// Extract number from string like "p1", "partition2", etc.
	re := regexp.MustCompile(`\d+`)
	matches := re.FindString(partSpec)
	if matches != "" {
		if num, err := strconv.Atoi(matches); err == nil {
			return num
		}
	}

	return 0
}

// detectBestPartition finds the most suitable partition to mount
func detectBestPartition(nbd string) (string, error) {
	// Get partition information using lsblk
	cmd := exec.Command("lsblk", "-o", "NAME,FSTYPE", "-r", "-n", nbd)
	output, err := cmd.Output()
	if err != nil {
		// If lsblk fails, check if we can use the device directly
		if _, statErr := os.Stat(nbd); statErr == nil {
			return nbd, nil
		}
		return "", fmt.Errorf("failed to get partition info for %s: %w", nbd, err)
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 0 {
		return nbd, nil // No partitions, use device directly
	}
	
	// Debug: print lsblk output
	// fmt.Printf("DEBUG: lsblk output for %s:\n%s\n", nbd, string(output))

	// Parse partition information
	type partition struct {
		name   string
		fstype string
		path   string
	}

	var partitions []partition
	baseDeviceName := strings.TrimPrefix(nbd, "/dev/")

	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 1 {
			continue
		}

		name := fields[0]
		fstype := ""
		if len(fields) > 1 {
			fstype = fields[1]
		}

		// Skip the base device, only look at partitions
		if name == baseDeviceName {
			continue
		}

		// Check if this is a partition of our device
		if strings.HasPrefix(name, baseDeviceName+"p") {
			partitions = append(partitions, partition{
				name:   name,
				fstype: fstype,
				path:   "/dev/" + name,
			})
			// fmt.Printf("DEBUG: Found partition: %s (fstype: %s)\n", name, fstype)
		}
	}

	if len(partitions) == 0 {
		// No partitions found, use the device directly
		return nbd, nil
	}

	// Priority order for filesystem types (most preferred first)
	preferredFS := []string{
		"ext4", "ext3", "ext2", // Linux filesystems
		"xfs", "btrfs", "f2fs", // Other Linux filesystems
		"ntfs", "fat32", "vfat", // Windows filesystems
		"hfs", "hfsplus", // macOS filesystems
	}

	// Try to find a partition with a preferred filesystem
	for _, preferredType := range preferredFS {
		for _, part := range partitions {
			if strings.EqualFold(part.fstype, preferredType) {
				return part.path, nil
			}
		}
	}

	// If no preferred filesystem found, look for any partition with a filesystem
	for _, part := range partitions {
		if part.fstype != "" && part.fstype != "-" {
			return part.path, nil
		}
	}

	// If no partition has a recognized filesystem, use the first partition
	// For cloud images, this is typically the root filesystem
	if len(partitions) > 0 {
		// fmt.Printf("DEBUG: No partition with recognized filesystem found, using first partition: %s\n", partitions[0].path)
		return partitions[0].path, nil
	}

	// Fallback to the device itself
	return nbd, nil
}