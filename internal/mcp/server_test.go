package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/codanael/docserve/internal/index"
)

func setupEmptyStore(t *testing.T) *index.Store {
	t.Helper()
	store, err := index.OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestMCPServerHealthz(t *testing.T) {
	store := setupTestStore(t)
	srv := NewServer(store, "test")
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
	if w.Body.String() != "ok" {
		t.Errorf("expected body 'ok', got %q", w.Body.String())
	}
}

func TestMCPServerReadyz(t *testing.T) {
	store := setupTestStore(t)
	srv := NewServer(store, "test")
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d (body: %s)", w.Code, w.Body.String())
	}
	if w.Body.String() != "ready" {
		t.Errorf("expected body 'ready', got %q", w.Body.String())
	}
}

func TestMCPServerReadyzEmpty(t *testing.T) {
	store := setupEmptyStore(t)
	srv := NewServer(store, "test")
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", w.Code)
	}
	if w.Body.String() != "not ready" {
		t.Errorf("expected body 'not ready', got %q", w.Body.String())
	}
}

func TestMCPServerInitialize(t *testing.T) {
	store := setupTestStore(t)
	srv := NewServer(store, "test")
	handler := srv.Handler()

	body := `{
		"jsonrpc": "2.0",
		"id": 1,
		"method": "initialize",
		"params": {
			"protocolVersion": "2025-03-26",
			"capabilities": {},
			"clientInfo": {"name": "test-client", "version": "1.0.0"}
		}
	}`

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d (body: %s)", w.Code, w.Body.String())
	}

	// Parse JSON-RPC response
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v (body: %s)", err, w.Body.String())
	}

	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result object in response, got: %v", resp)
	}

	serverInfo, ok := result["serverInfo"].(map[string]any)
	if !ok {
		t.Fatalf("expected serverInfo in result, got: %v", result)
	}

	if serverInfo["name"] != "docserve" {
		t.Errorf("expected serverInfo.name = 'docserve', got %q", serverInfo["name"])
	}
}
