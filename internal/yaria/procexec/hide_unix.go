//go:build !windows

package procexec

import "os/exec"

// HideConsole is a no-op on Unix (no console flash for GUI apps).
func HideConsole(cmd *exec.Cmd) {}
