//go:build windows

package daemon

import (
	"os"
	"os/signal"
	"syscall"
)

// daemonSysProcAttr returns SysProcAttr for Windows.
// Windows does not support Setsid; CREATE_NEW_PROCESS_GROUP is used instead.
func daemonSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: 0x00000010} // CREATE_NEW_PROCESS_GROUP
}

// notifyShutdownSignals registers OS signals that should trigger a graceful shutdown.
// Windows only supports os.Interrupt (Ctrl+C).
func notifyShutdownSignals(sigCh chan os.Signal) {
	signal.Notify(sigCh, os.Interrupt)
}
