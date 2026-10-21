package nebo

import "errors"

var (
	// ErrNoToken is returned when NEBO_APP_TOKEN is not set.
	ErrNoToken = errors.New("NEBO_APP_TOKEN environment variable is not set")

	// ErrNoHandlers is returned when Run() is called with no registered handlers.
	ErrNoHandlers = errors.New("no capability handlers registered — register at least one handler before calling Run()")
)
