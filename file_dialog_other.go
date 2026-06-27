//go:build !darwin

package main

import "fmt"

func chooseExecutableFile() (string, bool, error) {
	return "", false, fmt.Errorf("native file picker is only implemented on macOS")
}
