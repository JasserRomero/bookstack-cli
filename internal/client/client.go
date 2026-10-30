package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultTimeout  = 30 * time.Second
	defaultPageSize = 500 // the API maximum for count
	maxAttempts     = 5
	firstBackoff    = time.Second
	maxWait         = 30 * time.Second
	maxErrorBody    = 64 << 10
)

// Options configures a Client. BaseURL, TokenID and TokenSecret are required.
type Options struct {
	// BaseURL is the BookStack root URL; requests go to <BaseURL>/api/...
	BaseURL     string
	TokenID     string
	TokenSecret string
	// Version is sent in the User-Agent as bsk/<Version>; empty means "dev".
	Version string
	// Timeout bounds each attempt; zero means 30s.
	Timeout time.Duration
	// PageSize is the count used to paginate list endpoints; zero means 500.
	PageSize int
	// Sleep waits between retries; nil uses a timer that honors ctx. Tests
	// inject it so they do not wait.
	Sleep func(ctx context.Context, d time.Duration) error
}

// Client is a BookStack REST API client. It is safe for concurrent use.
type Client struct {
	baseURL   string
	auth      string
	secret    string
	userAgent string
	timeout   time.Duration
	pageSize  int
	sleep     func(ctx context.Context, d time.Duration) error
	http      *http.Client
}

// New validates opts and returns a Client.
func New(opts Options) (*Client, error) {
	u, err := url.Parse(opts.BaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, errors.New("client: base URL must be an absolute http or https URL")
	}
	if opts.TokenID == "" || opts.TokenSecret == "" {
		return nil, errors.New("client: token id and token secret are required")
	}
	c := &Client{
		baseURL:   strings.TrimRight(opts.BaseURL, "/"),
		auth:      "Token " + opts.TokenID + ":" + opts.TokenSecret,
		secret:    opts.TokenSecret,
		userAgent: "bsk/" + opts.Version,
		timeout:   opts.Timeout,
		pageSize:  opts.PageSize,
		sleep:     opts.Sleep,
		http:      &http.Client{},
	}
	if opts.Version == "" {
		c.userAgent = "bsk/dev"
	}
	if c.timeout <= 0 {
		c.timeout = defaultTimeout
	}
	if c.pageSize <= 0 || c.pageSize > defaultPageSize {
		c.pageSize = defaultPageSize
	}
	if c.sleep == nil {
		c.sleep = sleepContext
	}
	return c, nil
}

// response is a completed HTTP exchange. For error statuses body holds at
// most maxErrorBody bytes.
type response struct {
	status int
	header http.Header
	body   []byte
}

// do sends a request to /api/<path> and returns the 2xx response, retrying
// per policy: 429 for any method (honoring Retry-After), 502/503/504 and
// transport errors only for GET. Any other outcome is returned as *APIError.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body []byte) (*response, error) {
	target := c.baseURL + "/api/" + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	idempotent := method == http.MethodGet
	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, c.canceled(err)
		}
		res, err := c.attempt(ctx, method, target, body)
		last := attempt == maxAttempts
		wait := backoff(attempt)
		switch {
		case err != nil:
			if ctx.Err() != nil {
				return nil, c.canceled(ctx.Err())
			}
			if !idempotent || last {
				return nil, c.networkError(err)
			}
		case res.status == http.StatusTooManyRequests:
			if last {
				return nil, c.statusError(res)
			}
			if d, ok := retryAfter(res.header); ok {
				wait = d
			}
		case idempotent && retryableStatus(res.status):
			if last {
				return nil, c.statusError(res)
			}
		case res.status >= 200 && res.status < 300:
			return res, nil
		default:
			return nil, c.statusError(res)
		}
		if err := c.sleep(ctx, wait); err != nil {
			return nil, c.canceled(err)
		}
	}
}

// attempt performs one exchange bounded by the per-attempt timeout.
func (c *Client) attempt(ctx context.Context, method, target string, body []byte) (*response, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", c.auth)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var src io.Reader = resp.Body
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		src = io.LimitReader(resp.Body, maxErrorBody)
	}
	data, err := io.ReadAll(src)
	if err != nil {
		return nil, err
	}
	return &response{status: resp.StatusCode, header: resp.Header, body: data}, nil
}

// getJSON sends a GET and decodes the JSON response into v.
func (c *Client) getJSON(ctx context.Context, path string, query url.Values, v any) error {
	return c.sendJSON(ctx, http.MethodGet, path, query, nil, v)
}

// sendJSON sends a request with in (when non-nil) as its JSON body and
// decodes the JSON response into out.
func (c *Client) sendJSON(ctx context.Context, method, path string, query url.Values, in, out any) error {
	var body []byte
	if in != nil {
		// HTML content is sent as is, not as \u003c escapes.
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(in); err != nil {
			return &APIError{Code: CodeHTTPError, Message: c.redact(fmt.Sprintf("encode %s /api/%s body: %v", method, path, err)), Err: err}
		}
		body = bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
	}
	res, err := c.do(ctx, method, path, query, body)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(res.body, out); err != nil {
		return &APIError{
			Status:  res.status,
			Code:    CodeInvalidResponse,
			Message: c.redact(fmt.Sprintf("invalid JSON response from %s /api/%s: %v", method, path, err)),
		}
	}
	return nil
}

// backoff is the exponential wait after the given attempt: 1s, 2s, 4s...
func backoff(attempt int) time.Duration {
	d := firstBackoff << (attempt - 1)
	if d > maxWait || d <= 0 {
		return maxWait
	}
	return d
}

// retryAfter parses a Retry-After header in seconds, capped at maxWait.
func retryAfter(h http.Header) (time.Duration, bool) {
	secs, err := strconv.Atoi(strings.TrimSpace(h.Get("Retry-After")))
	if err != nil || secs < 0 {
		return 0, false
	}
	if d := time.Duration(secs) * time.Second; d < maxWait {
		return d, true
	}
	return maxWait, true
}

func retryableStatus(status int) bool {
	return status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout
}

func sleepContext(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// redact removes the token secret from s.
func (c *Client) redact(s string) string {
	if c.secret == "" {
		return s
	}
	return strings.ReplaceAll(s, c.secret, "[REDACTED]")
}
