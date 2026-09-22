package mcp

import (
	"net/http"
	"net/url"
	"strings"
)

// maxBodyBytes caps the size of a single MCP request body.
const maxBodyBytes = 1 << 20

// originCheck rejects browser requests whose Origin header is neither a
// loopback origin nor in the allowed list, as required by the MCP Streamable
// HTTP transport specification. Requests without an Origin header (CLI and
// agent clients) pass through.
func originCheck(allowed []string, next http.Handler) http.Handler {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, o := range allowed {
		allowedSet[normalizeOrigin(o)] = struct{}{}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" || originAllowed(origin, allowedSet) {
			next.ServeHTTP(w, r)
			return
		}
		http.Error(w, "forbidden: origin not allowed", http.StatusForbidden)
	})
}

func originAllowed(origin string, allowed map[string]struct{}) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return false
	}
	if _, ok := allowed[normalizeOrigin(origin)]; ok {
		return true
	}
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

// normalizeOrigin lower-cases scheme and host and drops any path so that
// configured and received origins compare equal.
func normalizeOrigin(origin string) string {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return strings.ToLower(strings.TrimRight(origin, "/"))
	}
	return strings.ToLower(u.Scheme + "://" + u.Host)
}
