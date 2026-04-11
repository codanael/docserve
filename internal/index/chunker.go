package index

import (
	"errors"
	"path/filepath"
	"strings"
)

const maxChunkBytes = 16000 // ~4000 tokens

// Chunker splits a file's content into indexable Chunks.
type Chunker interface {
	Chunk(filename string, content []byte) ([]Chunk, error)
}

// NewChunker returns the appropriate Chunker for the given filename.
func NewChunker(filename string) Chunker {
	switch filepath.Ext(filename) {
	case ".adoc", ".asciidoc":
		return &AsciidocChunker{}
	case ".md", ".mdx":
		return &MarkdownChunker{}
	default:
		return &PlainChunker{}
	}
}

// PlainChunker treats the entire file as a single chunk, splitting on paragraph
// boundaries if the content exceeds maxChunkBytes.
type PlainChunker struct{}

func (p *PlainChunker) Chunk(filename string, content []byte) ([]Chunk, error) {
	text := string(content)
	if text == "" {
		return nil, nil
	}
	parts := splitBySize(text, "", maxChunkBytes)
	chunks := make([]Chunk, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			continue
		}
		chunks = append(chunks, Chunk{
			Path:       filename,
			Breadcrumb: "",
			Content:    part,
		})
	}
	return chunks, nil
}

// AsciidocChunker is a placeholder — AsciiDoc support is not yet implemented.
type AsciidocChunker struct{}

func (a *AsciidocChunker) Chunk(filename string, content []byte) ([]Chunk, error) {
	return nil, errors.New("not yet implemented")
}

// splitBySize splits text into parts no larger than maxBytes, preferring to
// split on "\n\n" paragraph boundaries. If a single paragraph exceeds maxBytes
// it is emitted as its own (oversized) part.
func splitBySize(text, breadcrumb string, maxBytes int) []string {
	if len(text) <= maxBytes {
		return []string{text}
	}

	paragraphs := strings.Split(text, "\n\n")
	var parts []string
	current := strings.Builder{}

	for i, para := range paragraphs {
		sep := ""
		if i > 0 {
			sep = "\n\n"
		}
		candidate := sep + para
		if current.Len() > 0 && current.Len()+len(candidate) > maxBytes {
			// Flush current buffer.
			parts = append(parts, current.String())
			current.Reset()
			current.WriteString(para)
		} else {
			current.WriteString(candidate)
		}
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}
	return parts
}
