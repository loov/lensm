//go:build !darwin

package main

func chooseExecutableFile() (string, bool, error) {
	return "", false, ErrFilePickerUnsupported
}
