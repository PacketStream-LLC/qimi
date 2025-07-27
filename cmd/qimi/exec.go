package main

import (
	"fmt"
	"os"

	"github.com/packetstream-llc/qimi/internal/exec"
	"github.com/packetstream-llc/qimi/internal/mount"
	"github.com/packetstream-llc/qimi/internal/storage"
	"github.com/packetstream-llc/qimi/internal/utils"
	"github.com/spf13/cobra"
)

var (
	interactive  bool
	tty          bool
	execReadOnly bool
	nameservers  []string
)

var execCmd = &cobra.Command{
	Use:   "exec [flags] [image-file|name] [command] [args...]",
	Short: "Execute a command in a QEMU image",
	Long:  `Mount a QEMU image (if not already mounted) and execute a command inside it using chroot.`,
	Args:  cobra.MinimumNArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		if !utils.IsRoot() {
			fmt.Fprintf(os.Stderr, "Error: This command requires root privileges. Please run with sudo.\n")
			os.Exit(1)
		}

		target := args[0]
		command := args[1]
		commandArgs := args[2:]

		store, err := storage.New()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error initializing storage: %v\n", err)
			os.Exit(1)
		}

		mountInfo, err := store.GetMount(target)
		var mountPoint string
		var tempMount bool

		if err != nil {
			if _, statErr := os.Stat(target); statErr == nil {
				mounter, err := mount.New()
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error initializing mounter: %v\n", err)
					os.Exit(1)
				}

				mountPoint, err = mounter.Mount(target, execReadOnly)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error mounting image: %v\n", err)
					os.Exit(1)
				}
				tempMount = true
				defer func() {
					mounter.Unmount(mountPoint)
				}()
			} else {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		} else {
			mountPoint = mountInfo.MountPoint
		}

		executor := exec.New()
		if err := executor.Execute(mountPoint, command, commandArgs, interactive, tty, nameservers); err != nil {
			fmt.Fprintf(os.Stderr, "Error executing command: %v\n", err)
			os.Exit(1)
		}

		if tempMount {
			executor.CleanupMountNamespace(mountPoint)
		}
	},
}

func init() {
	execCmd.Flags().BoolVarP(&interactive, "interactive", "i", false, "Keep STDIN open")
	execCmd.Flags().BoolVarP(&tty, "tty", "t", false, "Allocate a pseudo-TTY")
	execCmd.Flags().BoolVar(&execReadOnly, "read-only", false, "Mount the image as read-only")
	execCmd.Flags().StringSliceVar(&nameservers, "nameserver", nil, "Custom nameservers for resolv.conf (can be specified multiple times)")
	rootCmd.AddCommand(execCmd)
}
