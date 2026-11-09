//go:build !darwin

package main

// Elsewhere the window draws the app menu itself.
const nativeMenu = false

var menuActions chan int

func installMenu([]nativeMenuDef)                 {}
func setMenuState(tag int, checked, enabled bool) {}
