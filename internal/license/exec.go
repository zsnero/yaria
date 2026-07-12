package license

import (
	"os/exec"

	"yaria/internal/yaria/procexec"
)

// Runs a command and returns its combined output.
func execCommand(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	procexec.HideConsole(cmd)
	return cmd.CombinedOutput()
}
