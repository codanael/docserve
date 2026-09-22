package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net"
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
	srv := NewServer(store, "test", Options{})
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
	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("expected X-Content-Type-Options 'nosniff', got %q", got)
	}
	if got := w.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("expected X-Frame-Options 'DENY', got %q", got)
	}
}

func TestMCPServerReadyz(t *testing.T) {
	store := setupTestStore(t)
	srv := NewServer(store, "test", Options{})
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
	srv := NewServer(store, "test", Options{})
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
	srv := NewServer(store, "test", Options{})
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

func TestMCPServerToolsListWithoutSession(t *testing.T) {
	store := setupTestStore(t)
	handler := NewServer(store, "test", Options{}).Handler()

	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 without Mcp-Session-Id, got %d: %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Mcp-Session-Id") != "" {
		t.Errorf("stateless server must not issue a session id")
	}
	var resp struct {
		Result struct {
			Tools []map[string]any `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Result.Tools) != 3 {
		t.Errorf("expected 3 tools, got %d", len(resp.Result.Tools))
	}
}

func TestMCPServerRejectsForeignOrigin(t *testing.T) {
	store := setupTestStore(t)
	handler := NewServer(store, "test", Options{}).Handler()

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}

	// Health endpoints are not subject to the Origin check.
	req = httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("Origin", "https://evil.example")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("healthz with foreign origin: expected 200, got %d", w.Code)
	}
}

func TestMCPServerBodyLimit(t *testing.T) {
	store := setupTestStore(t)
	handler := NewServer(store, "test", Options{}).Handler()

	huge := `{"jsonrpc":"2.0","id":1,"method":"ping","params":{"pad":"` + strings.Repeat("x", 2<<20) + `"}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(huge))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for oversized body, got %d", w.Code)
	}
}

// postMCP sends one JSON-RPC request to the handler and returns the decoded envelope.
func postMCP(t *testing.T, handler http.Handler, body string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var env map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v (%s)", err, w.Body.String())
	}
	return env
}

func TestMCPServerInstructions(t *testing.T) {
	handler := NewServer(setupTestStore(t), "test", Options{}).Handler()
	env := postMCP(t, handler, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`)
	result := env["result"].(map[string]any)
	instr, _ := result["instructions"].(string)
	if !strings.Contains(instr, "resolve-library") || !strings.Contains(instr, "get-library-docs") {
		t.Errorf("instructions should describe the workflow, got %q", instr)
	}
}

func TestMCPServerToolMetadata(t *testing.T) {
	handler := NewServer(setupTestStore(t), "test", Options{}).Handler()
	env := postMCP(t, handler, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
	tools := env["result"].(map[string]any)["tools"].([]any)

	byName := map[string]map[string]any{}
	for _, tl := range tools {
		tm := tl.(map[string]any)
		byName[tm["name"].(string)] = tm
	}

	for _, name := range []string{"list-libraries", "resolve-library", "get-library-docs"} {
		tm, ok := byName[name]
		if !ok {
			t.Fatalf("tool %s missing", name)
		}
		if title, _ := tm["title"].(string); title == "" {
			t.Errorf("%s: missing title", name)
		}
		ann, _ := tm["annotations"].(map[string]any)
		if ann["openWorldHint"] != false {
			t.Errorf("%s: openWorldHint = %v, want false", name, ann["openWorldHint"])
		}
		if ann["readOnlyHint"] != true || ann["destructiveHint"] != false || ann["idempotentHint"] != true {
			t.Errorf("%s: unexpected annotations %v", name, ann)
		}
		in, _ := tm["inputSchema"].(map[string]any)
		if in["additionalProperties"] != false {
			t.Errorf("%s: inputSchema.additionalProperties = %v, want false", name, in["additionalProperties"])
		}
	}

	for _, name := range []string{"list-libraries", "resolve-library"} {
		out, _ := byName[name]["outputSchema"].(map[string]any)
		if out["type"] != "object" {
			t.Errorf("%s: outputSchema.type = %v, want object", name, out["type"])
		}
		props, _ := out["properties"].(map[string]any)
		if len(props) == 0 {
			t.Errorf("%s: outputSchema has no properties", name)
		}
	}
	if _, has := byName["get-library-docs"]["outputSchema"]; has {
		t.Errorf("get-library-docs returns markdown text and must not declare an outputSchema")
	}

	props := byName["get-library-docs"]["inputSchema"].(map[string]any)["properties"].(map[string]any)
	mt := props["max_tokens"].(map[string]any)
	if mt["minimum"] != float64(1) {
		t.Errorf("max_tokens.minimum = %v, want 1", mt["minimum"])
	}
}

func TestMCPServerAuthProtectsOnlyMCP(t *testing.T) {
	handler := NewServer(setupTestStore(t), "test", Options{AuthToken: "tok"}).Handler()

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("/mcp without token: got %d, want 401", w.Code)
	}

	req.Header.Set("Authorization", "Bearer tok")
	req.Body = io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("/mcp with token: got %d, want 200: %s", w.Code, w.Body.String())
	}

	for _, path := range []string{"/healthz", "/readyz"} {
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code == http.StatusUnauthorized {
			t.Errorf("%s must not require auth", path)
		}
	}
}

func TestMCPServerInputValidation(t *testing.T) {
	handler := NewServer(setupTestStore(t), "test", Options{}).Handler()

	// Missing required argument → tool execution error (isError), not a JSON-RPC error.
	env := postMCP(t, handler, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"resolve-library","arguments":{}}}`)
	result, ok := env["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result envelope, got %v", env)
	}
	if result["isError"] != true {
		t.Errorf("expected isError=true, got %v", result)
	}

	// Unknown argument → rejected because additionalProperties is false.
	env = postMCP(t, handler, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"list-libraries","arguments":{"bogus":1}}}`)
	if result, _ := env["result"].(map[string]any); result["isError"] != true {
		t.Errorf("expected isError=true for unknown argument, got %v", env)
	}

	// Valid call still works.
	env = postMCP(t, handler, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"resolve-library","arguments":{"query":"spring"}}}`)
	if result, _ := env["result"].(map[string]any); result["isError"] == true {
		t.Errorf("valid call failed: %v", env)
	} else if sc, _ := result["structuredContent"].(map[string]any); sc["name"] != "spring-boot" {
		t.Errorf("structuredContent.name = %v", sc["name"])
	}
}

func TestMCPServerAllowsProxiedHostOnLoopback(t *testing.T) {
	handler := NewServer(setupTestStore(t), "test", Options{}).Handler()

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Host = "docs.example.com"
	// Simulate a reverse proxy connecting over the loopback interface.
	req = req.WithContext(context.WithValue(req.Context(), http.LocalAddrContextKey, &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8080}))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for non-loopback Host over loopback connection, got %d: %s", w.Code, w.Body.String())
	}
}

func TestMCPServerLogsToolCalls(t *testing.T) {
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })

	handler := NewServer(setupTestStore(t), "test", Options{}).Handler()
	postMCP(t, handler, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list-libraries","arguments":{}}}`)
	postMCP(t, handler, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"resolve-library","arguments":{"query":"nope"}}}`)

	logs := buf.String()
	if !strings.Contains(logs, "tool=list-libraries") || !strings.Contains(logs, "error=false") {
		t.Errorf("expected success log line, got:\n%s", logs)
	}
	if !strings.Contains(logs, "tool=resolve-library") || !strings.Contains(logs, "error=true") {
		t.Errorf("expected error log line, got:\n%s", logs)
	}
}
