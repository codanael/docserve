package index

import (
	"os"
	"strings"
	"testing"
)

func TestAsciidocChunkerSimple(t *testing.T) {
	content, err := os.ReadFile("../../testdata/asciidoc/simple.adoc")
	if err != nil {
		t.Fatalf("read simple.adoc: %v", err)
	}

	c := &AsciidocChunker{}
	chunks, err := c.Chunk("docs/simple.adoc", content)
	if err != nil {
		t.Fatalf("Chunk error: %v", err)
	}

	if len(chunks) < 4 {
		t.Fatalf("expected at least 4 chunks, got %d", len(chunks))
	}

	// Collect breadcrumbs for assertion.
	breadcrumbs := make(map[string]bool)
	for _, ch := range chunks {
		breadcrumbs[ch.Breadcrumb] = true
	}

	expected := []string{
		"Getting Started",
		"Getting Started > Installation",
		"Getting Started > Configuration > Database",
		"Getting Started > Configuration > Cache",
	}
	for _, want := range expected {
		if !breadcrumbs[want] {
			t.Errorf("missing breadcrumb %q; got breadcrumbs: %v", want, breadcrumbs)
		}
	}

	// Verify that code block content is preserved in the Installation chunk.
	var installChunk *Chunk
	for i := range chunks {
		if chunks[i].Breadcrumb == "Getting Started > Installation" {
			installChunk = &chunks[i]
			break
		}
	}
	if installChunk == nil {
		t.Fatal("no chunk with breadcrumb 'Getting Started > Installation'")
	}
	if !strings.Contains(installChunk.Content, "----") {
		t.Errorf("installation chunk missing ---- code block delimiter; content: %q", installChunk.Content)
	}
	if !strings.Contains(installChunk.Content, "mvn install mylib") {
		t.Errorf("installation chunk missing mvn install command; content: %q", installChunk.Content)
	}
}

func TestAsciidocChunkerAdmonitions(t *testing.T) {
	content, err := os.ReadFile("../../testdata/asciidoc/admonitions.adoc")
	if err != nil {
		t.Fatalf("read admonitions.adoc: %v", err)
	}

	c := &AsciidocChunker{}
	chunks, err := c.Chunk("docs/admonitions.adoc", content)
	if err != nil {
		t.Fatalf("Chunk error: %v", err)
	}

	// Find Authentication chunk and verify admonitions are included.
	var authChunk *Chunk
	for i := range chunks {
		if chunks[i].Breadcrumb == "Security Guide > Authentication" {
			authChunk = &chunks[i]
			break
		}
	}
	if authChunk == nil {
		t.Fatal("no chunk with breadcrumb 'Security Guide > Authentication'")
	}
	if !strings.Contains(authChunk.Content, "NOTE:") {
		t.Errorf("authentication chunk missing NOTE admonition; content: %q", authChunk.Content)
	}
	if !strings.Contains(authChunk.Content, "TIP:") {
		t.Errorf("authentication chunk missing TIP admonition; content: %q", authChunk.Content)
	}

	// Find Authorization chunk and verify admonitions are included.
	var authzChunk *Chunk
	for i := range chunks {
		if chunks[i].Breadcrumb == "Security Guide > Authorization" {
			authzChunk = &chunks[i]
			break
		}
	}
	if authzChunk == nil {
		t.Fatal("no chunk with breadcrumb 'Security Guide > Authorization'")
	}
	if !strings.Contains(authzChunk.Content, "WARNING:") {
		t.Errorf("authorization chunk missing WARNING admonition; content: %q", authzChunk.Content)
	}
	if !strings.Contains(authzChunk.Content, "IMPORTANT:") {
		t.Errorf("authorization chunk missing IMPORTANT admonition; content: %q", authzChunk.Content)
	}
}

func TestAsciidocChunkerEmpty(t *testing.T) {
	c := &AsciidocChunker{}
	chunks, err := c.Chunk("empty.adoc", []byte{})
	if err != nil {
		t.Fatalf("Chunk error: %v", err)
	}
	if len(chunks) != 0 {
		t.Errorf("expected 0 chunks for empty input, got %d", len(chunks))
	}
}
