package autostart

import (
	"fmt"
	"os"
	"os/exec"
)

// Launch handles only the explicit unprivileged desktop launcher mode.
// invoke is injected so tests never execute pkexec or open authentication UI.
func Launch(args []string, uid int, home, xdg string, invoke func([]string) int) (bool, int) {
	if len(args) < 2 || args[1] != "--desktop-launch" {
		return false, 0
	}
	if len(args) != 2 || uid == 0 {
		return true, 1
	}
	config, err := ConfigDir(home, xdg)
	if err != nil {
		return true, 1
	}
	return true, invoke([]string{"--disable-internal-agent", Binary, "--desktop-config-dir", config})
}

func InvokePKExec(args []string) int {
	command := exec.Command("/usr/bin/pkexec", args...)
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := command.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return exit.ExitCode()
		}
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

// Context consumes our launch context before GTK parses application arguments.
func Context(args []string) (config string, remaining []string, err error) {
	remaining = []string{args[0]}
	for i := 1; i < len(args); i++ {
		if args[i] != "--desktop-config-dir" {
			remaining = append(remaining, args[i])
			continue
		}
		if config != "" || i+1 >= len(args) {
			return "", nil, fmt.Errorf("invalid desktop configuration context")
		}
		i++
		config = args[i]
		if config == "" {
			return "", nil, fmt.Errorf("empty desktop configuration directory")
		}
	}
	return config, remaining, nil
}
