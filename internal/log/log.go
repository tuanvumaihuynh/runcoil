// Package log builds the slog logger of the binaries.
package log

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/lmittmann/tint"
)

// Config represents the logging configuration.
type Config struct {
	Format    Format     `yaml:"format"`
	Level     slog.Level `yaml:"level"`
	AddSource bool       `yaml:"add_source"`
}

// Validate checks the logging configuration. Every value is valid, so it returns nil.
func (l *Config) Validate() error {
	return nil
}

// Format is the output format of the logger.
type Format uint8

const (
	// FormatJSON writes one JSON object per line.
	FormatJSON Format = iota
	// FormatText writes colored text for a terminal.
	FormatText
)

// String implements flag.Value.
func (f Format) String() string {
	return []string{"JSON", "TEXT"}[f]
}

// Set implements flag.Value.
func (f Format) Set(s string) error {
	return f.UnmarshalText([]byte(s))
}

// UnmarshalText implements [encoding.TextUnmarshaler].
func (f *Format) UnmarshalText(text []byte) error {
	switch strings.ToUpper(string(text)) {
	case "JSON":
		*f = FormatJSON
	case "TEXT":
		*f = FormatText
	default:
		return fmt.Errorf("unknown log format: %s", text)
	}

	return nil
}

// MarshalText implements [encoding.TextMarshaler].
func (f Format) MarshalText() ([]byte, error) {
	return []byte(f.String()), nil
}

// NewLogger creates the logger from the configuration and makes it the slog default.
func NewLogger(cfg Config) (*slog.Logger, error) {
	var handler slog.Handler

	if cfg.Format == FormatJSON {
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level:     cfg.Level,
			AddSource: cfg.AddSource,
		})
	} else {
		handler = tint.NewHandler(os.Stdout, &tint.Options{
			Level:      cfg.Level,
			AddSource:  cfg.AddSource,
			TimeFormat: time.RFC3339,
			ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
				if a.Value.Kind() == slog.KindAny {
					if _, ok := a.Value.Any().(error); ok {
						return tint.Attr(9, a)
					}
				}

				return a
			},
		})
	}

	logger := slog.New(handler)
	slog.SetDefault(logger)

	return logger, nil
}
