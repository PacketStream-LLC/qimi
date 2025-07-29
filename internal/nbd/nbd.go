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

// PartitionInfo contains information about a partition
type PartitionInfo struct {
	Number int
	Path   string
	FSType string
}

// GetPartitionDevice returns the best partition device to mount
// If partitionNum is specified (> 0), use that partition
// Otherwise, try to find the most suitable partition (preferring common filesystems)
// If multiple suitable partitions are found and no partitionNum is specified, returns an error
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
	partitions, deviceHasFS, err := detectSuitablePartitions(nbd)
	if err != nil {
		return "", err
	}

	if len(partitions) == 0 {
		// No suitable partitions found, use the device directly if it has a filesystem
		if deviceHasFS {
			return nbd, nil
		}
		// No filesystem found anywhere
		return nbd, nil // Still return the device for compatibility
	}

	if len(partitions) > 1 {
		// Check if we have obvious root filesystems vs boot/swap partitions
		rootFSTypes := []string{"ext4", "ext3", "ext2", "xfs", "btrfs", "f2fs"}
		var rootPartitions []PartitionInfo
		
		for _, part := range partitions {
			for _, rootFS := range rootFSTypes {
				if strings.EqualFold(part.FSType, rootFS) {
					rootPartitions = append(rootPartitions, part)
					break
				}
			}
		}
		
		// If we have exactly one obvious root filesystem, use it
		if len(rootPartitions) == 1 {
			return rootPartitions[0].Path, nil
		}
		
		// If we have multiple root filesystems of different types, pick the most preferred
		if len(rootPartitions) > 1 {
			// Check if they're all the same filesystem type
			firstType := strings.ToLower(rootPartitions[0].FSType)
			allSameType := true
			for _, part := range rootPartitions[1:] {
				if strings.ToLower(part.FSType) != firstType {
					allSameType = false
					break
				}
			}
			
			// If they're all the same type (e.g., multiple XFS), pick the larger one
			if allSameType {
				largestPartition, err := findLargestPartition(rootPartitions)
				if err != nil {
					// If we can't determine size, fall back to asking user
					var partNums []string
					for _, p := range rootPartitions {
						partNums = append(partNums, fmt.Sprintf("%d (%s)", p.Number, p.FSType))
					}
					return "", fmt.Errorf("multiple %s partitions found: %s. Please specify a partition number using --partition flag", firstType, strings.Join(partNums, ", "))
				}
				return largestPartition.Path, nil
			}
			
			// Different root filesystem types, pick the most preferred one
			return rootPartitions[0].Path, nil
		}
		
		// No obvious root filesystems, return the most preferred available
		return partitions[0].Path, nil
	}

	// Single suitable partition found
	return partitions[0].Path, nil
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

// detectSuitablePartitions finds all suitable partitions to mount
// Returns a list of partitions with recognized filesystems, sorted by preference
// Also returns whether the device itself has a filesystem
func detectSuitablePartitions(nbd string) ([]PartitionInfo, bool, error) {
	// Get partition information using lsblk
	cmd := exec.Command("lsblk", "-o", "NAME,FSTYPE", "-r", "-n", nbd)
	output, err := cmd.Output()
	if err != nil {
		// If lsblk fails, check if we can use the device directly
		if _, statErr := os.Stat(nbd); statErr == nil {
			return nil, true, nil
		}
		return nil, false, fmt.Errorf("failed to get partition info for %s: %w", nbd, err)
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 0 {
		return nil, false, nil // No partitions
	}

	// Debug: print lsblk output
	// fmt.Printf("DEBUG: lsblk output for %s:\n%s\n", nbd, string(output))

	// Parse partition information
	var partitions []PartitionInfo
	var deviceHasFS bool
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

		// Check if the base device itself has a filesystem
		if name == baseDeviceName {
			if fstype != "" && fstype != "-" {
				deviceHasFS = true
			}
			continue
		}

		// Check if this is a partition of our device
		if strings.HasPrefix(name, baseDeviceName+"p") {
			// Extract partition number
			partNumStr := strings.TrimPrefix(name, baseDeviceName+"p")
			partNum, _ := strconv.Atoi(partNumStr)
			if partNum > 0 {
				partitions = append(partitions, PartitionInfo{
					Number: partNum,
					Path:   "/dev/" + name,
					FSType: fstype,
				})
			}
		}
	}

	if len(partitions) == 0 {
		// No partitions found
		return nil, deviceHasFS, nil
	}

	// Priority order for filesystem types (most preferred first)
	preferredFS := []string{
		"ext4", "ext3", "ext2", // Linux filesystems
		"xfs", "btrfs", "f2fs", // Other Linux filesystems
		"ntfs", "fat32", "vfat", // Windows filesystems
		"hfs", "hfsplus", // macOS filesystems
	}

	// Filter partitions with recognized filesystems
	var suitablePartitions []PartitionInfo
	for _, part := range partitions {
		if part.FSType != "" && part.FSType != "-" {
			suitablePartitions = append(suitablePartitions, part)
		}
	}

	// If no partitions have recognized filesystems, return all partitions
	if len(suitablePartitions) == 0 {
		return partitions, deviceHasFS, nil
	}

	// Sort suitable partitions by filesystem preference
	// Create a priority map
	priority := make(map[string]int)
	for i, fs := range preferredFS {
		priority[strings.ToLower(fs)] = i
	}

	// Sort partitions by filesystem priority
	for i := 0; i < len(suitablePartitions)-1; i++ {
		for j := i + 1; j < len(suitablePartitions); j++ {
			pri1, ok1 := priority[strings.ToLower(suitablePartitions[i].FSType)]
			pri2, ok2 := priority[strings.ToLower(suitablePartitions[j].FSType)]

			// If both have priority, sort by priority
			if ok1 && ok2 && pri2 < pri1 {
				suitablePartitions[i], suitablePartitions[j] = suitablePartitions[j], suitablePartitions[i]
			} else if !ok1 && ok2 {
				// If only j has priority, swap
				suitablePartitions[i], suitablePartitions[j] = suitablePartitions[j], suitablePartitions[i]
			}
		}
	}

	return suitablePartitions, deviceHasFS, nil
}

// findLargestPartition finds the partition with the largest size from the given list
func findLargestPartition(partitions []PartitionInfo) (PartitionInfo, error) {
	if len(partitions) == 0 {
		return PartitionInfo{}, fmt.Errorf("no partitions provided")
	}

	// Get partition sizes using lsblk
	var devicePaths []string
	for _, p := range partitions {
		devicePaths = append(devicePaths, p.Path)
	}

	cmd := exec.Command("lsblk", "-o", "NAME,SIZE", "-r", "-n", "-b")
	cmd.Args = append(cmd.Args, devicePaths...)
	
	output, err := cmd.Output()
	if err != nil {
		return PartitionInfo{}, fmt.Errorf("failed to get partition sizes: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	partitionSizes := make(map[string]int64)

	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		
		deviceName := fields[0]
		sizeStr := fields[1]
		
		size, err := strconv.ParseInt(sizeStr, 10, 64)
		if err != nil {
			continue
		}
		
		// Map device name to full path
		fullPath := "/dev/" + deviceName
		partitionSizes[fullPath] = size
	}

	// Find the partition with the largest size
	var largestPartition PartitionInfo
	var largestSize int64 = -1

	for _, partition := range partitions {
		if size, exists := partitionSizes[partition.Path]; exists {
			if size > largestSize {
				largestSize = size
				largestPartition = partition
			}
		}
	}

	if largestSize == -1 {
		return PartitionInfo{}, fmt.Errorf("could not determine partition sizes")
	}

	return largestPartition, nil
}
