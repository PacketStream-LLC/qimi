package main

import (
	"fmt"
	"os"

	"github.com/packetstream-llc/qimi/internal/logger"
	"github.com/packetstream-llc/qimi/internal/mount"
	"github.com/packetstream-llc/qimi/internal/nbd"
	"github.com/packetstream-llc/qimi/internal/storage"
	"github.com/packetstream-llc/qimi/internal/utils"
	"github.com/spf13/cobra"
)

var (
	readOnly  bool
	partition string
)

var mountCmd = &cobra.Command{
	Use:   "mount [image-file] [name]",
	Short: "Mount a QEMU image",
	Long:  `Mount a QEMU image file (.qcow2, .qcow2c, .raw) with an optional name.`,
	Args:  cobra.RangeArgs(1, 2),
	Run: func(cmd *cobra.Command, args []string) {
		if !utils.IsRoot() {
			fmt.Fprintf(os.Stderr, "Error: This command requires root privileges. Please run with sudo.\n")
			os.Exit(1)
		}

		imagePath := args[0]
		var name string
		if len(args) > 1 {
			name = args[1]
		}

		store, err := storage.New()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error initializing storage: %v\n", err)
			os.Exit(1)
		}

		mounter, err := mount.New()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error initializing mounter: %v\n", err)
			os.Exit(1)
		}

		partitionNum := 0
		if partition != "" {
			partitionNum = nbd.GetPartitionNumber(partition)
		}

		mountPoint, err := mounter.MountWithPartition(imagePath, readOnly, partitionNum)
		if err != nil {
			logger.Fatal("Error mounting image: %v", err)
		}

		mountInfo := &storage.MountInfo{
			ImagePath:  imagePath,
			MountPoint: mountPoint,
			Name:       name,
			ReadOnly:   readOnly,
		}

		if err := store.AddMount(mountInfo); err != nil {
			mounter.Unmount(mountPoint)
			logger.Fatal("Error saving mount info: %v", err)
		}

		fmt.Printf("Successfully mounted %s", imagePath)
		if name != "" {
			fmt.Printf(" as '%s'", name)
		}
		fmt.Printf(" at %s\n", mountPoint)
	},
}

func init() {
	mountCmd.Flags().BoolVar(&readOnly, "read-only", false, "Mount the image as read-only")
	mountCmd.Flags().StringVarP(&partition, "partition", "p", "", "Specify partition number to mount (e.g., 1,2,3). If not specified, auto-detect best partition")
	rootCmd.AddCommand(mountCmd)
}
