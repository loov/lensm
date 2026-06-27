package main

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

func chooseExecutableFile() (string, bool, error) {
	cmd := exec.Command("osascript",
		"-e", `set chosenFile to choose file with prompt "Choose executable to disassemble"`,
		"-e", `POSIX path of chosenFile`,
	)
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		msg := err.Error()
		if errors.As(err, &exitErr) {
			msg = strings.TrimSpace(string(exitErr.Stderr))
			if msg == "" {
				msg = err.Error()
			}
		}
		if strings.Contains(msg, "User canceled") || strings.Contains(msg, "(-128)") {
			return "", false, nil
		}
		return "", false, fmt.Errorf("choose file: %s", msg)
	}

	path := strings.TrimSpace(string(out))
	if path == "" {
		return "", false, fmt.Errorf("choose file: empty path")
	}
	return path, true, nil
}
