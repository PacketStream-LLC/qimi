package main

import (
	"fmt"
	"os"
	osExec "os/exec"

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
	RunE: func(cmd *cobra.Command, args []string) error {
		if !utils.IsRoot() {
			return fmt.Errorf("this command requires root privileges. Please run with sudo")
		}

		target := args[0]
		command := args[1]
		commandArgs := args[2:]

		store, err := storage.New()
		if err != nil {
			return fmt.Errorf("error initializing storage: %w", err)
		}

		mountInfo, err := store.GetMount(target)
		var mountPoint string
		var tempMount bool
		var mounter *mount.Mounter

		if err != nil {
			if _, statErr := os.Stat(target); statErr == nil {
				mounter, err = mount.New()
				if err != nil {
					return fmt.Errorf("error initializing mounter: %w", err)
				}

				mountPoint, err = mounter.Mount(target, execReadOnly)
				if err != nil {
					return fmt.Errorf("error mounting image: %w", err)
				}
				tempMount = true
			} else {
				return fmt.Errorf("error: %w", err)
			}
		} else {
			mountPoint = mountInfo.MountPoint
		}

		executor := exec.New()

		// Setup cleanup function
		cleanup := func() {
			// Clean up mount namespace first
			if err := executor.CleanupMountNamespace(mountPoint); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to cleanup mount namespace: %v\n", err)
			}
			if err := executor.CleanupBackupFiles(mountPoint); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to cleanup backup files: %v\n", err)
			}

			// If this was a temporary mount, unmount it
			if tempMount && mounter != nil {
				if err := mounter.Unmount(mountPoint); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to unmount: %v\n", err)
				}
			}
		}

		// Execute the command
		execErr := executor.Execute(mountPoint, command, commandArgs, interactive, tty, nameservers)

		// Always cleanup
		cleanup()

		// Return the execution error (cobra will handle the exit code)
		if execErr != nil {
			// check if it is exit status
			if exitErr, ok := execErr.(*osExec.ExitError); ok {
				if exitErr.ExitCode() != 0 {
					os.Exit(exitErr.ExitCode())
				}

				// If the command failed, return the error with exit code
				return fmt.Errorf("command exited with code %d: %w", exitErr.ExitCode(), execErr)
			}
		}

		return nil
	},
}

func init() {
	execCmd.Flags().BoolVarP(&interactive, "interactive", "i", false, "Keep STDIN open")
	execCmd.Flags().BoolVarP(&tty, "tty", "t", false, "Allocate a pseudo-TTY")
	execCmd.Flags().BoolVar(&execReadOnly, "read-only", false, "Mount the image as read-only")
	execCmd.Flags().StringSliceVar(&nameservers, "nameserver", nil, "Custom nameservers for resolv.conf (can be specified multiple times)")
	rootCmd.AddCommand(execCmd)
}
