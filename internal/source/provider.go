package source

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/codanael/docserve/internal/config"
)

// Provider is the interface that all source providers must implement.
type Provider interface {
	Resolve(ctx context.Context, ref string) (string, error)
	Fetch(ctx context.Context, sha string, paths []string, destDir string) error
}

// NewProvider constructs the appropriate Provider for cfg.
func NewProvider(cfg config.SourceConfig, client *http.Client) (Provider, error) {
	switch cfg.Provider {
	case "github":
		return NewGitHubProvider(cfg, client), nil
	case "azure-devops":
		return NewAzureDevOpsProvider(cfg, client), nil
	case "confluence":
		return NewConfluenceProvider(cfg, client), nil
	default:
		return nil, fmt.Errorf("unknown provider: %s", cfg.Provider)
	}
}

// authTransport wraps an http.RoundTripper to inject auth headers.
type authTransport struct {
	base http.RoundTripper
	auth config.AuthConfig
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Clone the request to avoid mutating the original.
	r := req.Clone(req.Context())

	switch t.auth.Type {
	case "basic":
		username := os.Getenv(t.auth.UsernameEnv)
		password := os.Getenv(t.auth.PasswordEnv)
		if username != "" && password != "" {
			cred := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
			r.Header.Set("Authorization", "Basic "+cred)
		}
	case "bearer":
		if token := os.Getenv(t.auth.TokenEnv); token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
	default:
		// GitHub-style token auth (type is empty or unrecognised).
		if token := os.Getenv(t.auth.TokenEnv); token != "" {
			r.Header.Set("Authorization", "token "+token)
		}
	}

	return t.base.RoundTrip(r)
}

// WrapClientAuth returns a new *http.Client whose transport injects auth headers.
// If auth.TokenEnv, auth.UsernameEnv, and auth.PasswordEnv are all empty, the
// original client is returned unchanged.
func WrapClientAuth(client *http.Client, auth config.AuthConfig) *http.Client {
	if auth.TokenEnv == "" && auth.UsernameEnv == "" && auth.PasswordEnv == "" {
		return client
	}

	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}

	clone := *client
	clone.Transport = &authTransport{base: base, auth: auth}
	return &clone
}

// splitRepo splits "owner/repo" into its components.
func splitRepo(repo string) (owner, name string, err error) {
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("repo must be in owner/name format, got %q", repo)
	}
	return parts[0], parts[1], nil
}

