//go:build !windows

package daemon

import (
	"os"
	"os/signal"
	"syscall"
)

// daemonSysProcAttr returns SysProcAttr to detach the daemon process.
// On Unix, Setsid creates a new session so the daemon survives parent exit.
func daemonSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}

// notifyShutdownSignals registers OS signals that should trigger a graceful shutdown.
func notifyShutdownSignals(sigCh chan os.Signal) {
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
}
