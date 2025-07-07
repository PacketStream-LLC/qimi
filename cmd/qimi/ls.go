package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/packetstream-llc/qimi/internal/storage"
	"github.com/spf13/cobra"
)

var lsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List mounted images",
	Long:  `Show all currently mounted QEMU images and their mount locations.`,
	Run: func(cmd *cobra.Command, args []string) {
		// check system dependencies
		store, err := storage.New()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error initializing storage: %v\n", err)
			os.Exit(1)
		}

		mounts := store.ListMounts()
		if len(mounts) == 0 {
			fmt.Println("No images currently mounted")
			return
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
		fmt.Fprintln(w, "NAME\tIMAGE\tMOUNT POINT\tREAD-ONLY\tSTATUS")
		for _, m := range mounts {
			name := m.Name
			if name == "" {
				name = "-"
			}
			readOnly := "no"
			if m.ReadOnly {
				readOnly = "yes"
			}
			status := "active"
			if !store.IsValidMount(m) {
				status = "stale"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", name, m.ImagePath, m.MountPoint, readOnly, status)
		}
		w.Flush()
	},
}

func init() {
	rootCmd.AddCommand(lsCmd)
}
