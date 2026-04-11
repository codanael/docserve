package index

import (
	"strings"
)

// MarkdownChunker splits Markdown files into chunks by heading hierarchy.
type MarkdownChunker struct{}

// Chunk implements Chunker for Markdown files.
func (m *MarkdownChunker) Chunk(filename string, content []byte) ([]Chunk, error) {
	if len(content) == 0 {
		return nil, nil
	}

	lines := strings.Split(string(content), "\n")
	lines = stripFrontmatter(lines)

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

		// Toggle code block state on lines starting with ```.
		if strings.HasPrefix(trimmed, "```") {
			inCodeBlock = !inCodeBlock
			sectionLines = append(sectionLines, line)
			continue
		}

		// Only detect headings outside code blocks.
		if !inCodeBlock {
			level, title := parseMarkdownHeading(line)
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

// parseMarkdownHeading parses a Markdown ATX heading line.
// Returns (level, title) where level is 1–6, or (0, "") if not a heading.
func parseMarkdownHeading(line string) (int, string) {
	if len(line) == 0 || line[0] != '#' {
		return 0, ""
	}
	level := 0
	for level < len(line) && line[level] == '#' {
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

// stripFrontmatter removes YAML front matter delimited by "---" at the start
// of the file. Returns the remaining lines.
func stripFrontmatter(lines []string) []string {
	if len(lines) == 0 {
		return lines
	}
	if strings.TrimSpace(lines[0]) != "---" {
		return lines
	}
	// Find closing "---".
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			return lines[i+1:]
		}
	}
	// No closing delimiter found — return as-is.
	return lines
}

// buildBreadcrumb joins non-empty heading stack entries with " > ".
func buildBreadcrumb(stack []string) string {
	var parts []string
	for _, s := range stack {
		if s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " > ")
}
