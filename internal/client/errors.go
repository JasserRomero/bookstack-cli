package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// Error codes of APIError. The CLI maps them to exit codes.
const (
	CodeAuth        = "auth"         // 401, 403
	CodeNotFound    = "not_found"    // 404
	CodeValidation  = "validation"   // 422
	CodeRateLimited = "rate_limited" // 429 after retries
	CodeServer      = "server"       // 5xx
	CodeHTTPError   = "http_error"   // any other non-2xx status
	CodeNetwork     = "network"      // transport failure, timeout or cancellation
	// CodeInvalidResponse reports a 2xx response whose body is not the
	// expected JSON, such as an HTML login page served by a proxy.
	CodeInvalidResponse = "invalid_response"
)

// APIError is the error returned by every Client method. It never contains
// the token secret.
type APIError struct {
	// Status is the HTTP status, or 0 when no response was received.
	Status  int
	Code    string
	Message string
	// Validation maps field names to messages for 422 responses.
	Validation map[string][]string
	// Err is the underlying transport or context error, if any.
	Err error
}

func (e *APIError) Error() string { return e.Message }

func (e *APIError) Unwrap() error { return e.Err }

// statusError maps a non-2xx response to an *APIError.
func (c *Client) statusError(res *response) *APIError {
	e := &APIError{Status: res.status, Code: codeForStatus(res.status)}
	var body struct {
		Error struct {
			Message    string                     `json:"message"`
			Validation map[string]json.RawMessage `json:"validation"`
		} `json:"error"`
	}
	if json.Unmarshal(res.body, &body) == nil {
		e.Message = body.Error.Message
		e.Validation = parseValidation(body.Error.Validation)
	}
	if e.Message == "" {
		e.Message = http.StatusText(res.status)
	}
	if e.Message == "" {
		e.Message = fmt.Sprintf("HTTP %d", res.status)
	}
	if len(e.Validation) > 0 {
		e.Message += " " + renderValidation(e.Validation)
	}
	e.Message = c.redact(e.Message)
	return e
}

func codeForStatus(status int) string {
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return CodeAuth
	case status == http.StatusNotFound:
		return CodeNotFound
	case status == http.StatusUnprocessableEntity:
		return CodeValidation
	case status == http.StatusTooManyRequests:
		return CodeRateLimited
	case status >= 500:
		return CodeServer
	default:
		return CodeHTTPError
	}
}

// parseValidation accepts a list of messages or a single message per field.
func parseValidation(raw map[string]json.RawMessage) map[string][]string {
	if len(raw) == 0 {
		return nil
	}
	out := make(map[string][]string, len(raw))
	for field, v := range raw {
		var msgs []string
		if json.Unmarshal(v, &msgs) != nil {
			var msg string
			if json.Unmarshal(v, &msg) != nil {
				msg = string(v)
			}
			msgs = []string{msg}
		}
		out[field] = msgs
	}
	return out
}

// renderValidation renders "field: msg msg; field: msg" sorted by field.
func renderValidation(v map[string][]string) string {
	fields := make([]string, 0, len(v))
	for f := range v {
		fields = append(fields, f)
	}
	sort.Strings(fields)
	parts := make([]string, len(fields))
	for i, f := range fields {
		parts[i] = f + ": " + strings.Join(v[f], " ")
	}
	return strings.Join(parts, "; ")
}

// networkError wraps a transport failure. The *url.Error layer is dropped
// because its text carries the request URL.
func (c *Client) networkError(err error) *APIError {
	var uerr *url.Error
	if errors.As(err, &uerr) {
		err = uerr.Err
	}
	return &APIError{Code: CodeNetwork, Message: c.redact("request failed: " + err.Error()), Err: err}
}

// canceled reports that the caller's context ended the request.
func (c *Client) canceled(err error) *APIError {
	return &APIError{Code: CodeNetwork, Message: "request canceled: " + err.Error(), Err: err}
}
