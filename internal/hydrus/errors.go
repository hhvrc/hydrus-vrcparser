package hydrus

import (
	"fmt"
	"net/http"
	"strings"
)

// ConfigError means the client configuration is unusable (missing key,
// malformed address). Distinguished from transport failures because it is
// never worth retrying.
type ConfigError struct {
	Msg string
}

func (e *ConfigError) Error() string { return e.Msg }

// ConnectionError means Hydrus could not be reached at all, or did not answer
// within the per-attempt timeout on any attempt. The host reports it and fails
// the batch rather than exiting the process.
//
// A timeout is reported here, never as a context error: the host treats
// context cancellation as "stop the whole run", and a busy Hydrus is not that.
type ConnectionError struct {
	Address string
	// Timeout is set when the last attempt timed out rather than failed.
	Timeout bool
	Err     error
}

func (e *ConnectionError) Error() string {
	return fmt.Sprintf("could not reach Hydrus at %s: %v", e.Address, e.Err)
}

func (e *ConnectionError) Unwrap() error { return e.Err }

// APIError means Hydrus was reachable but rejected the request.
type APIError struct {
	StatusCode int
	// Body is the response body, or "" if it could not be read.
	Body string
	Path string
}

func (e *APIError) Error() string {
	hint := ""
	if e.StatusCode == http.StatusUnauthorized || e.StatusCode == http.StatusForbidden {
		hint = " (check the API key and that its permissions cover tag search/add)"
	}
	msg := fmt.Sprintf("Hydrus returned %d %s for %s%s", e.StatusCode, http.StatusText(e.StatusCode), e.Path, hint)
	if body := strings.TrimSpace(e.Body); body != "" {
		msg += ": " + body
	}
	return msg
}

// ServiceResolutionError means a local tag service could not be resolved
// unambiguously. Available lists what does exist, for the error message.
type ServiceResolutionError struct {
	Msg       string
	Available []string
}

func (e *ServiceResolutionError) Error() string {
	if len(e.Available) == 0 {
		return e.Msg
	}
	return e.Msg + "\nAvailable:\n  - " + strings.Join(e.Available, "\n  - ")
}
