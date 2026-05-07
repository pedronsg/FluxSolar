// Package logger sends structured lines to stdout and optionally to logd via a Unix datagram socket.
// Import as github.com/OrbitOS-org/orbit-os-sdk-go/v26/logger; use alongside github.com/OrbitOS-org/orbit-os-sdk-go/v26/client on device apps.
package logger

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

// LogLevel is a log severity level.
type LogLevel int

// Standard log levels.
const (
	TRACE LogLevel = iota
	DEBUG
	INFO
	WARN
	ERROR
	FATAL
)

var levelLetters = [...]rune{'T', 'D', 'I', 'W', 'E', 'F'}

// Minimum level to print.
var logLevel LogLevel = TRACE

var (
	conn        *net.UnixConn
	mu          sync.Mutex
	printStdout = true
	socketPath  = "/tmp/logd.sock"
	AppName     = "void"
)

func Init(appName string, levelStr string, enableStdout bool) {
	SetAppName(appName)
	SetLevelStr(levelStr)
	SetPrintStdout(enableStdout)
}

func SetAppName(name string) {
	mu.Lock()
	defer mu.Unlock()
	AppName = name
}

func SetPrintStdout(enable bool) {
	printStdout = enable
}

// SetLevel sets the minimum log level.
// Example: logger.SetLevel(logger.WARN)
func SetLevel(level LogLevel) {
	logLevel = level
}

// SetLevelStr sets the minimum level from a string (TRACE, DEBUG, ...).
func SetLevelStr(levelStr string) {
	logLevel = getLogLevelFromString(strings.ToUpper(levelStr))
}

// GetLevel returns the configured minimum level.
func GetLevel() LogLevel {
	return logLevel
}

// Trace logs at TRACE.
func Trace(tag, text string) { printMessage(TRACE, tag, text) }

// Tracef logs at TRACE with formatting.
func Tracef(tag, format string, args ...any) {
	printMessage(TRACE, tag, fmt.Sprintf(format, args...))
}

// Debug logs at DEBUG.
func Debug(tag, text string) { printMessage(DEBUG, tag, text) }

// Debugf logs at DEBUG with formatting.
func Debugf(tag, format string, args ...any) {
	printMessage(DEBUG, tag, fmt.Sprintf(format, args...))
}

// Info logs at INFO.
func Info(tag, text string) { printMessage(INFO, tag, text) }

// Infof logs at INFO with formatting.
func Infof(tag, format string, args ...any) {
	printMessage(INFO, tag, fmt.Sprintf(format, args...))
}

// Warn logs at WARN.
func Warn(tag, text string) { printMessage(WARN, tag, text) }

// Warnf logs at WARN with formatting.
func Warnf(tag, format string, args ...any) {
	printMessage(WARN, tag, fmt.Sprintf(format, args...))
}

// Error logs at ERROR.
func Error(tag, text string) { printMessage(ERROR, tag, text) }

// Errorf logs at ERROR with formatting.
func Errorf(tag, format string, args ...any) {
	printMessage(ERROR, tag, fmt.Sprintf(format, args...))
}

// Fatal logs at FATAL.
func Fatal(tag, text string) { printMessage(FATAL, tag, text) }

// Fatalf logs at FATAL with formatting.
func Fatalf(tag, format string, args ...any) {
	printMessage(FATAL, tag, fmt.Sprintf(format, args...))
}

// buildLogLine builds a log line.
func buildLogLine(name string, level LogLevel, tag string, msg string) string {
	var sb strings.Builder
	sb.Grow(len(name) + 1 + len(tag) + len(msg))
	sb.WriteString(name)
	sb.WriteByte('|')
	sb.WriteByte(byte(getLogLevelLetter(level)))
	sb.WriteByte('|')
	sb.WriteString(tag)
	sb.WriteByte('|')
	sb.WriteString(msg)
	return sb.String()
}

// printMessage prints a message to stdout and logd.
func printMessage(level LogLevel, tag, text string) {
	mu.Lock()
	defer mu.Unlock()

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	if level >= logLevel {

		lines := strings.Split(text, "\n")
		for _, line := range lines {
			if printStdout {
				fmt.Printf("%s %c [%.17s]: %s\n", timestamp, getLogLevelLetter(level), tag, line)
			}

			tryConnect := func() {
				addr := net.UnixAddr{Name: socketPath, Net: "unixgram"}
				c, err := net.DialUnix("unixgram", nil, &addr)
				if err != nil {
					conn = nil
				} else {
					conn = c
				}
			}

			if conn == nil {
				tryConnect()
			}

			if conn != nil {
				lineStr := buildLogLine(AppName, level, tag, line)
				_, err := conn.Write([]byte(lineStr))
				if err != nil {
					fmt.Println("lost connection to logd, will retry...")
					conn.Close()
					conn = nil
				}
			}
		}
	}
}

// getLogLevelLetter converts a LogLevel to a letter.
func getLogLevelLetter(level LogLevel) rune {
	if int(level) < 0 || int(level) >= len(levelLetters) {
		return 'X'
	}
	return levelLetters[level]
}

// getLogLevelFromString converts a string to a LogLevel.
func getLogLevelFromString(strLevel string) LogLevel {
	ret := ERROR
	switch strLevel {
	case "TRACE":
		ret = TRACE
	case "DEBUG":
		ret = DEBUG
	case "INFO":
		ret = INFO
	case "WARN":
		ret = WARN
	case "ERROR":
		ret = ERROR
	case "FATAL":
		ret = FATAL
	}
	return ret
}
