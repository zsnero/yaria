package logger

import (
	"os"

	"github.com/sirupsen/logrus"
)

type Logger interface {
	Info(format string, args ...any)
	Warn(format string, args ...any)
	Error(format string, args ...any)
}

type ConsoleLogger struct {
	logger *logrus.Logger
}

func NewConsoleLogger() *ConsoleLogger {
	logger := logrus.New()
	logger.SetOutput(os.Stdout)
	logger.SetFormatter(&logrus.TextFormatter{
		ForceColors:   true,
		FullTimestamp: true,
	})
	logger.SetLevel(logrus.InfoLevel)
	return &ConsoleLogger{logger: logger}
}

func (l *ConsoleLogger) Info(format string, args ...any) {
	l.logger.Infof(format, args...)
}

func (l *ConsoleLogger) Warn(format string, args ...any) {
	l.logger.Warnf(format, args...)
}

func (l *ConsoleLogger) Error(format string, args ...any) {
	l.logger.Errorf(format, args...)
}
