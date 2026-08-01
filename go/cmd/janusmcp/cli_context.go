package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// cliContext creates a signal-aware context and applies the optional command
// timeout. The flag wins over JANUS_CLI_TIMEOUT; an empty value means no timeout.
func cliContext(flagValue string) (context.Context, context.CancelFunc, error) {
	raw := flagValue
	if raw == "" {
		raw = os.Getenv("JANUS_CLI_TIMEOUT")
	}
	parent, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	if raw == "" {
		return parent, stopSignals, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		stopSignals()
		return nil, nil, cliErrf(exitUsage, "invalid_timeout", "timeout must be a positive Go duration (for example 30s or 2m): %q", raw)
	}
	ctx, cancelTimeout := context.WithTimeout(parent, d)
	return ctx, func() {
		cancelTimeout()
		stopSignals()
	}, nil
}
