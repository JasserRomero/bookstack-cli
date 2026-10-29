package client

import (
	"context"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestErrorMapping(t *testing.T) {
	cases := []struct {
		name        string
		status      int
		contentType string
		body        string
		code        string
		message     string
	}{
		{"401 json", 401, "application/json", `{"error":{"code":401,"message":"No authorization token found on the request"}}`, CodeAuth, "No authorization token found on the request"},
		{"403 json", 403, "application/json", `{"error":{"code":403,"message":"You do not have permission"}}`, CodeAuth, "You do not have permission"},
		{"404 json", 404, "application/json", `{"error":{"code":404,"message":"Page not found"}}`, CodeNotFound, "Page not found"},
		{"404 html", 404, "text/html", `<!DOCTYPE html><html><body><h1>Not Found</h1></body></html>`, CodeNotFound, "Not Found"},
		{"401 empty body", 401, "text/plain", ``, CodeAuth, "Unauthorized"},
		{"400 json", 400, "application/json", `{"error":{"code":400,"message":"Bad things"}}`, CodeHTTPError, "Bad things"},
		{"409 html", 409, "text/html", `<html>conflict</html>`, CodeHTTPError, "Conflict"},
		{"500 json", 500, "application/json", `{"error":{"code":500,"message":"Server Error"}}`, CodeServer, "Server Error"},
		{"501 html", 501, "text/html", `<html>oops</html>`, CodeServer, "Not Implemented"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, rec, sl := newTestClient(t, func(w http.ResponseWriter, r *http.Request, n int) {
				w.Header().Set("Content-Type", tc.contentType)
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			_, err := c.GetPage(context.Background(), 7)
			apiErr := asAPIError(t, err, tc.code, tc.status)
			if apiErr.Message != tc.message {
				t.Errorf("Message = %q, want %q", apiErr.Message, tc.message)
			}
			if apiErr.Validation != nil {
				t.Errorf("Validation = %v, want nil", apiErr.Validation)
			}
			if got := len(rec.all()); got != 1 {
				t.Errorf("attempts = %d, want 1", got)
			}
			if got := sl.got(); len(got) != 0 {
				t.Errorf("waits = %v, want none", got)
			}
			if err.Error() != tc.message {
				t.Errorf("Error() = %q, want %q", err.Error(), tc.message)
			}
		})
	}
}

func TestValidationErrorCarriesFieldDetails(t *testing.T) {
	c, _, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request, n int) {
		writeJSON(w, 422, `{"error":{"code":422,"message":"The given data was invalid.","validation":{"name":["The name field is required."],"book_id":["The book id field is required.","Must be an integer."]}}}`)
	})
	_, err := c.do(context.Background(), http.MethodPost, "pages", nil, []byte(`{}`))
	apiErr := asAPIError(t, err, CodeValidation, 422)
	want := map[string][]string{
		"name":    {"The name field is required."},
		"book_id": {"The book id field is required.", "Must be an integer."},
	}
	if !reflect.DeepEqual(apiErr.Validation, want) {
		t.Errorf("Validation = %v, want %v", apiErr.Validation, want)
	}
	wantMsg := "The given data was invalid. book_id: The book id field is required. Must be an integer.; name: The name field is required."
	if apiErr.Message != wantMsg {
		t.Errorf("Message = %q, want %q", apiErr.Message, wantMsg)
	}
}

func TestValidationToleratesStringMessages(t *testing.T) {
	c, _, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request, n int) {
		writeJSON(w, 422, `{"error":{"message":"Invalid.","validation":{"slug":"Taken."}}}`)
	})
	_, err := c.GetPage(context.Background(), 1)
	apiErr := asAPIError(t, err, CodeValidation, 422)
	if !reflect.DeepEqual(apiErr.Validation, map[string][]string{"slug": {"Taken."}}) {
		t.Errorf("Validation = %v", apiErr.Validation)
	}
}

func TestErrorBodyIsBounded(t *testing.T) {
	// A message longer than 64 KiB makes the JSON unparseable once truncated,
	// so the client falls back to the status text instead of reading it all.
	huge := strings.Repeat("x", 70<<10)
	c, _, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request, n int) {
		writeJSON(w, 404, `{"error":{"message":"`+huge+`"}}`)
	})
	_, err := c.GetPage(context.Background(), 1)
	apiErr := asAPIError(t, err, CodeNotFound, 404)
	if apiErr.Message != "Not Found" {
		t.Errorf("Message has %d bytes, want the status text", len(apiErr.Message))
	}
}

func TestErrorMessageRedactsSecret(t *testing.T) {
	c, _, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request, n int) {
		writeJSON(w, 401, `{"error":{"message":"bad token `+testTokenSecret+`"}}`)
	})
	_, err := c.GetPage(context.Background(), 1)
	apiErr := asAPIError(t, err, CodeAuth, 401)
	if apiErr.Message != "bad token [REDACTED]" {
		t.Errorf("Message = %q", apiErr.Message)
	}
}
