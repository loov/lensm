package main

import "errors"

// ErrFilePickerUnsupported reports that no native file picker exists on
// this platform, as opposed to the picker failing.
var ErrFilePickerUnsupported = errors.New("native file picker is only implemented on macOS")
