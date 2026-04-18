package license

import (
	"os/exec"
)

// Runs a command and returns its combined output.
func execCommand(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	return cmd.CombinedOutput()
}
