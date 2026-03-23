package cmd

import "github.com/spf13/cobra"

var rootCmd = &cobra.Command{
	Use:           "nexus",
	Short:         "History-first CLI for SSH and remote file transfers",
	SilenceUsage:  true,
	SilenceErrors: true,
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.AddCommand(newSSHCmd())
	rootCmd.AddCommand(newPullCmd())
	rootCmd.AddCommand(newPushCmd())
	rootCmd.AddCommand(newHostCmd())
}
