package index

import (
	"strings"
)

// Chunk implements Chunker for AsciiDoc files.
func (a *AsciidocChunker) Chunk(filename string, content []byte) ([]Chunk, error) {
	if len(content) == 0 {
		return nil, nil
	}

	lines := strings.Split(string(content), "\n")

	var chunks []Chunk

	// headingStack holds the current heading titles for each level (index 0 = H1).
	var headingStack [6]string

	var sectionLines []string
	var currentBreadcrumb string
	inCodeBlock := false

	flush := func() {
		text := strings.TrimSpace(strings.Join(sectionLines, "\n"))
		if text == "" {
			sectionLines = sectionLines[:0]
			return
		}
		parts := splitBySize(text, currentBreadcrumb, maxChunkBytes)
		for _, part := range parts {
			if strings.TrimSpace(part) == "" {
				continue
			}
			chunks = append(chunks, Chunk{
				Path:       filename,
				Breadcrumb: currentBreadcrumb,
				Content:    part,
			})
		}
		sectionLines = sectionLines[:0]
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Toggle code block state on lines that are exactly "----".
		if trimmed == "----" {
			inCodeBlock = !inCodeBlock
			sectionLines = append(sectionLines, line)
			continue
		}

		// Only detect headings outside code blocks.
		if !inCodeBlock {
			level, title := parseAsciidocHeading(line)
			if level > 0 {
				flush()
				// Update heading stack: set this level, clear deeper levels.
				headingStack[level-1] = title
				for i := level; i < 6; i++ {
					headingStack[i] = ""
				}
				// Build breadcrumb from non-empty slots.
				currentBreadcrumb = buildBreadcrumb(headingStack[:])
				continue
			}
		}

		sectionLines = append(sectionLines, line)
	}

	flush()
	return chunks, nil
}

// parseAsciidocHeading parses an AsciiDoc section title line.
// Returns (level, title) where level is 1–6, or (0, "") if not a heading.
func parseAsciidocHeading(line string) (int, string) {
	if len(line) == 0 || line[0] != '=' {
		return 0, ""
	}
	level := 0
	for level < len(line) && line[level] == '=' {
		level++
	}
	if level > 6 {
		return 0, ""
	}
	if level >= len(line) || line[level] != ' ' {
		return 0, ""
	}
	title := strings.TrimSpace(line[level+1:])
	return level, title
}
