package logger

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
	LevelFatal
)

var levelNames = map[Level]string{
	LevelDebug: "DEBUG",
	LevelInfo:  "INFO",
	LevelWarn:  "WARN",
	LevelError: "ERROR",
	LevelFatal: "FATAL",
}

var levelColors = map[Level]string{
	LevelDebug: "\033[36m", // Cyan
	LevelInfo:  "\033[32m", // Green
	LevelWarn:  "\033[33m", // Yellow
	LevelError: "\033[31m", // Red
	LevelFatal: "\033[35m", // Magenta
}

const colorReset = "\033[0m"

type Logger struct {
	level      Level
	output     io.Writer
	useColors  bool
	timeFormat string
}

var defaultLogger = &Logger{
	level:      LevelInfo,
	output:     os.Stderr,
	useColors:  true,
	timeFormat: "15:04:05",
}

func New(level Level) *Logger {
	return &Logger{
		level:      level,
		output:     os.Stderr,
		useColors:  true,
		timeFormat: "15:04:05",
	}
}

func (l *Logger) SetLevel(level Level) {
	l.level = level
}

func (l *Logger) SetOutput(w io.Writer) {
	l.output = w
}

func (l *Logger) SetColors(enabled bool) {
	l.useColors = enabled
}

func (l *Logger) log(level Level, format string, args ...interface{}) {
	if level < l.level {
		return
	}

	timestamp := time.Now().Format(l.timeFormat)
	levelName := levelNames[level]
	message := fmt.Sprintf(format, args...)

	var output string
	if l.useColors {
		color := levelColors[level]
		output = fmt.Sprintf("%s [%s%s%s] %s\n", timestamp, color, levelName, colorReset, message)
	} else {
		output = fmt.Sprintf("%s [%s] %s\n", timestamp, levelName, message)
	}

	fmt.Fprint(l.output, output)

	if level == LevelFatal {
		os.Exit(1)
	}
}

func (l *Logger) Debug(format string, args ...interface{}) {
	l.log(LevelDebug, format, args...)
}

func (l *Logger) Info(format string, args ...interface{}) {
	l.log(LevelInfo, format, args...)
}

func (l *Logger) Warn(format string, args ...interface{}) {
	l.log(LevelWarn, format, args...)
}

func (l *Logger) Error(format string, args ...interface{}) {
	l.log(LevelError, format, args...)
}

func (l *Logger) Fatal(format string, args ...interface{}) {
	l.log(LevelFatal, format, args...)
}

// Global logger functions
func SetLevel(level Level) {
	defaultLogger.SetLevel(level)
}

func SetOutput(w io.Writer) {
	defaultLogger.SetOutput(w)
}

func SetColors(enabled bool) {
	defaultLogger.SetColors(enabled)
}

func Debug(format string, args ...interface{}) {
	defaultLogger.Debug(format, args...)
}

func Info(format string, args ...interface{}) {
	defaultLogger.Info(format, args...)
}

func Warn(format string, args ...interface{}) {
	defaultLogger.Warn(format, args...)
}

func Error(format string, args ...interface{}) {
	defaultLogger.Error(format, args...)
}

func Fatal(format string, args ...interface{}) {
	defaultLogger.Fatal(format, args...)
}

// ParseLevel converts a string to a log level
func ParseLevel(s string) (Level, error) {
	switch strings.ToLower(s) {
	case "debug":
		return LevelDebug, nil
	case "info":
		return LevelInfo, nil
	case "warn", "warning":
		return LevelWarn, nil
	case "error":
		return LevelError, nil
	case "fatal":
		return LevelFatal, nil
	default:
		return LevelInfo, fmt.Errorf("invalid log level: %s", s)
	}
}