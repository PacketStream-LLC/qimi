package main

import (
	"fmt"
	"os"

	"github.com/packetstream-llc/qimi/internal/nbd"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "qimi",
	Short: "Qimi: Qemu Image Manipulator, Interactive - Mount and run QEMU images",
	Long:  `Qimi allows you to mount QEMU images (.qcow2, .qcow2c, .raw) and run binaries inside them.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Check system dependencies before running any command
		if err := nbd.CheckSystemDependencies(); err != nil {
			err := fmt.Errorf("system dependencies not met: %w\n\nRequired dependencies:\n- qemu-nbd (install qemu-utils package)\n- partprobe (install parted package)\n- nbd kernel module (modprobe nbd)", err)
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
			return err
		}
		return nil
	},
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
