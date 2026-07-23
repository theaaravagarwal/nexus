package main

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func installHelp(root *cobra.Command) {
	root.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		_, _ = io.WriteString(cmd.OutOrStdout(), renderHelp(cmd, shouldColorHelp(cmd.OutOrStdout())))
	})
}

func shouldColorHelp(writer io.Writer) bool {
	if _, disabled := os.LookupEnv("NO_COLOR"); disabled {
		return false
	}
	if os.Getenv("CLICOLOR_FORCE") != "" {
		return true
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	file, ok := writer.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func renderHelp(cmd *cobra.Command, color bool) string {
	style := func(code, text string) string {
		if !color {
			return text
		}
		return "\x1b[" + code + "m" + text + "\x1b[0m"
	}
	heading := func(text string) string { return style("1;38;5;81", text) }
	commandStyle := func(text string) string { return style("1;38;5;141", text) }
	muted := func(text string) string { return style("38;5;244", text) }

	var out strings.Builder
	out.WriteString(commandStyle("NEXUS"))
	out.WriteString("  ")
	out.WriteString(muted("remote workspace"))
	out.WriteString("\n\n")
	if cmd.Long != "" {
		out.WriteString(cmd.Long)
	} else {
		out.WriteString(cmd.Short)
	}
	out.WriteString("\n\n")
	out.WriteString(heading("Usage"))
	out.WriteString("\n  ")
	out.WriteString(commandStyle(cmd.UseLine()))
	out.WriteString("\n")

	children := cmd.Commands()
	if len(children) > 0 {
		out.WriteString("\n")
		out.WriteString(heading("Commands"))
		out.WriteString("\n")
		for _, child := range children {
			if !child.IsAvailableCommand() || child.Hidden {
				continue
			}
			name := child.Name()
			if len(child.Aliases) > 0 {
				name += muted(" (" + strings.Join(child.Aliases, ", ") + ")")
			}
			fmt.Fprintf(&out, "  %-24s %s\n", commandStyle(name), child.Short)
		}
	}

	flags := collectHelpFlags(cmd)
	if len(flags) > 0 {
		out.WriteString("\n")
		out.WriteString(heading("Flags"))
		out.WriteString("\n")
		for _, flag := range flags {
			label := "--" + flag.Name
			if flag.Shorthand != "" {
				label = "-" + flag.Shorthand + ", " + label
			}
			if flag.NoOptDefVal == "" {
				label += " " + flag.Value.Type()
			}
			fmt.Fprintf(&out, "  %-24s %s", commandStyle(label), flag.Usage)
			if flag.DefValue != "" && flag.DefValue != "false" && flag.DefValue != "0" {
				out.WriteString(muted(" (default " + flag.DefValue + ")"))
			}
			out.WriteString("\n")
		}
	}
	if cmd.Example != "" {
		out.WriteString("\n")
		out.WriteString(heading("Examples"))
		out.WriteString("\n")
		for _, line := range strings.Split(cmd.Example, "\n") {
			out.WriteString("  " + line + "\n")
		}
	}
	out.WriteString("\n")
	out.WriteString(muted("Run `nexus <command> --help` for command details. Set NO_COLOR=1 for plain output."))
	out.WriteString("\n")
	return out.String()
}

func collectHelpFlags(cmd *cobra.Command) []*pflag.Flag {
	seen := map[string]*pflag.Flag{}
	cmd.NonInheritedFlags().VisitAll(func(flag *pflag.Flag) {
		if !flag.Hidden {
			seen[flag.Name] = flag
		}
	})
	cmd.InheritedFlags().VisitAll(func(flag *pflag.Flag) {
		if !flag.Hidden {
			seen[flag.Name] = flag
		}
	})
	flags := make([]*pflag.Flag, 0, len(seen))
	for _, flag := range seen {
		flags = append(flags, flag)
	}
	sort.Slice(flags, func(i, j int) bool { return flags[i].Name < flags[j].Name })
	return flags
}
