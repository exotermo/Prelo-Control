package gateway

import "fmt"

// CallError mirrors GatewayCallException: status carries the HTTP status to report upstream
// (the Gateway's own status on a rejection, 504 on timeout, 502 on any other transport
// failure), and Message is the fixed, non-leaking description used across all three cases.
type CallError struct {
	Status  int
	Message string
	Err     error
}

func (e *CallError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	}
	return e.Message
}

func (e *CallError) Unwrap() error { return e.Err }
