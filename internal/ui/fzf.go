package ui

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

var ErrNoSelection = errors.New("no selection")

func Select(prompt string, options []string) (string, error) {
	if len(options) == 0 {
		return "", ErrNoSelection
	}

	args := []string{
		"--height", "40%",
		"--layout", "reverse",
		"--border",
		"--prompt", prompt,
	}
	output, err := runFZF(args, strings.Join(options, "\n"))
	if err != nil {
		return "", err
	}
	output = strings.TrimSpace(output)
	if output == "" {
		return "", ErrNoSelection
	}
	return output, nil
}

func SelectOrQuery(prompt string, options []string) (string, error) {
	args := []string{
		"--height", "40%",
		"--layout", "reverse",
		"--border",
		"--prompt", prompt,
		"--print-query",
		"--bind", "enter:accept",
	}

	joined := strings.Join(options, "\n")
	if joined == "" {
		joined = "\n"
	}

	output, err := runFZF(args, joined)
	if err != nil {
		return "", err
	}

	output = strings.ReplaceAll(output, "\r\n", "\n")
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	if len(lines) == 0 {
		return "", ErrNoSelection
	}

	query := strings.TrimSpace(lines[0])
	selected := ""
	if len(lines) > 1 {
		selected = strings.TrimSpace(lines[len(lines)-1])
	}

	if selected != "" {
		return selected, nil
	}
	if query != "" {
		return query, nil
	}
	return "", ErrNoSelection
}

func runFZF(args []string, input string) (string, error) {
	if _, err := exec.LookPath("fzf"); err != nil {
		return "", errors.New("fzf not found in PATH")
	}

	cmd := exec.Command("fzf", args...)
	cmd.Stdin = strings.NewReader(input)
	cmd.Stderr = os.Stderr

	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code := exitErr.ExitCode()
			if code == 1 || code == 130 {
				return "", ErrNoSelection
			}
		}
		return "", fmt.Errorf("failed to run fzf: %w", err)
	}
	return out.String(), nil
}
