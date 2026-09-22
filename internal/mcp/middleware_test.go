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
