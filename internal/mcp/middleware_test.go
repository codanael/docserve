package mcp

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestOriginCheck(t *testing.T) {
	h := originCheck([]string{"https://app.example.com"}, okHandler())

	cases := []struct {
		origin string
		want   int
	}{
		{"", http.StatusOK},
		{"http://localhost:3000", http.StatusOK},
		{"http://127.0.0.1", http.StatusOK},
		{"http://[::1]:8080", http.StatusOK},
		{"http://LOCALHOST:3000", http.StatusOK},
		{"http://[0:0:0:0:0:0:0:1]:8080", http.StatusOK},
		{"http://127.0.0.2", http.StatusOK},
		{"https://app.example.com", http.StatusOK},
		{"HTTPS://APP.EXAMPLE.COM", http.StatusOK},
		{"https://evil.example", http.StatusForbidden},
		{"http://localhost.evil.example", http.StatusForbidden},
		{"null", http.StatusForbidden},
		{"not a url", http.StatusForbidden},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		if tc.origin != "" {
			req.Header.Set("Origin", tc.origin)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != tc.want {
			t.Errorf("Origin %q: got %d, want %d", tc.origin, w.Code, tc.want)
		}
	}
}

func TestBearerAuth(t *testing.T) {
	h := bearerAuth("s3cret", okHandler())

	cases := []struct {
		header string
		want   int
	}{
		{"", http.StatusUnauthorized},
		{"Bearer wrong", http.StatusUnauthorized},
		{"Basic czNjcmV0", http.StatusUnauthorized},
		{"Bearer s3cret", http.StatusOK},
		{"bearer s3cret", http.StatusOK},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		if tc.header != "" {
			req.Header.Set("Authorization", tc.header)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != tc.want {
			t.Errorf("Authorization %q: got %d, want %d", tc.header, w.Code, tc.want)
		}
		if tc.want == http.StatusUnauthorized && w.Header().Get("WWW-Authenticate") == "" {
			t.Errorf("Authorization %q: missing WWW-Authenticate header", tc.header)
		}
	}
}

func TestBearerAuthDisabledWhenEmpty(t *testing.T) {
	h := bearerAuth("", okHandler())
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected passthrough with empty token, got %d", w.Code)
	}
}
