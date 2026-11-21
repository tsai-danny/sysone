package systemone

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// Sentinel errors for local (pre-request) validation failures.
var (
	// ErrNoQuestions is returned when a request would carry no questions.
	ErrNoQuestions = errors.New("systemone: no questions defined")
	// ErrInvalidRequest is the sentinel for locally invalid requests
	// (empty state, empty choice criteria, score levels outside 2–10, …).
	ErrInvalidRequest = errors.New("systemone: invalid request")
)

// invalid builds an [ErrInvalidRequest]-wrapped error with an actionable Hint.
func invalid(msg, hint string) error {
	return fmt.Errorf("%w: %s\nHint: %s", ErrInvalidRequest, msg, hint)
}

// APIError is a remote error in OpenAI shape:
// {"error":{"code":400,"message":"...","type":"invalid_request_error"}}.
// Use errors.As to inspect it.
type APIError struct {
	Code    int    // HTTP status (also the wire "code")
	Message string // server message
	Type    string // e.g. "invalid_request_error", "not_supported_error"
}

func (e *APIError) Error() string {
	return fmt.Sprintf("systemone: API error %d (%s): %s\nHint: %s",
		e.Code, e.Type, e.Message, e.hint())
}

func (e *APIError) hint() string {
	switch {
	case e.Code == http.StatusServiceUnavailable:
		return "the model is still loading — retry in a few seconds, and check `sysone health`"
	case e.Code == http.StatusNotImplemented:
		return "the loaded model is not a decision model, or images are unsupported — start llama-server with a decision model (e.g. ggml-org/Julia-1-GGUF) and, for images, with a multimodal projector (--mmproj); check `sysone props` for vision support"
	case e.Code == http.StatusUnauthorized || e.Code == http.StatusForbidden:
		return "check --api-key / SYSTEMONE_API_KEY against the server's --api-key"
	case e.Code == http.StatusNotFound:
		return "check --url / SYSTEMONE_URL — expected the full endpoint, e.g. http://localhost:8080/v1/systemone"
	case e.Code >= 400 && e.Code < 500:
		return "fix the request per the message: state is required, questions must be non-empty, choice criteria non-empty, score criteria 2–10 levels, images must be data URLs"
	default:
		return "backend returned an error — check the server logs and `sysone health`"
	}
}

// IsLoading reports whether the error is a 503 (model still loading).
func (e *APIError) IsLoading() bool { return e.Code == http.StatusServiceUnavailable }

// IsContract reports whether the error is a backend 400/501 contract error.
func (e *APIError) IsContract() bool {
	return e.Code == http.StatusBadRequest || e.Code == http.StatusNotImplemented
}

// apiErrorBody is the OAI error envelope.
type apiErrorBody struct {
	Error struct {
		Code    any    `json:"code"` // number or string on the wire
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// parseAPIError builds an *APIError from a non-2xx response body.
// It never includes the request payload (no API keys, no images).
func parseAPIError(status int, body []byte) *APIError {
	e := &APIError{Code: status, Message: http.StatusText(status)}
	var env apiErrorBody
	if json.Unmarshal(body, &env) == nil && env.Error.Message != "" {
		e.Message = env.Error.Message
		e.Type = env.Error.Type
	}
	if e.Type == "" {
		switch status {
		case http.StatusBadRequest:
			e.Type = "invalid_request_error"
		case http.StatusNotImplemented:
			e.Type = "not_supported_error"
		default:
			e.Type = "server_error"
		}
	}
	return e
}
