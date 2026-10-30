package cli

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/JasserRomero/bookstack-cli/internal/client"
	"github.com/JasserRomero/bookstack-cli/internal/config"
)

// retrySleep is the client's wait between retries; nil uses the client
// default. Replaced in tests so retries do not wait.
var retrySleep func(ctx context.Context, d time.Duration) error

// exitCodesHelp is appended to the help of every command that calls the API.
const exitCodesHelp = `

Errors are JSON on stderr: {"error":{"code":"<code>","status":<http>,"message":"..."}};
status is present only when BookStack answered with an HTTP status.

Exit codes:
  0  ok
  1  error (config_missing, config_invalid, rate_limited, server, http_error,
     network, invalid_response, ambiguous, unsafe_write)
  2  usage (invalid arguments or flags; no request is made)
  3  auth: invalid token or missing permission (HTTP 401/403)
  4  not found (HTTP 404)
  6  validation (HTTP 422)`

// newAPIClient builds a client from the effective configuration of profile.
func newAPIClient(profile, version string) (*client.Client, *config.Config, error) {
	cfg, err := config.Load(profile)
	if err != nil {
		return nil, nil, err
	}
	c, err := client.New(client.Options{
		BaseURL:     cfg.URL,
		TokenID:     cfg.TokenID,
		TokenSecret: cfg.TokenSecret,
		Version:     version,
		Sleep:       retrySleep,
	})
	if err != nil {
		// Load guarantees the token values, so only the URL can be unusable.
		return nil, nil, &Error{
			Code:    CodeConfigInvalid,
			Message: fmt.Sprintf("invalid url %q: must be an absolute http or https URL", cfg.URL),
			Exit:    ExitError,
		}
	}
	return c, cfg, nil
}

const positiveIntegerMsg = "must be a positive integer"

func parsePositive(s string) (int, bool) {
	n, err := strconv.Atoi(s)
	return n, err == nil && n > 0
}

// parseID parses an <id> argument.
func parseID(s string) (int, error) {
	n, valid := parsePositive(s)
	if !valid {
		return 0, usageErrorf("invalid id %q: %s", s, positiveIntegerMsg)
	}
	return n, nil
}

// idFlag is a flag value holding a positive integer id; 0 means not set.
type idFlag int

func (f *idFlag) String() string {
	if *f == 0 {
		return ""
	}
	return strconv.Itoa(int(*f))
}

func (f *idFlag) Set(s string) error {
	n, valid := parsePositive(s)
	if !valid {
		return errors.New(positiveIntegerMsg)
	}
	*f = idFlag(n)
	return nil
}

func (f *idFlag) Type() string { return "id" }
