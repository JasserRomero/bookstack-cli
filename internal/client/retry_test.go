package client

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRetry429HonorsRetryAfterThenSucceeds(t *testing.T) {
	c, rec, sl := newTestClient(t, func(w http.ResponseWriter, r *http.Request, n int) {
		if n == 1 {
			w.Header().Set("Retry-After", "7")
			writeJSON(w, 429, `{"error":{"message":"Too many"}}`)
			return
		}
		writeJSON(w, 200, `{"version":"v25.11.6"}`)
	})
	sys, err := c.System(context.Background())
	if err != nil {
		t.Fatalf("System: %v", err)
	}
	if sys.Version != "v25.11.6" {
		t.Errorf("Version = %q", sys.Version)
	}
	if got := len(rec.all()); got != 2 {
		t.Errorf("attempts = %d, want 2", got)
	}
	if got := sl.got(); !equalDurations(got, durations(7)) {
		t.Errorf("waits = %v, want [7s]", got)
	}
}

func TestRetry429OnNonGETResendsBody(t *testing.T) {
	var bodies []string
	c, rec, sl := newTestClient(t, func(w http.ResponseWriter, r *http.Request, n int) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		if n == 1 {
			writeJSON(w, 429, `{}`)
			return
		}
		writeJSON(w, 200, `{"id":1}`)
	})
	if _, err := c.do(context.Background(), http.MethodPut, "pages/1", nil, []byte(`{"name":"x"}`)); err != nil {
		t.Fatalf("do: %v", err)
	}
	if got := len(rec.all()); got != 2 {
		t.Errorf("attempts = %d, want 2", got)
	}
	if len(bodies) != 2 || bodies[0] != `{"name":"x"}` || bodies[1] != `{"name":"x"}` {
		t.Errorf("bodies = %q", bodies)
	}
	if got := rec.all()[0].Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}
	// Without Retry-After the exponential backoff applies.
	if got := sl.got(); !equalDurations(got, durations(1)) {
		t.Errorf("waits = %v, want [1s]", got)
	}
}

func TestRetry429ExhaustedIsRateLimited(t *testing.T) {
	c, rec, sl := newTestClient(t, func(w http.ResponseWriter, r *http.Request, n int) {
		writeJSON(w, 429, `{"error":{"message":"Too Many Attempts."}}`)
	})
	_, err := c.GetPage(context.Background(), 1)
	apiErr := asAPIError(t, err, CodeRateLimited, 429)
	if apiErr.Message != "Too Many Attempts." {
		t.Errorf("Message = %q", apiErr.Message)
	}
	if got := len(rec.all()); got != 5 {
		t.Errorf("attempts = %d, want 5", got)
	}
	if got := sl.got(); !equalDurations(got, durations(1, 2, 4, 8)) {
		t.Errorf("waits = %v, want [1s 2s 4s 8s]", got)
	}
}

func TestRetryAfterIsCappedAt30s(t *testing.T) {
	c, _, sl := newTestClient(t, func(w http.ResponseWriter, r *http.Request, n int) {
		if n == 1 {
			w.Header().Set("Retry-After", "3600")
			writeJSON(w, 429, `{}`)
			return
		}
		writeJSON(w, 200, `{}`)
	})
	if _, err := c.System(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := sl.got(); !equalDurations(got, durations(30)) {
		t.Errorf("waits = %v, want [30s]", got)
	}
}

func TestRetry5xxOnGETThenSucceeds(t *testing.T) {
	for _, status := range []int{502, 503, 504} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			c, rec, sl := newTestClient(t, func(w http.ResponseWriter, r *http.Request, n int) {
				if n < 3 {
					w.Header().Set("Content-Type", "text/html")
					w.WriteHeader(status)
					return
				}
				writeJSON(w, 200, `{"id":9,"name":"P"}`)
			})
			p, err := c.GetPage(context.Background(), 9)
			if err != nil {
				t.Fatalf("GetPage: %v", err)
			}
			if p.ID != 9 {
				t.Errorf("ID = %d", p.ID)
			}
			if got := len(rec.all()); got != 3 {
				t.Errorf("attempts = %d, want 3", got)
			}
			if got := sl.got(); !equalDurations(got, durations(1, 2)) {
				t.Errorf("waits = %v, want [1s 2s]", got)
			}
		})
	}
}

