//go:build integration

package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/codanael/docserve/internal/index"
	mcpsrv "github.com/codanael/docserve/internal/mcp"
)

const sessionIDHeader = "Mcp-Session-Id"

// jsonRPC sends a JSON-RPC request with an id field and returns the session ID from the response header.
func jsonRPC(t *testing.T, baseURL string, id int, method string, params map[string]any) string {
	t.Helper()

	body := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("jsonRPC marshal: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, baseURL+"/mcp", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("jsonRPC new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("jsonRPC do: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("jsonRPC %s: status %d, body: %s", method, resp.StatusCode, body)
	}

	return resp.Header.Get(sessionIDHeader)
}

// jsonRPCWithSession sends a JSON-RPC request with a session ID header and returns the result map.
func jsonRPCWithSession(t *testing.T, baseURL string, sessionID string, id int, method string, params map[string]any) map[string]any {
	t.Helper()

	body := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("jsonRPCWithSession marshal: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, baseURL+"/mcp", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("jsonRPCWithSession new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if sessionID != "" {
		req.Header.Set(sessionIDHeader, sessionID)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("jsonRPCWithSession do: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("jsonRPCWithSession %s: status %d, body: %s", method, resp.StatusCode, body)
	}

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("jsonRPCWithSession read body: %v", err)
	}

	var envelope map[string]any
	if err := json.Unmarshal(responseBody, &envelope); err != nil {
		t.Fatalf("jsonRPCWithSession unmarshal: %v (body: %s)", err, responseBody)
	}

	result, ok := envelope["result"].(map[string]any)
	if !ok {
		t.Fatalf("jsonRPCWithSession %s: expected result map in response, got: %v", method, envelope)
	}
	return result
}

// sendNotification sends a JSON-RPC notification (no id field) to the server.
func sendNotification(t *testing.T, baseURL string, sessionID string, method string, params map[string]any) {
	t.Helper()

	body := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	}
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("sendNotification marshal: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, baseURL+"/mcp", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("sendNotification new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if sessionID != "" {
		req.Header.Set(sessionIDHeader, sessionID)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("sendNotification do: %v", err)
	}
	defer resp.Body.Close()
	_, _ = io.ReadAll(resp.Body)
}

func TestMCPProtocolFlow(t *testing.T) {
	// 1. Setup: create in-memory store, add a library with chunks.
	store, err := index.OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()

	libID, err := store.UpsertLibrary(index.Library{
		Name:      "testlib",
		Repo:      "example/testlib",
		Ref:       "main",
		CommitSHA: "abc123",
		FetchedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("UpsertLibrary: %v", err)
	}

	err = store.ReplaceChunks(libID, []index.Chunk{
		{
			Path:       "docs/intro.md",
			Breadcrumb: "Introduction",
			Content:    "This is the introduction to testlib. It covers installation and setup.",
		},
		{
			Path:       "docs/api.md",
			Breadcrumb: "API Reference",
			Content:    "The testlib API provides functions for data processing and transformation.",
		},
	})
	if err != nil {
		t.Fatalf("ReplaceChunks: %v", err)
	}

	// 2. Create MCP server + httptest server.
	srv := mcpsrv.NewServer(store, "test")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 3. Initialize: POST /mcp with initialize request, capture session ID.
	sessionID := jsonRPC(t, ts.URL, 1, "initialize", map[string]any{
		"protocolVersion": "2025-03-26",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "integration-test-client",
			"version": "1.0.0",
		},
	})

	// 4. Send initialized notification with session ID.
	sendNotification(t, ts.URL, sessionID, "notifications/initialized", map[string]any{})

	// 5. List tools: POST tools/list → verify 3 tools.
	toolsResult := jsonRPCWithSession(t, ts.URL, sessionID, 2, "tools/list", map[string]any{})

	tools, ok := toolsResult["tools"].([]any)
	if !ok {
		t.Fatalf("expected tools array in result, got: %v", toolsResult)
	}
	if len(tools) != 3 {
		t.Errorf("expected 3 tools, got %d: %v", len(tools), tools)
	}

	toolNames := make([]string, 0, len(tools))
	for _, tool := range tools {
		if tm, ok := tool.(map[string]any); ok {
			if name, ok := tm["name"].(string); ok {
				toolNames = append(toolNames, name)
			}
		}
	}
	t.Logf("tools: %v", toolNames)

	// 6. Call list-libraries → verify testlib present.
	listResult := jsonRPCWithSession(t, ts.URL, sessionID, 3, "tools/call", map[string]any{
		"name":      "list-libraries",
		"arguments": map[string]any{},
	})

	content, ok := listResult["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("expected content in list-libraries result, got: %v", listResult)
	}

	firstContent, ok := content[0].(map[string]any)
	if !ok {
		t.Fatalf("expected content[0] to be a map, got: %v", content[0])
	}

	text, _ := firstContent["text"].(string)
	if !strings.Contains(text, "testlib") {
		t.Errorf("expected list-libraries result to contain 'testlib', got: %s", text)
	}

	// 7. Call get-library-docs with query → verify results contain expected content.
	docsResult := jsonRPCWithSession(t, ts.URL, sessionID, 4, "tools/call", map[string]any{
		"name": "get-library-docs",
		"arguments": map[string]any{
			"library": "testlib",
			"query":   "installation setup",
		},
	})

	docsContent, ok := docsResult["content"].([]any)
	if !ok || len(docsContent) == 0 {
		t.Fatalf("expected content in get-library-docs result, got: %v", docsResult)
	}

	docsFirstContent, ok := docsContent[0].(map[string]any)
	if !ok {
		t.Fatalf("expected docsContent[0] to be a map, got: %v", docsContent[0])
	}

	docsText, _ := docsFirstContent["text"].(string)
	if !strings.Contains(strings.ToLower(docsText), "introduction") &&
		!strings.Contains(strings.ToLower(docsText), "installation") &&
		!strings.Contains(strings.ToLower(docsText), "testlib") {
		t.Errorf("expected get-library-docs result to contain relevant content, got: %s", docsText)
	}

	t.Logf("get-library-docs result snippet: %.200s", docsText)
}
