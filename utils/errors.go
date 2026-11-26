package utils

import "strings"

// IsNetworkConnectionError Network Connection Error
func IsNetworkConnectionError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()
	// Common network connection error patterns
	return strings.Contains(errStr, "use of closed network connection") ||
		strings.Contains(errStr, "connection reset by peer") ||
		strings.Contains(errStr, "connection closed during streaming") ||
		strings.Contains(errStr, "broken pipe") ||
		strings.Contains(errStr, "websocket: close")
}

// IsContextCanceled  Returns true if the context is canceled
func IsContextCanceled(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()
	// Common network connection error patterns
	return strings.Contains(errStr, "context canceled")
}
