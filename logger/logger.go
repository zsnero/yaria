package logger

import (
	"fmt"
	"io"
	"os"
)

// Logger interface for logging
type Logger interface {
	Info(format string, args ...any)
	Warn(format string, args ...any)
	Error(format string, args ...any)
}

type ConsoleLogger struct {
	stdout io.Writer
}

func NewConsoleLogger() *ConsoleLogger {
	return &ConsoleLogger{stdout: os.Stdout}
}

// Info
func (l *ConsoleLogger) Info(format string, args ...any) {
	fmt.Fprintf(l.stdout, format+"\n", args...)
}

// Warning
func (l *ConsoleLogger) Warn(format string, args ...any) {
	fmt.Fprintf(l.stdout, format+"\n", args...)
}

// Error
func (l *ConsoleLogger) Error(format string, args ...any) {
	fmt.Fprintf(l.stdout, format+"\n", args...)
}
