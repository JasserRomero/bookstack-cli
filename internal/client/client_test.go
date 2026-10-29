package client

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRequestSendsAuthAcceptAndUserAgent(t *testing.T) {
	c, rec, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request, n int) {
		writeJSON(w, 200, `{"version":"v25.11.6","instance_id":"abc","app_name":"Docs"}`)
	})
	if _, err := c.System(context.Background()); err != nil {
		t.Fatalf("System: %v", err)
	}
	reqs := rec.all()
	if len(reqs) != 1 {
		t.Fatalf("got %d requests, want 1", len(reqs))
	}
	r := reqs[0]
	if r.Method != http.MethodGet || r.URL.Path != "/api/system" {
		t.Errorf("got %s %s, want GET /api/system", r.Method, r.URL.Path)
	}
	if got, want := r.Header.Get("Authorization"), "Token "+testTokenID+":"+testTokenSecret; got != want {
		t.Errorf("Authorization = %q, want %q", got, want)
	}
	if got := r.Header.Get("Accept"); got != "application/json" {
		t.Errorf("Accept = %q, want application/json", got)
	}
	if got := r.Header.Get("User-Agent"); got != "bsk/"+testVersion {
		t.Errorf("User-Agent = %q, want bsk/%s", got, testVersion)
	}
}

func TestUserAgentDefaultsToDev(t *testing.T) {
	var ua string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua = r.Header.Get("User-Agent")
		writeJSON(w, 200, `{}`)
	}))
	defer srv.Close()
	c, err := New(Options{BaseURL: srv.URL, TokenID: testTokenID, TokenSecret: testTokenSecret})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.System(context.Background()); err != nil {
		t.Fatal(err)
	}
	if ua != "bsk/dev" {
		t.Errorf("User-Agent = %q, want bsk/dev", ua)
	}
}

func TestBaseURLWithPathPrefix(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		writeJSON(w, 200, `{}`)
	}))
	defer srv.Close()
	c, err := New(Options{BaseURL: srv.URL + "/wiki/", TokenID: testTokenID, TokenSecret: testTokenSecret})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.System(context.Background()); err != nil {
		t.Fatal(err)
	}
	if path != "/wiki/api/system" {
		t.Errorf("path = %q, want /wiki/api/system", path)
	}
}

func TestNewRejectsInvalidOptions(t *testing.T) {
	cases := map[string]Options{
		"empty url":     {TokenID: "a", TokenSecret: testTokenSecret},
		"relative url":  {BaseURL: "docs.example.com", TokenID: "a", TokenSecret: testTokenSecret},
		"ftp url":       {BaseURL: "ftp://docs.example.com", TokenID: "a", TokenSecret: testTokenSecret},
		"missing id":    {BaseURL: "https://docs.example.com", TokenSecret: testTokenSecret},
		"missing token": {BaseURL: "https://docs.example.com", TokenID: "a"},
	}
	for name, opts := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := New(opts)
			if err == nil {
				t.Fatal("expected error")
			}
			assertNoSecret(t, err)
		})
	}
}

func TestDefaultTimeoutIs30s(t *testing.T) {
	c, err := New(Options{BaseURL: "https://docs.example.com", TokenID: "a", TokenSecret: "b"})
	if err != nil {
		t.Fatal(err)
	}
	if c.timeout != 30*time.Second {
		t.Errorf("timeout = %v, want 30s", c.timeout)
	}
}

func TestPerAttemptTimeoutIsNetworkError(t *testing.T) {
	rec := &recorder{}
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.add(r)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer srv.Close()
	defer close(release)
	sl := &sleeper{}
	c, err := New(Options{BaseURL: srv.URL, TokenID: testTokenID, TokenSecret: testTokenSecret, Timeout: 20 * time.Millisecond, Sleep: sl.sleep})
	if err != nil {
		t.Fatal(err)
	}
	// A non-GET is not retried on network errors, so one attempt times out.
	_, err = c.do(context.Background(), http.MethodPost, "pages", nil, []byte(`{}`))
	apiErr := asAPIError(t, err, CodeNetwork, 0)
	if !strings.Contains(apiErr.Message, "timeout") && !strings.Contains(apiErr.Message, "deadline") {
		t.Errorf("message %q does not mention the timeout", apiErr.Message)
	}
	if got := len(rec.all()); got != 1 {
		t.Errorf("attempts = %d, want 1", got)
	}
}

func TestCanceledContextStopsBeforeRequest(t *testing.T) {
	c, rec, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request, n int) {
		writeJSON(w, 200, `{}`)
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.System(ctx)
	asAPIError(t, err, CodeNetwork, 0)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("errors.Is(err, context.Canceled) = false for %v", err)
	}
	if got := len(rec.all()); got != 0 {
		t.Errorf("requests = %d, want 0", got)
	}
}

func TestNetworkErrorDoesNotLeakSecret(t *testing.T) {
	// Reserve a port and close it so the connection is refused.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	sl := &sleeper{}
	// The secret also appears in the base URL to prove URL-bearing errors are redacted.
	c, err := New(Options{BaseURL: "http://" + addr + "/" + testTokenSecret, TokenID: testTokenID, TokenSecret: testTokenSecret, Sleep: sl.sleep})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.System(context.Background())
	apiErr := asAPIError(t, err, CodeNetwork, 0)
	if !strings.Contains(apiErr.Message, "connection refused") {
		t.Errorf("message %q does not report the transport failure", apiErr.Message)
	}
	// GET network errors are retried up to 5 attempts.
	if got := sl.got(); !equalDurations(got, durations(1, 2, 4, 8)) {
		t.Errorf("waits = %v, want [1s 2s 4s 8s]", got)
	}
}
