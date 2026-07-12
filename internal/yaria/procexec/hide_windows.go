//go:build windows

package procexec

import (
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000

// HideConsole prevents a console window from flashing when starting GUI-app
// child processes (yt-dlp.exe, aria2c.exe, ffmpeg.exe, etc.).
func HideConsole(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
}
