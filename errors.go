package nebo

import "errors"

var (
	// ErrNoSidecarToken is returned when NEBO_SIDECAR_TOKEN is not set.
	ErrNoSidecarToken = errors.New("NEBO_SIDECAR_TOKEN environment variable is not set")

	// ErrNoAppToken is returned when NEBO_APP_TOKEN is not set.
	ErrNoAppToken = errors.New("NEBO_APP_TOKEN environment variable is not set")

	// ErrNoHandlers is returned when Run() is called with no registered handlers.
	ErrNoHandlers = errors.New("no capability handlers registered — register at least one handler before calling Run()")
)
