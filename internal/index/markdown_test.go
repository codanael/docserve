package index

import (
	"os"
	"strings"
	"testing"
)

func TestMarkdownChunkerSimple(t *testing.T) {
	content, err := os.ReadFile("../../testdata/markdown/simple.md")
	if err != nil {
		t.Fatalf("read simple.md: %v", err)
	}

	c := &MarkdownChunker{}
	chunks, err := c.Chunk("docs/simple.md", content)
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
	if !strings.Contains(installChunk.Content, "```bash") {
		t.Errorf("installation chunk missing ```bash code block; content: %q", installChunk.Content)
	}
	if !strings.Contains(installChunk.Content, "go get github.com/codanael/docserve") {
		t.Errorf("installation chunk missing go get command; content: %q", installChunk.Content)
	}
}

func TestMarkdownChunkerFrontmatter(t *testing.T) {
	content, err := os.ReadFile("../../testdata/markdown/frontmatter.md")
	if err != nil {
		t.Fatalf("read frontmatter.md: %v", err)
	}

	c := &MarkdownChunker{}
	chunks, err := c.Chunk("docs/frontmatter.md", content)
	if err != nil {
		t.Fatalf("Chunk error: %v", err)
	}

	if len(chunks) == 0 {
		t.Fatal("expected chunks, got none")
	}

	// Front matter should not appear in any chunk content.
	for _, ch := range chunks {
		if strings.Contains(ch.Content, "title:") {
			t.Errorf("chunk content contains front matter 'title:': %q", ch.Content)
		}
		if strings.Contains(ch.Content, "version:") {
			t.Errorf("chunk content contains front matter 'version:': %q", ch.Content)
		}
	}

	// Verify expected breadcrumbs.
	breadcrumbs := make(map[string]bool)
	for _, ch := range chunks {
		breadcrumbs[ch.Breadcrumb] = true
	}
	if !breadcrumbs["API Reference"] {
		t.Errorf("missing breadcrumb 'API Reference'; got: %v", breadcrumbs)
	}
	if !breadcrumbs["API Reference > Authentication"] {
		t.Errorf("missing breadcrumb 'API Reference > Authentication'; got: %v", breadcrumbs)
	}
}

func TestMarkdownChunkerLargeSection(t *testing.T) {
	content, err := os.ReadFile("../../testdata/markdown/large.md")
	if err != nil {
		t.Fatalf("read large.md: %v", err)
	}

	c := &MarkdownChunker{}
	chunks, err := c.Chunk("docs/large.md", content)
	if err != nil {
		t.Fatalf("Chunk error: %v", err)
	}

	if len(chunks) < 2 {
		t.Fatalf("expected large section to be split into 2+ chunks, got %d", len(chunks))
	}

	// All chunks should have the "Large Section" breadcrumb.
	for _, ch := range chunks {
		if ch.Breadcrumb != "Large Section" {
			t.Errorf("unexpected breadcrumb %q (expected 'Large Section')", ch.Breadcrumb)
		}
		if len(ch.Content) > maxChunkBytes*2 {
			t.Errorf("chunk too large: %d bytes", len(ch.Content))
		}
	}
}

func TestMarkdownChunkerEmpty(t *testing.T) {
	c := &MarkdownChunker{}
	chunks, err := c.Chunk("empty.md", []byte{})
	if err != nil {
		t.Fatalf("Chunk error: %v", err)
	}
	if len(chunks) != 0 {
		t.Errorf("expected 0 chunks for empty input, got %d", len(chunks))
	}
}

func TestMarkdownChunkerNoHeadings(t *testing.T) {
	input := "This is plain text without any headings.\n\nJust paragraphs of content."
	c := &MarkdownChunker{}
	chunks, err := c.Chunk("plain.md", []byte(input))
	if err != nil {
		t.Fatalf("Chunk error: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk for no-heading input, got %d", len(chunks))
	}
	if chunks[0].Breadcrumb != "" {
		t.Errorf("expected empty breadcrumb, got %q", chunks[0].Breadcrumb)
	}
}
