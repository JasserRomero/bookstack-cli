package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/JasserRomero/bookstack-cli/internal/config"
	"github.com/spf13/cobra"
)

// Exit codes (umbrella S5).
const (
	ExitOK    = 0
	ExitError = 1
	ExitUsage = 2
)

// Error codes reported in the JSON error output.
const (
	CodeError         = "error"
	CodeUsage         = "usage"
	CodeConfigMissing = "config_missing"
	CodeConfigInvalid = "config_invalid"
)

// Error is a command failure with its JSON error code and process exit code.
type Error struct {
	Code    string
	Message string
	Exit    int
}

func (e *Error) Error() string { return e.Message }

func usageErrorf(format string, args ...any) *Error {
	return &Error{Code: CodeUsage, Message: fmt.Sprintf(format, args...), Exit: ExitUsage}
}

// Execute runs the root command. On failure it writes the JSON error to the
// command's stderr and returns an *Error carrying the exit code.
//
// Classification is applied here, to the whole command tree, so a command
// never has to remember a wrapper: flag and argument errors are usage errors
// (exit 2), and errors returned by command bodies go through classify.
func Execute(cmd *cobra.Command) error {
	classifyTree(cmd.Root())
	err := cmd.Execute()
	if err == nil {
		return nil
	}
	e := classify(err)
	writeError(cmd.ErrOrStderr(), e)
	return e
}

func classifyTree(c *cobra.Command) {
	c.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return &Error{Code: CodeUsage, Message: err.Error(), Exit: ExitUsage}
	})
	if args := c.Args; args != nil {
		c.Args = func(cmd *cobra.Command, a []string) error {
			if err := args(cmd, a); err != nil {
				return &Error{Code: CodeUsage, Message: err.Error(), Exit: ExitUsage}
			}
			return nil
		}
	}
	for _, sub := range c.Commands() {
		classifyTree(sub)
	}
}

// ExitCode returns the process exit code for an error returned by Execute.
func ExitCode(err error) int {
	if err == nil {
		return ExitOK
	}
	var e *Error
	if errors.As(err, &e) {
		return e.Exit
	}
	return ExitError
}

func classify(err error) *Error {
	var (
		e       *Error
		unknown *config.UnknownProfileError
		missing *config.MissingError
		invalid *config.InvalidError
	)
	switch {
	case errors.As(err, &e):
		return e
	case errors.As(err, &unknown):
		return &Error{Code: CodeUsage, Message: err.Error(), Exit: ExitUsage}
	case errors.As(err, &missing):
		return &Error{Code: CodeConfigMissing, Message: err.Error(), Exit: ExitError}
	case errors.As(err, &invalid):
		return &Error{Code: CodeConfigInvalid, Message: err.Error(), Exit: ExitError}
	default:
		return &Error{Code: CodeError, Message: err.Error(), Exit: ExitError}
	}
}

type errorBody struct {
	Code    string `json:"code"`
	Status  int    `json:"status,omitempty"`
	Message string `json:"message"`
}

func writeError(w io.Writer, e *Error) {
	_ = writeJSON(w, struct {
		Error errorBody `json:"error"`
	}{errorBody{Code: e.Code, Message: e.Message}})
}

// writeJSON writes v as one line of JSON.
func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}
