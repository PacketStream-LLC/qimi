package main

import (
	"fmt"
	"os"

	"github.com/packetstream-llc/qimi/internal/logger"
	"github.com/packetstream-llc/qimi/internal/nbd"
	"github.com/spf13/cobra"
)

var (
	logLevel string
)

var rootCmd = &cobra.Command{
	Use:   "qimi",
	Short: "Qimi: Qemu Image Manipulator, Interactive - Mount and run QEMU images",
	Long:  `Qimi allows you to mount QEMU images (.qcow2, .qcow2c, .raw) and run binaries inside them.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Setup logger
		if level, err := logger.ParseLevel(logLevel); err != nil {
			logger.Warn("invalid log level '%s', using info", logLevel)
			logger.SetLevel(logger.LevelInfo)
		} else {
			logger.SetLevel(level)
		}

		// Check system dependencies before running any command
		if err := nbd.CheckSystemDependencies(); err != nil {
			logger.Fatal("system dependencies not met: %v\n\nRequired dependencies:\n- qemu-nbd (install qemu-utils package)\n- partprobe (install parted package)\n- nbd kernel module (modprobe nbd)", err)
		}
		return nil
	},
}

func init() {
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "Set log level (debug, info, warn, error, fatal)")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		logger.Fatal("%v", err)
	}
}
