package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"nexus/internal/hosts"
)

func newHostCmd() *cobra.Command {
	hostCmd := &cobra.Command{
		Use:   "host",
		Short: "Manage host history",
	}

	hostCmd.AddCommand(&cobra.Command{
		Use:   "add [user@ip]",
		Short: "Add a host to history",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := mustStore()
			if err != nil {
				return err
			}

			if err := hosts.Validate(args[0]); err != nil {
				return fmt.Errorf("invalid host format: %w", err)
			}
			added, err := store.Add(args[0])
			if err != nil {
				return err
			}
			if !added {
				fmt.Println("host already exists")
				return nil
			}
			fmt.Println("host added")
			return nil
		},
	})

	hostCmd.AddCommand(&cobra.Command{
		Use:     "remove [user@ip]",
		Aliases: []string{"rm"},
		Short:   "Remove a host from history",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := mustStore()
			if err != nil {
				return err
			}
			removed, err := store.Remove(args[0])
			if err != nil {
				return err
			}
			if !removed {
				fmt.Println("host not found")
				return nil
			}
			fmt.Println("host removed")
			return nil
		},
	})

	hostCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List known hosts",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := mustStore()
			if err != nil {
				return err
			}

			items, err := store.Load()
			if err != nil {
				return err
			}
			for _, item := range items {
				fmt.Println(item)
			}
			return nil
		},
	})

	return hostCmd
}
