package main

import (
	"fmt"
	"os"

	"github.com/packetstream-llc/qimi/internal/storage"
	"github.com/spf13/cobra"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up stale mount entries",
	Long:  `Remove mount entries that are no longer valid (e.g., after system reboot).`,
	Run: func(cmd *cobra.Command, args []string) {
		store, err := storage.New()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error initializing storage: %v\n", err)
			os.Exit(1)
		}

		// List mounts before cleanup
		beforeMounts := store.ListMounts()

		if err := store.CleanupStaleMounts(); err != nil {
			fmt.Fprintf(os.Stderr, "Error cleaning up stale mounts: %v\n", err)
			os.Exit(1)
		}

		// List mounts after cleanup
		afterMounts := store.ListMounts()

		removed := len(beforeMounts) - len(afterMounts)
		if removed > 0 {
			fmt.Printf("Cleaned up %d stale mount(s)\n", removed)
		} else {
			fmt.Println("No stale mounts found")
		}
	},
}

func init() {
	rootCmd.AddCommand(cleanupCmd)
}
