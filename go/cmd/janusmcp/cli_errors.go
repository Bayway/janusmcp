package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	exitGeneric   = 1
	exitUsage     = 2
	exitSelection = 3
	exitAuth      = 4
	exitUpstream  = 5
	exitTool      = 6
	exitTimeout   = 124
	exitInterrupt = 130
)

// commandError carries the stable process exit code and machine-readable error
// kind used by the CLI. rendered is set when the command already emitted the
// complete JSON response and main must only exit with the requested status.
type commandError struct {
	code     int
	kind     string
	err      error
	json     bool
	rendered bool
}

func (e *commandError) Error() string { return e.err.Error() }
func (e *commandError) Unwrap() error { return e.err }

func cliErr(code int, kind string, err error) error {
	if err == nil {
		err = errors.New(kind)
	}
	return &commandError{code: code, kind: kind, err: err}
}

func cliErrf(code int, kind, format string, args ...any) error {
	return cliErr(code, kind, fmt.Errorf(format, args...))
}

func jsonErr(err error) error {
	var ce *commandError
	if errors.As(err, &ce) {
		clone := *ce
		clone.json = true
		return &clone
	}
	return &commandError{code: exitGeneric, kind: "internal_error", err: err, json: true}
}

func renderedErr(code int, kind string, err error) error {
	return &commandError{code: code, kind: kind, err: err, rendered: true}
}

func normalizeContextError(ctx context.Context, err error) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return cliErr(exitTimeout, "timeout", fmt.Errorf("operation timed out: %w", context.DeadlineExceeded))
	}
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
		return cliErr(exitInterrupt, "interrupted", errors.New("operation interrupted"))
	}
	return err
}

func classifyUpstreamError(kind string, err error) error {
	message := strings.ToLower(err.Error())
	for _, marker := range []string{"oauth", "authenticat", "authoriz", "credential", "token", "unauthorized", "forbidden", "status 401", "status 403"} {
		if strings.Contains(message, marker) {
			return cliErr(exitAuth, "authentication_error", err)
		}
	}
	return cliErr(exitUpstream, kind, err)
}

func renderCLIError(w io.Writer, err error) int {
	var ce *commandError
	if !errors.As(err, &ce) {
		fmt.Fprintf(w, "[janusmcp] error: %v\n", err)
		return exitGeneric
	}
	if ce.rendered {
		return ce.code
	}
	if ce.json {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": false,
			"error": map[string]string{
				"code":    ce.kind,
				"message": ce.err.Error(),
			},
		})
	} else {
		fmt.Fprintf(w, "[janusmcp] error: %v\n", ce.err)
	}
	return ce.code
}