func TestRetry5xxOnGETExhaustedIsServer(t *testing.T) {
	c, rec, sl := newTestClient(t, func(w http.ResponseWriter, r *http.Request, n int) {
		w.WriteHeader(503)
	})
	_, err := c.GetPage(context.Background(), 1)
	asAPIError(t, err, CodeServer, 503)
	if got := len(rec.all()); got != 5 {
		t.Errorf("attempts = %d, want 5", got)
	}
	if got := sl.got(); !equalDurations(got, durations(1, 2, 4, 8)) {
		t.Errorf("waits = %v", got)
	}
}

func TestNoRetryFor500OnGET(t *testing.T) {
	c, rec, sl := newTestClient(t, func(w http.ResponseWriter, r *http.Request, n int) {
		w.WriteHeader(500)
	})
	_, err := c.GetPage(context.Background(), 1)
	asAPIError(t, err, CodeServer, 500)
	if got := len(rec.all()); got != 1 {
		t.Errorf("attempts = %d, want 1", got)
	}
	if got := sl.got(); len(got) != 0 {
		t.Errorf("waits = %v", got)
	}
}

func TestNoRetryFor503OnNonGET(t *testing.T) {
	c, rec, sl := newTestClient(t, func(w http.ResponseWriter, r *http.Request, n int) {
		w.WriteHeader(503)
	})
	_, err := c.do(context.Background(), http.MethodPost, "pages", nil, []byte(`{}`))
	asAPIError(t, err, CodeServer, 503)
	if got := len(rec.all()); got != 1 {
		t.Errorf("attempts = %d, want 1", got)
	}
	if got := sl.got(); len(got) != 0 {
		t.Errorf("waits = %v", got)
	}
}

// flakyListener drops the first n connections to simulate network errors.
type flakyListener struct {
	net.Listener
	drop int
}

func (l *flakyListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil || l.drop == 0 {
			return conn, err
		}
		l.drop--
		conn.Close()
	}
}

func newFlakyServer(t *testing.T, drop int, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewUnstartedServer(h)
	srv.Listener = &flakyListener{Listener: srv.Listener, drop: drop}
	srv.Start()
	t.Cleanup(srv.Close)
	return srv
}

func TestNetworkErrorOnGETRetriedThenSucceeds(t *testing.T) {
	srv := newFlakyServer(t, 2, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, `{"version":"v1"}`)
	})
	sl := &sleeper{}
	c, err := New(Options{BaseURL: srv.URL, TokenID: testTokenID, TokenSecret: testTokenSecret, Sleep: sl.sleep})
	if err != nil {
		t.Fatal(err)
	}
	sys, err := c.System(context.Background())
	if err != nil {
		t.Fatalf("System: %v", err)
	}
	if sys.Version != "v1" {
		t.Errorf("Version = %q", sys.Version)
	}
	if got := sl.got(); !equalDurations(got, durations(1, 2)) {
		t.Errorf("waits = %v, want [1s 2s]", got)
	}
}

func TestNetworkErrorOnNonGETNotRetried(t *testing.T) {
	srv := newFlakyServer(t, 1, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, `{}`)
	})
	sl := &sleeper{}
	c, err := New(Options{BaseURL: srv.URL, TokenID: testTokenID, TokenSecret: testTokenSecret, Sleep: sl.sleep})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.do(context.Background(), http.MethodPost, "pages", nil, []byte(`{}`))
	asAPIError(t, err, CodeNetwork, 0)
	if got := sl.got(); len(got) != 0 {
		t.Errorf("waits = %v, want none", got)
	}
}

func TestCancelDuringBackoffStopsPromptly(t *testing.T) {
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.add(r)
		w.WriteHeader(503)
	}))
	defer srv.Close()
	// Default (real) sleeper: the first backoff would be 1s.
	c, err := New(Options{BaseURL: srv.URL, TokenID: testTokenID, TokenSecret: testTokenSecret})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = c.GetPage(ctx, 1)
	if elapsed := time.Since(start); elapsed > 700*time.Millisecond {
		t.Errorf("returned after %v, want prompt return on cancellation", elapsed)
	}
	asAPIError(t, err, CodeNetwork, 0)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("errors.Is(err, DeadlineExceeded) = false for %v", err)
	}
	if got := len(rec.all()); got != 1 {
		t.Errorf("attempts = %d, want 1", got)
	}
}
