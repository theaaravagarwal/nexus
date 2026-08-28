package app

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/spf13/cobra"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:         "version",
		Short:       "Print version and build information",
		Args:        cobra.NoArgs,
		Annotations: map[string]string{"skip-bootstrap": "true"},
		Run: func(cmd *cobra.Command, args []string) {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "nexus %s (%s, %s, %s/%s)\n", version, commit, date, runtime.GOOS, runtime.GOARCH)
		},
	}
}

func newCompletionCmd(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:         "completion [bash|zsh|fish|powershell]",
		Short:       "Generate shell completion",
		Args:        cobra.ExactArgs(1),
		ValidArgs:   []string{"bash", "zsh", "fish", "powershell"},
		Annotations: map[string]string{"skip-bootstrap": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return root.GenBashCompletion(cmd.OutOrStdout())
			case "zsh":
				return root.GenZshCompletion(cmd.OutOrStdout())
			case "fish":
				return root.GenFishCompletion(cmd.OutOrStdout(), true)
			case "powershell":
				return root.GenPowerShellCompletion(cmd.OutOrStdout())
			default:
				return fmt.Errorf("unsupported shell %q", args[0])
			}
		},
	}
}

func (a *app) newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check Nexus dependencies and configuration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			requiredMissing := false
			for _, binary := range []string{"ssh", "rsync", "fzf", "ssh-copy-id"} {
				path, err := exec.LookPath(binary)
				status := "ok"
				if err != nil {
					status = "missing"
					if binary == "ssh" {
						requiredMissing = true
					}
				}
				if path != "" {
					status += " (" + path + ")"
				}
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%-12s %s\n", binary, status)
			}
			configMode := "unknown"
			if info, err := os.Stat(a.configFile); err == nil {
				configMode = info.Mode().Perm().String()
			}
			stateStatus := "ok"
			if _, err := loadState(a.stateFile); err != nil {
				stateStatus = "invalid: " + sanitizeTerminalText(err.Error())
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(),
				"config   %s (%s)\nhistory  %s\nstate    %s (%s)\ntheme    %s\nworkspace %s\ntabs     %t\npins     %d configured, %d unresolved\n",
				a.configFile, configMode, a.hostsFile, a.stateFile, stateStatus, activeTheme().Name,
				loadedConfig.UI.Workspace, loadedConfig.UI.ExperimentalTabs,
				len(loadedConfig.UI.PinnedActions), len(unresolvedPinnedActions(loadedConfig)))
			if requiredMissing {
				return fmt.Errorf("required dependency missing: ssh")
			}
			return nil
		},
	}
}
