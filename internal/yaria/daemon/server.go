package daemon

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

// SocketPath returns the Unix socket path for IPC
func SocketPath() string {
	return filepath.Join(os.TempDir(), "yaria.sock")
}

// IsRunning checks if the daemon is reachable
func IsRunning() bool {
	conn, err := net.DialTimeout("unix", SocketPath(), 500*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// StopDaemon sends a stop command to the running daemon and waits for it to exit
func StopDaemon() {
	if !IsRunning() {
		_ = os.Remove(SocketPath())
		return
	}
	_, _ = Send(Request{Cmd: CmdStop})
	for i := 0; i < 30; i++ {
		time.Sleep(100 * time.Millisecond)
		if !IsRunning() {
			break
		}
	}
	_ = os.Remove(SocketPath())
}

// RestartDaemon stops the old daemon and starts a fresh one
func RestartDaemon() error {
	StopDaemon()
	return EnsureRunning()
}

// EnsureRunning starts the daemon if not already running.
func EnsureRunning() error {
	if IsRunning() {
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	// Start daemon as a detached process
	attr := &os.ProcAttr{
		Dir:   "/",
		Env:   os.Environ(),
		Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
		Sys:   daemonSysProcAttr(),
	}
	proc, err := os.StartProcess(exe, []string{exe, "daemon"}, attr)
	if err != nil {
		return fmt.Errorf("failed to start daemon: %w", err)
	}
	_ = proc.Release()

	// Wait for socket to appear
	for i := 0; i < 30; i++ {
		time.Sleep(200 * time.Millisecond)
		if IsRunning() {
			return nil
		}
	}
	return fmt.Errorf("daemon did not start within timeout")
}

// RunDaemon runs the daemon process (blocking)
func RunDaemon() error {
	sockPath := SocketPath()
	_ = os.Remove(sockPath)

	// State store in user config dir
	configDir, err := os.UserConfigDir()
	if err != nil {
		configDir = os.TempDir()
	}
	stateDir := filepath.Join(configDir, "yaria")
	store, err := NewStateStore(stateDir)
	if err != nil {
		return fmt.Errorf("failed to create state store: %w", err)
	}

	mgr := NewManager(store)
	defer mgr.Close()
	defer store.Close()

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}
	defer ln.Close()

	// Graceful shutdown
	shutdown := make(chan struct{})
	sigCh := make(chan os.Signal, 1)
	notifyShutdownSignals(sigCh)
	go func() {
		<-sigCh
		close(shutdown)
		ln.Close()
	}()

	// Accept loop
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				select {
				case <-shutdown:
					return
				default:
					continue
				}
			}
			go handleConn(conn, mgr, shutdown)
		}
	}()

	<-shutdown
	return nil
}

func handleConn(conn net.Conn, mgr *Manager, shutdown chan struct{}) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
	if !scanner.Scan() {
		return
	}
	var req Request
	if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
		resp := Response{OK: false, Error: "invalid request"}
		data, _ := json.Marshal(resp)
		fmt.Fprintf(conn, "%s\n", data)
		return
	}

	resp := handleRequest(req, mgr, shutdown)
	data, _ := json.Marshal(resp)
	fmt.Fprintf(conn, "%s\n", data)
}

func handleRequest(req Request, mgr *Manager, shutdown chan struct{}) Response {
	switch req.Cmd {
	case CmdAdd:
		id, err := mgr.Add(req)
		if err != nil {
			return Response{OK: false, Error: err.Error()}
		}
		return Response{OK: true, Torrents: []DownloadInfo{{ID: id, Title: req.Title}}}
	case CmdRemove:
		if err := mgr.Remove(req.ID); err != nil {
			return Response{OK: false, Error: err.Error()}
		}
		return Response{OK: true}
	case CmdPause:
		if err := mgr.Pause(req.ID); err != nil {
			return Response{OK: false, Error: err.Error()}
		}
		return Response{OK: true}
	case CmdResume:
		if err := mgr.Resume(req.ID); err != nil {
			return Response{OK: false, Error: err.Error()}
		}
		return Response{OK: true}
	case CmdList:
		list := mgr.List()
		return Response{OK: true, Torrents: list}
	case CmdStop:
		go func() {
			time.Sleep(100 * time.Millisecond)
			p, _ := os.FindProcess(os.Getpid())
			_ = p.Signal(os.Interrupt)
		}()
		return Response{OK: true}
	default:
		return Response{OK: false, Error: "unknown command: " + req.Cmd}
	}
}
