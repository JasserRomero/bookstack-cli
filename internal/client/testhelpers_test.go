package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	testTokenID     = "tok-id"
	testTokenSecret = "s3cr3t-T0KEN-value"
	testVersion     = "1.2.3"
)

// sleeper records the waits requested by the client without waiting.
type sleeper struct {
	mu    sync.Mutex
	waits []time.Duration
}

func (s *sleeper) sleep(ctx context.Context, d time.Duration) error {
	s.mu.Lock()
	s.waits = append(s.waits, d)
	s.mu.Unlock()
	return ctx.Err()
}

func (s *sleeper) got() []time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]time.Duration(nil), s.waits...)
}

// recorder counts and keeps the requests received by a test server.
type recorder struct {
	mu   sync.Mutex
	reqs []*http.Request
}

func (r *recorder) add(req *http.Request) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reqs = append(r.reqs, req.Clone(context.Background()))
	return len(r.reqs)
}

func (r *recorder) all() []*http.Request {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]*http.Request(nil), r.reqs...)
}

// newTestClient starts an httptest server running h and returns a client
// pointed at it with an injected sleeper.
func newTestClient(t *testing.T, h func(w http.ResponseWriter, r *http.Request, n int)) (*Client, *recorder, *sleeper) {
	t.Helper()
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h(w, r, rec.add(r))
	}))
	t.Cleanup(srv.Close)
	sl := &sleeper{}
	c, err := New(Options{
		BaseURL:     srv.URL + "/",
		TokenID:     testTokenID,
		TokenSecret: testTokenSecret,
		Version:     testVersion,
		Sleep:       sl.sleep,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c, rec, sl
}

func writeJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

// asAPIError asserts err is an *APIError with the given code and status and
// that it does not leak the token secret.
func asAPIError(t *testing.T, err error, code string, status int) *APIError {
	t.Helper()
	if err == nil {
		t.Fatalf("expected %s error, got nil", code)
	}
	assertNoSecret(t, err)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.Code != code || apiErr.Status != status {
		t.Fatalf("got code=%q status=%d (%q), want code=%q status=%d", apiErr.Code, apiErr.Status, apiErr.Message, code, status)
	}
	return apiErr
}

func assertNoSecret(t *testing.T, err error) {
	t.Helper()
	for e := err; e != nil; e = errors.Unwrap(e) {
		if strings.Contains(e.Error(), testTokenSecret) {
			t.Fatalf("error leaks the token secret: %q", e.Error())
		}
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) && strings.Contains(apiErr.Message, testTokenSecret) {
		t.Fatalf("APIError.Message leaks the token secret: %q", apiErr.Message)
	}
}

func durations(ds ...int) []time.Duration {
	out := make([]time.Duration, len(ds))
	for i, d := range ds {
		out[i] = time.Duration(d) * time.Second
	}
	return out
}

func equalDurations(a, b []time.Duration) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
