package nexus

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func installHelp(root *cobra.Command, a *app) {
	root.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		if a != nil && a.configFile != "" {
			if cfg, err := loadAppConfig(a.configFile); err == nil {
				loadedConfig = cfg
			}
		}
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
	t := activeTheme()
	style := func(colorValue, text string, bold bool) string {
		if !color {
			return text
		}
		code := ansiForeground(colorValue)
		if bold {
			code = "1;" + code
		}
		return "\x1b[" + code + "m" + text + "\x1b[0m"
	}
	heading := func(text string) string { return style(t.Live, text, true) }
	commandStyle := func(text string) string { return style(t.Focus, text, true) }
	muted := func(text string) string { return style(t.Muted, text, false) }

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

func ansiForeground(value string) string {
	value = strings.TrimSpace(value)
	if len(value) == 7 && value[0] == '#' {
		r, rErr := strconv.ParseUint(value[1:3], 16, 8)
		g, gErr := strconv.ParseUint(value[3:5], 16, 8)
		b, bErr := strconv.ParseUint(value[5:7], 16, 8)
		if rErr == nil && gErr == nil && bErr == nil {
			return fmt.Sprintf("38;2;%d;%d;%d", r, g, b)
		}
	}
	if _, err := strconv.Atoi(value); err == nil {
		return "38;5;" + value
	}
	return "39"
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
