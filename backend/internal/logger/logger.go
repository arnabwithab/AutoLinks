// ----- structured file-based logging @ backend/internal/logger/logger.go -----
package logger

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	logDir string
	mu     sync.Mutex
	l      *log.Logger
)

func init() {
	logDir = "logs"
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		log.Fatalf("failed to create log directory: %v", err)
	}

	filename := filepath.Join(logDir, fmt.Sprintf("log_%s.log", time.Now().Format("2006-01-02")))
	f, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		log.Fatalf("failed to open log file: %v", err)
	}
	l = log.New(f, "", 0)
}

func logf(level, format string, args ...interface{}) {
	mu.Lock()
	defer mu.Unlock()

	msg := fmt.Sprintf(format, args...)
	l.Printf("%s-%s-%s", time.Now().Format("2006-01-02 15:04:05"), level, msg)
}

// Info logs an informational message.
func Info(format string, args ...interface{}) {
	logf("INFO", format, args...)
}

// Warning logs a warning message.
func Warning(format string, args ...interface{}) {
	logf("WARNING", format, args...)
}

// Error logs an error message.
func Error(format string, args ...interface{}) {
	logf("ERROR", format, args...)
}

// Fatal logs a fatal message and exits the process.
func Fatal(format string, args ...interface{}) {
	logf("FATAL", format, args...)
	os.Exit(1)
}
