package main

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// chooseExecutableFile opens the native file picker and blocks until it
// is dismissed. Call it off the UI event loop; a frame in progress
// cannot complete while the dialog is open.
func chooseExecutableFile() (string, bool, error) {
	cmd := exec.Command("osascript",
		"-e", `set chosenFile to choose file with prompt "Choose executable to disassemble"`,
		"-e", `POSIX path of chosenFile`,
	)
	out, err := cmd.Output()
	if err != nil {
		msg := err.Error()
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
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
		return "", false, errors.New("choose file: empty path")
	}
	return path, true, nil
}
