package main

import (
	"fmt"
	"os"

	"github.com/packetstream-llc/qimi/internal/mount"
	"github.com/packetstream-llc/qimi/internal/storage"
	"github.com/packetstream-llc/qimi/internal/utils"
	"github.com/spf13/cobra"
)

var unmountCmd = &cobra.Command{
	Use:   "unmount [image-file|name]",
	Short: "Unmount a QEMU image",
	Long:  `Unmount a QEMU image by its file path or name.`,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if !utils.IsRoot() {
			fmt.Fprintf(os.Stderr, "Error: This command requires root privileges. Please run with sudo.\n")
			os.Exit(1)
		}

		target := args[0]

		store, err := storage.New()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error initializing storage: %v\n", err)
			os.Exit(1)
		}

		mountInfo, err := store.GetMount(target)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		mounter, err := mount.New()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error initializing mounter: %v\n", err)
			os.Exit(1)
		}

		if err := mounter.Unmount(mountInfo.MountPoint); err != nil {
			fmt.Fprintf(os.Stderr, "Error unmounting: %v\n", err)
			os.Exit(1)
		}

		if err := store.RemoveMount(target); err != nil {
			fmt.Fprintf(os.Stderr, "Error removing mount info: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Successfully unmounted %s\n", target)
	},
}

func init() {
	rootCmd.AddCommand(unmountCmd)
}
