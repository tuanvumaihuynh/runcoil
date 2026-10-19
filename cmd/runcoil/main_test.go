package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The first signal leaves the drain to the manager. The second one exits with code 1.
func TestExitOnSecondSignal(t *testing.T) {
	signals := make(chan os.Signal, 2)
	codes := make(chan int, 1)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	go exitOnSecondSignal(context.Background(), logger, signals, func(code int) { codes <- code })

	signals <- syscall.SIGTERM
	select {
	case code := <-codes:
		require.Failf(t, "exit after one signal", "code %d", code)
	case <-time.After(100 * time.Millisecond):
	}

	signals <- syscall.SIGTERM
	select {
	case code := <-codes:
		require.Equal(t, 1, code)
	case <-time.After(time.Second):
		require.Fail(t, "no exit after the second signal")
	}
}
