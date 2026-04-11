# Confluence Data Center Provider Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a Confluence Data Center provider that fetches page trees, converts XHTML storage format to markdown, and feeds into the existing indexation pipeline.

**Architecture:** New provider implementing the `Provider` interface (Resolve + Fetch). A separate `internal/confluence/` package handles XHTML→Markdown conversion using `html-to-markdown/v2` with a custom plugin for Confluence-specific elements (`ac:*`, `ri:*`). The provider writes `.md` files to disk; the existing MarkdownChunker and FTS5 pipeline handle the rest unchanged.

**Tech Stack:** Go, `github.com/JohannesKaufmann/html-to-markdown/v2`, Confluence REST API v1, `net/http/httptest` for testing.

**Spec:** `docs/superpowers/specs/2026-04-11-confluence-provider-design.md`

---

### Task 1: Add `Depth` field and `"confluence"` provider to config

**Files:**
- Modify: `internal/config/config.go:25-37` (SourceConfig struct)
- Modify: `internal/config/config.go:39-52` (rawSource struct)
- Modify: `internal/config/config.go:55-79` (UnmarshalYAML)
- Modify: `internal/config/config.go:117-144` (validate)
- Modify: `internal/config/config_test.go`

- [ ] **Step 1: Write the failing tests for Confluence config**

Add to `internal/config/config_test.go`:

```go
func TestLoadConfigConfluence(t *testing.T) {
	content := `
sources:
  - name: team-runbooks
    provider: confluence
    base_url: https://confluence.example.com
    ref: "123456"
    depth: 3
    schedule: "0 */4 * * *"
    auth:
      type: bearer
      token_env: CONFLUENCE_PAT
`
	f := writeTempConfig(t, content)
	cfg, err := config.Load(f)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	src := cfg.Sources[0]
	if src.Provider != "confluence" {
		t.Errorf("Provider = %q, want confluence", src.Provider)
	}
	if src.BaseURL != "https://confluence.example.com" {
		t.Errorf("BaseURL = %q", src.BaseURL)
	}
	if src.Ref != "123456" {
		t.Errorf("Ref = %q", src.Ref)
	}
	if src.Depth == nil || *src.Depth != 3 {
		t.Errorf("Depth = %v, want 3", src.Depth)
	}
	// Paths should default to [""] for confluence when not specified.
	if len(src.Paths) != 1 || src.Paths[0] != "" {
		t.Errorf("Paths = %v, want [\"\"]", src.Paths)
	}
}

func TestLoadConfigConfluenceDepthDefault(t *testing.T) {
	content := `
sources:
  - name: team-docs
    provider: confluence
    base_url: https://confluence.example.com
    ref: "999"
`
	f := writeTempConfig(t, content)
	cfg, err := config.Load(f)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	src := cfg.Sources[0]
	// depth unset → nil (provider will treat nil as unlimited)
	if src.Depth != nil {
		t.Errorf("Depth = %v, want nil (unlimited)", src.Depth)
	}
}

func TestLoadConfigConfluenceValidation(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{
			name: "confluence missing base_url",
			content: `
sources:
  - name: test
    provider: confluence
    ref: "123"
`,
		},
		{
			name: "confluence missing ref",
			content: `
sources:
  - name: test
    provider: confluence
    base_url: https://confluence.example.com
`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := writeTempConfig(t, tc.content)
			_, err := config.Load(f)
			if err == nil {
				t.Errorf("Load() expected error for case %q, got nil", tc.name)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/agent/projects/mcp-docs && go test ./internal/config/ -run "TestLoadConfigConfluence" -v`
Expected: compilation errors (Depth field doesn't exist, "confluence" is unknown provider)

- [ ] **Step 3: Add Depth field to SourceConfig and rawSource**

In `internal/config/config.go`, add the `Depth` field to `SourceConfig` (after `Ref`):

```go
type SourceConfig struct {
	Name     string     `yaml:"name"`
	Provider string     `yaml:"provider"`
	Repo     string     `yaml:"repo"`
	Org      string     `yaml:"org"`      // Azure DevOps
	Project  string     `yaml:"project"`  // Azure DevOps
	BaseURL  string     `yaml:"base_url"`
	Ref      string     `yaml:"ref"`
	Depth    *int       `yaml:"depth"`    // Confluence: max subtree depth (nil=unlimited, 0=root only)
	Proxy    bool       // defaults to true; handled via custom UnmarshalYAML
	Paths    []string   `yaml:"paths"`
	Schedule string     `yaml:"schedule"`
	Auth     AuthConfig `yaml:"auth"`
}
```

Add to `rawSource` as well:

```go
type rawSource struct {
	Name     string     `yaml:"name"`
	Provider string     `yaml:"provider"`
	Repo     string     `yaml:"repo"`
	Org      string     `yaml:"org"`
	Project  string     `yaml:"project"`
	BaseURL  string     `yaml:"base_url"`
	Ref      string     `yaml:"ref"`
	Depth    *int       `yaml:"depth"`
	Proxy    *bool      `yaml:"proxy"`
	Paths    []string   `yaml:"paths"`
	Schedule string     `yaml:"schedule"`
	Auth     AuthConfig `yaml:"auth"`
}
```

Add `s.Depth = raw.Depth` in `UnmarshalYAML`, after the line `s.Ref = raw.Ref`.

- [ ] **Step 4: Update validation for Confluence provider**

Replace the validate function in `internal/config/config.go`:

```go
func validate(cfg *Config) error {
	if len(cfg.Sources) == 0 {
		return fmt.Errorf("config must have at least one source")
	}

	for i, src := range cfg.Sources {
		if src.Name == "" {
			return fmt.Errorf("source[%d]: name is required", i)
		}
		if src.Provider == "" {
			return fmt.Errorf("source %q: provider is required", src.Name)
		}
		switch src.Provider {
		case "github", "azure-devops":
			if len(src.Paths) == 0 {
				return fmt.Errorf("source %q: at least one path is required", src.Name)
			}
		case "confluence":
			if src.BaseURL == "" {
				return fmt.Errorf("source %q: base_url is required for confluence provider", src.Name)
			}
			// Default paths to [""] so the fetcher walks the entire rawDir.
			if len(src.Paths) == 0 {
				cfg.Sources[i].Paths = []string{""}
			}
			// Default Repo for display purposes (used by Library.Repo in fetcher).
			if src.Repo == "" {
				cfg.Sources[i].Repo = src.BaseURL + "/pages/" + src.Ref
			}
		default:
			return fmt.Errorf("source %q: unknown provider %q (must be github, azure-devops, or confluence)", src.Name, src.Provider)
		}
		if src.Ref == "" {
			return fmt.Errorf("source %q: ref is required", src.Name)
		}
	}

	return nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd /home/agent/projects/mcp-docs && go test ./internal/config/ -v`
Expected: ALL tests pass (including existing ones)

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): add Depth field and confluence provider validation"
```

---

### Task 2: Add html-to-markdown/v2 dependency

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] **Step 1: Add the dependency**

Run: `cd /home/agent/projects/mcp-docs && go get github.com/JohannesKaufmann/html-to-markdown/v2@latest`

- [ ] **Step 2: Verify it resolves**

Run: `cd /home/agent/projects/mcp-docs && go mod tidy`
Expected: no errors, `go.mod` shows `github.com/JohannesKaufmann/html-to-markdown/v2` in the require block

- [ ] **Step 3: Verify project still builds**

Run: `cd /home/agent/projects/mcp-docs && go build ./...`
Expected: no errors

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: add html-to-markdown/v2 for Confluence XHTML conversion"
```

---

### Task 3: XHTML converter — preprocessing and base Convert function

**Files:**
- Create: `internal/confluence/converter.go`
- Create: `internal/confluence/converter_test.go`

- [ ] **Step 1: Write failing tests for CDATA preprocessing and basic HTML conversion**

Create `internal/confluence/converter_test.go`:

```go
package confluence

import (
	"strings"
	"testing"
)

func TestPreprocessCDATA(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "simple CDATA",
			input: `<ac:plain-text-body><![CDATA[hello world]]></ac:plain-text-body>`,
			want:  `<ac:plain-text-body>hello world</ac:plain-text-body>`,
		},
		{
			name:  "CDATA with special chars",
			input: `<ac:plain-text-body><![CDATA[if (a < b && c > d) { return a & b; }]]></ac:plain-text-body>`,
			want:  `<ac:plain-text-body>if (a &lt; b &amp;&amp; c &gt; d) { return a &amp; b; }</ac:plain-text-body>`,
		},
		{
			name:  "no CDATA",
			input: `<p>hello</p>`,
			want:  `<p>hello</p>`,
		},
		{
			name:  "multiple CDATA sections",
			input: `<x><![CDATA[one]]></x><y><![CDATA[two]]></y>`,
			want:  `<x>one</x><y>two</y>`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := preprocessCDATA(tc.input)
			if got != tc.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tc.want)
			}
		})
	}
}

func TestConvertBasicHTML(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string // substring that must appear in output
	}{
		{
			name:  "heading",
			input: `<h1>Title</h1>`,
			want:  "# Title",
		},
		{
			name:  "paragraph",
			input: `<p>Hello <strong>world</strong></p>`,
			want:  "Hello **world**",
		},
		{
			name:  "unordered list",
			input: `<ul><li>one</li><li>two</li></ul>`,
			want:  "- one",
		},
		{
			name:  "external link",
			input: `<a href="https://example.com">click</a>`,
			want:  "[click](https://example.com)",
		},
		{
			name:  "table",
			input: `<table><tbody><tr><th><p>A</p></th><th><p>B</p></th></tr><tr><td><p>1</p></td><td><p>2</p></td></tr></tbody></table>`,
			want:  "| A | B |",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Convert(tc.input)
			if err != nil {
				t.Fatalf("Convert: %v", err)
			}
			if !strings.Contains(got, tc.want) {
				t.Errorf("output does not contain %q.\ngot:\n%s", tc.want, got)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/agent/projects/mcp-docs && go test ./internal/confluence/ -v`
Expected: compilation error (package/functions don't exist)

- [ ] **Step 3: Implement preprocessCDATA and Convert**

Create `internal/confluence/converter.go`:

```go
package confluence

import (
	"regexp"
	"strings"

	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/table"
)

var cdataRe = regexp.MustCompile(`<!\[CDATA\[([\s\S]*?)\]\]>`)

// preprocessCDATA escapes CDATA sections so Go's html parser does not
// treat them as comments.
func preprocessCDATA(input string) string {
	return cdataRe.ReplaceAllStringFunc(input, func(m string) string {
		content := cdataRe.FindStringSubmatch(m)[1]
		content = strings.ReplaceAll(content, "&", "&amp;")
		content = strings.ReplaceAll(content, "<", "&lt;")
		content = strings.ReplaceAll(content, ">", "&gt;")
		return content
	})
}

// preprocess prepares Confluence storage format XHTML for parsing.
func preprocess(input string) string {
	input = preprocessCDATA(input)
	input = strings.ReplaceAll(input, "&nbsp;", "&#160;")
	return "<div>" + input + "</div>"
}

// collapseBlankLines reduces runs of 3+ newlines to 2.
func collapseBlankLines(s string) string {
	prev := ""
	for prev != s {
		prev = s
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(s)
}

// Convert transforms Confluence XHTML storage format into clean markdown.
func Convert(storageXHTML string) (string, error) {
	prepared := preprocess(storageXHTML)

	conv := converter.NewConverter(
		converter.WithPlugins(
			base.NewBasePlugin(),
			commonmark.NewCommonmarkPlugin(),
			table.NewTablePlugin(),
		),
	)

	md, err := conv.ConvertString(prepared)
	if err != nil {
		return "", err
	}

	return collapseBlankLines(md), nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/agent/projects/mcp-docs && go test ./internal/confluence/ -v`
Expected: ALL pass

- [ ] **Step 5: Commit**

```bash
git add internal/confluence/converter.go internal/confluence/converter_test.go
git commit -m "feat(confluence): add XHTML preprocessor and base Convert function"
```

---

### Task 4: Confluence plugin — macro handlers

**Files:**
- Create: `internal/confluence/plugin.go`
- Modify: `internal/confluence/converter.go` (register plugin)
- Modify: `internal/confluence/converter_test.go` (add macro tests)

- [ ] **Step 1: Write failing tests for Confluence macros**

Append to `internal/confluence/converter_test.go`:

```go
func TestConvertCodeBlock(t *testing.T) {
	input := `<ac:structured-macro ac:name="code" ac:schema-version="1">
<ac:parameter ac:name="language">go</ac:parameter>
<ac:plain-text-body><![CDATA[func main() {
	fmt.Println("hello")
}]]></ac:plain-text-body>
</ac:structured-macro>`

	got, err := Convert(input)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if !strings.Contains(got, "```go") {
		t.Errorf("missing ```go fence.\ngot:\n%s", got)
	}
	if !strings.Contains(got, `fmt.Println("hello")`) {
		t.Errorf("missing code content.\ngot:\n%s", got)
	}
}

func TestConvertCodeBlockNoLanguage(t *testing.T) {
	input := `<ac:structured-macro ac:name="code" ac:schema-version="1">
<ac:plain-text-body><![CDATA[some code]]></ac:plain-text-body>
</ac:structured-macro>`

	got, err := Convert(input)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if !strings.Contains(got, "```\n") {
		t.Errorf("missing bare ``` fence.\ngot:\n%s", got)
	}
	if !strings.Contains(got, "some code") {
		t.Errorf("missing code content.\ngot:\n%s", got)
	}
}

func TestConvertAdmonitions(t *testing.T) {
	macros := []struct {
		name   string
		prefix string
	}{
		{"info", "**Info:**"},
		{"note", "**Note:**"},
		{"warning", "**Warning:**"},
		{"tip", "**Tip:**"},
	}

	for _, m := range macros {
		t.Run(m.name, func(t *testing.T) {
			input := `<ac:structured-macro ac:name="` + m.name + `" ac:schema-version="1">
<ac:rich-text-body><p>Important message</p></ac:rich-text-body>
</ac:structured-macro>`

			got, err := Convert(input)
			if err != nil {
				t.Fatalf("Convert: %v", err)
			}
			if !strings.Contains(got, m.prefix) {
				t.Errorf("missing prefix %q.\ngot:\n%s", m.prefix, got)
			}
			if !strings.Contains(got, "Important message") {
				t.Errorf("missing body content.\ngot:\n%s", got)
			}
			// Should be a blockquote
			if !strings.Contains(got, ">") {
				t.Errorf("not a blockquote.\ngot:\n%s", got)
			}
		})
	}
}

func TestConvertPanel(t *testing.T) {
	input := `<ac:structured-macro ac:name="panel" ac:schema-version="1">
<ac:parameter ac:name="title">My Panel</ac:parameter>
<ac:rich-text-body><p>Panel content here</p></ac:rich-text-body>
</ac:structured-macro>`

	got, err := Convert(input)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if !strings.Contains(got, "**My Panel**") {
		t.Errorf("missing panel title.\ngot:\n%s", got)
	}
	if !strings.Contains(got, "Panel content here") {
		t.Errorf("missing panel body.\ngot:\n%s", got)
	}
}

func TestConvertExpand(t *testing.T) {
	input := `<ac:structured-macro ac:name="expand" ac:schema-version="1">
<ac:parameter ac:name="title">Click to expand</ac:parameter>
<ac:rich-text-body><p>Hidden content</p></ac:rich-text-body>
</ac:structured-macro>`

	got, err := Convert(input)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if !strings.Contains(got, "**Click to expand**") {
		t.Errorf("missing expand title.\ngot:\n%s", got)
	}
	if !strings.Contains(got, "Hidden content") {
		t.Errorf("missing expand body.\ngot:\n%s", got)
	}
}

func TestConvertStatus(t *testing.T) {
	input := `<ac:structured-macro ac:name="status" ac:schema-version="1">
<ac:parameter ac:name="title">Done</ac:parameter>
<ac:parameter ac:name="colour">Green</ac:parameter>
</ac:structured-macro>`

	got, err := Convert(input)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if !strings.Contains(got, "`[Done]`") {
		t.Errorf("missing status badge.\ngot:\n%s", got)
	}
}

func TestConvertTocRemoved(t *testing.T) {
	input := `<ac:structured-macro ac:name="toc" ac:schema-version="1">
<ac:parameter ac:name="maxLevel">3</ac:parameter>
</ac:structured-macro>`

	got, err := Convert(input)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if strings.TrimSpace(got) != "" {
		t.Errorf("toc should produce empty output.\ngot:\n%s", got)
	}
}

func TestConvertAnchorRemoved(t *testing.T) {
	input := `<ac:structured-macro ac:name="anchor" ac:schema-version="1">
<ac:parameter ac:name="">my-anchor</ac:parameter>
</ac:structured-macro>`

	got, err := Convert(input)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if strings.TrimSpace(got) != "" {
		t.Errorf("anchor should produce empty output.\ngot:\n%s", got)
	}
}

func TestConvertUnknownMacroWithBody(t *testing.T) {
	input := `<ac:structured-macro ac:name="custom-thing" ac:schema-version="1">
<ac:rich-text-body><p>Body content</p></ac:rich-text-body>
</ac:structured-macro>`

	got, err := Convert(input)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if !strings.Contains(got, "Body content") {
		t.Errorf("unknown macro body should be rendered.\ngot:\n%s", got)
	}
}

func TestConvertUnknownMacroWithoutBody(t *testing.T) {
	input := `<ac:structured-macro ac:name="unknown-no-body" ac:schema-version="1">
<ac:parameter ac:name="key">value</ac:parameter>
</ac:structured-macro>`

	got, err := Convert(input)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if strings.TrimSpace(got) != "" {
		t.Errorf("unknown macro without body should be removed.\ngot:\n%s", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/agent/projects/mcp-docs && go test ./internal/confluence/ -run "TestConvertCode|TestConvertAdm|TestConvertPanel|TestConvertExpand|TestConvertStatus|TestConvertToc|TestConvertAnchor|TestConvertUnknown" -v`
Expected: FAIL (macros rendered as raw text or ignored)

- [ ] **Step 3: Implement the Confluence plugin**

Create `internal/confluence/plugin.go`:

```go
package confluence

import (
	"fmt"
	"log"
	"strings"

	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"golang.org/x/net/html"
)

// confluencePlugin registers renderers for Confluence XHTML elements.
type confluencePlugin struct{}

func newConfluencePlugin() *confluencePlugin {
	return &confluencePlugin{}
}

func (p *confluencePlugin) Name() string {
	return "confluence"
}

func (p *confluencePlugin) Init(conv *converter.Converter) error {
	// Macros (ac:structured-macro and ac:macro)
	conv.Register.RendererFor("ac:structured-macro", converter.TagTypeBlock,
		renderStructuredMacro, converter.PriorityStandard)
	conv.Register.RendererFor("ac:macro", converter.TagTypeBlock,
		renderStructuredMacro, converter.PriorityStandard)

	// Links
	conv.Register.RendererFor("ac:link", converter.TagTypeInline,
		renderLink, converter.PriorityStandard)

	// Images
	conv.Register.RendererFor("ac:image", converter.TagTypeInline,
		renderImage, converter.PriorityStandard)

	// Task lists
	conv.Register.RendererFor("ac:task-list", converter.TagTypeBlock,
		renderTaskList, converter.PriorityStandard)
	conv.Register.RendererFor("ac:task", converter.TagTypeBlock,
		renderTask, converter.PriorityStandard)

	// Layout — flatten content
	for _, tag := range []string{"ac:layout", "ac:layout-section", "ac:layout-cell"} {
		conv.Register.RendererFor(tag, converter.TagTypeBlock,
			renderPassthrough, converter.PriorityStandard)
	}

	// Elements consumed by parent handlers — suppress direct rendering
	for _, tag := range []string{"ac:parameter", "ac:default-parameter"} {
		conv.Register.RendererFor(tag, converter.TagTypeInline,
			renderRemove, converter.PriorityStandard)
	}

	// Rich text body — recurse into children
	conv.Register.RendererFor("ac:rich-text-body", converter.TagTypeBlock,
		renderPassthrough, converter.PriorityStandard)

	// Plain text body — extract text
	conv.Register.RendererFor("ac:plain-text-body", converter.TagTypeInline,
		renderPlainTextBody, converter.PriorityStandard)

	// Emoticons — remove
	conv.Register.RendererFor("ac:emoticon", converter.TagTypeInline,
		renderRemove, converter.PriorityStandard)

	// Task sub-elements — suppress (consumed by renderTask)
	for _, tag := range []string{"ac:task-id", "ac:task-status"} {
		conv.Register.RendererFor(tag, converter.TagTypeInline,
			renderRemove, converter.PriorityStandard)
	}
	conv.Register.RendererFor("ac:task-body", converter.TagTypeInline,
		renderPassthrough, converter.PriorityStandard)

	// Resource identifiers — suppress (consumed by parent link/image handlers)
	for _, tag := range []string{"ri:page", "ri:attachment", "ri:url", "ri:user",
		"ri:blog-post", "ri:space", "ri:content-entity", "ri:shortcut"} {
		conv.Register.RendererFor(tag, converter.TagTypeInline,
			renderRemove, converter.PriorityStandard)
	}

	// ac:link-body and ac:plain-text-link-body — passthrough
	conv.Register.RendererFor("ac:link-body", converter.TagTypeInline,
		renderPassthrough, converter.PriorityStandard)
	conv.Register.RendererFor("ac:plain-text-link-body", converter.TagTypeInline,
		renderPassthrough, converter.PriorityStandard)

	return nil
}

// --- Macro rendering ---

func renderStructuredMacro(ctx converter.Context, w converter.Writer, n *html.Node) converter.RenderStatus {
	macroName := getAttr(n, "ac:name")

	switch macroName {
	case "code":
		return renderCodeBlock(ctx, w, n)
	case "info", "note", "warning", "tip":
		return renderAdmonition(ctx, w, n, macroName)
	case "panel":
		return renderPanel(ctx, w, n)
	case "expand":
		return renderExpand(ctx, w, n)
	case "status":
		return renderStatusBadge(ctx, w, n)
	case "excerpt":
		return renderExcerptBody(ctx, w, n)
	case "section", "column":
		return renderPassthrough(ctx, w, n)
	case "toc", "anchor", "attachments", "children", "contentbylabel",
		"excerpt-include", "include", "jiraissues", "details":
		return renderRemove(ctx, w, n)
	default:
		return renderUnknownMacro(ctx, w, n, macroName)
	}
}

func renderCodeBlock(ctx converter.Context, w converter.Writer, n *html.Node) converter.RenderStatus {
	lang := getMacroParam(n, "language")
	body := getPlainTextBody(n)

	w.WriteString("\n\n```" + lang + "\n")
	w.WriteString(body)
	if !strings.HasSuffix(body, "\n") {
		w.WriteString("\n")
	}
	w.WriteString("```\n\n")

	return converter.RenderSuccess
}

func renderAdmonition(ctx converter.Context, w converter.Writer, n *html.Node, macroName string) converter.RenderStatus {
	title := strings.Title(macroName)
	bodyNode := getRichTextBody(n)

	w.WriteString("\n\n> ")
	w.WriteString(fmt.Sprintf("**%s:** ", title))
	if bodyNode != nil {
		ctx.RenderChildNodes(ctx, w, bodyNode)
	}
	w.WriteString("\n\n")

	return converter.RenderSuccess
}

func renderPanel(ctx converter.Context, w converter.Writer, n *html.Node) converter.RenderStatus {
	title := getMacroParam(n, "title")
	bodyNode := getRichTextBody(n)

	w.WriteString("\n\n> ")
	if title != "" {
		w.WriteString(fmt.Sprintf("**%s**\n> \n> ", title))
	}
	if bodyNode != nil {
		ctx.RenderChildNodes(ctx, w, bodyNode)
	}
	w.WriteString("\n\n")

	return converter.RenderSuccess
}

func renderExpand(ctx converter.Context, w converter.Writer, n *html.Node) converter.RenderStatus {
	title := getMacroParam(n, "title")
	bodyNode := getRichTextBody(n)

	w.WriteString("\n\n")
	if title != "" {
		w.WriteString(fmt.Sprintf("**%s**\n\n", title))
	}
	if bodyNode != nil {
		ctx.RenderChildNodes(ctx, w, bodyNode)
	}
	w.WriteString("\n\n")

	return converter.RenderSuccess
}

func renderStatusBadge(ctx converter.Context, w converter.Writer, n *html.Node) converter.RenderStatus {
	title := getMacroParam(n, "title")
	w.WriteString(fmt.Sprintf("`[%s]`", title))
	return converter.RenderSuccess
}

func renderExcerptBody(ctx converter.Context, w converter.Writer, n *html.Node) converter.RenderStatus {
	bodyNode := getRichTextBody(n)
	if bodyNode != nil {
		ctx.RenderChildNodes(ctx, w, bodyNode)
	}
	return converter.RenderSuccess
}

func renderUnknownMacro(ctx converter.Context, w converter.Writer, n *html.Node, macroName string) converter.RenderStatus {
	bodyNode := getRichTextBody(n)
	if bodyNode != nil {
		log.Printf("[confluence] unknown macro %q — rendering body", macroName)
		ctx.RenderChildNodes(ctx, w, bodyNode)
		return converter.RenderSuccess
	}
	log.Printf("[confluence] unknown macro %q without body — removing", macroName)
	return converter.RenderSuccess
}

// --- Link rendering ---

func renderLink(ctx converter.Context, w converter.Writer, n *html.Node) converter.RenderStatus {
	linkText := getLinkText(ctx, w, n)
	target := ""

	if ri := findChild(n, "ri:page"); ri != nil {
		target = getAttr(ri, "ri:content-title")
	} else if ri := findChild(n, "ri:attachment"); ri != nil {
		target = getAttr(ri, "ri:filename")
	} else if ri := findChild(n, "ri:url"); ri != nil {
		target = getAttr(ri, "ri:value")
	}

	if linkText == "" {
		linkText = target
	}

	w.WriteString(fmt.Sprintf("[%s](%s)", linkText, target))
	return converter.RenderSuccess
}

// --- Image rendering ---

func renderImage(ctx converter.Context, w converter.Writer, n *html.Node) converter.RenderStatus {
	alt := ""
	src := ""

	if ri := findChild(n, "ri:attachment"); ri != nil {
		filename := getAttr(ri, "ri:filename")
		alt = filename
		src = filename
	} else if ri := findChild(n, "ri:url"); ri != nil {
		src = getAttr(ri, "ri:value")
		alt = "image"
	}

	w.WriteString(fmt.Sprintf("![%s](%s)", alt, src))
	return converter.RenderSuccess
}

// --- Task list rendering ---

func renderTaskList(ctx converter.Context, w converter.Writer, n *html.Node) converter.RenderStatus {
	w.WriteString("\n")
	ctx.RenderChildNodes(ctx, w, n)
	w.WriteString("\n")
	return converter.RenderSuccess
}

func renderTask(ctx converter.Context, w converter.Writer, n *html.Node) converter.RenderStatus {
	status := ""
	var bodyNode *html.Node

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode {
			switch c.Data {
			case "ac:task-status":
				status = getTextContent(c)
			case "ac:task-body":
				bodyNode = c
			}
		}
	}

	if status == "complete" {
		w.WriteString("- [x] ")
	} else {
		w.WriteString("- [ ] ")
	}
	if bodyNode != nil {
		ctx.RenderChildNodes(ctx, w, bodyNode)
	}
	w.WriteString("\n")

	return converter.RenderSuccess
}

// --- Passthrough and remove ---

func renderPassthrough(ctx converter.Context, w converter.Writer, n *html.Node) converter.RenderStatus {
	ctx.RenderChildNodes(ctx, w, n)
	return converter.RenderSuccess
}

func renderRemove(_ converter.Context, _ converter.Writer, _ *html.Node) converter.RenderStatus {
	return converter.RenderSuccess
}

func renderPlainTextBody(_ converter.Context, w converter.Writer, n *html.Node) converter.RenderStatus {
	w.WriteString(getTextContent(n))
	return converter.RenderSuccess
}

// --- Helpers ---

// getAttr returns the value of the named attribute on n, or "".
func getAttr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

// findChild returns the first direct child element with the given tag name.
func findChild(n *html.Node, tag string) *html.Node {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == tag {
			return c
		}
	}
	return nil
}

// findDescendant returns the first descendant element with the given tag name (BFS).
func findDescendant(n *html.Node, tag string) *html.Node {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode {
			if c.Data == tag {
				return c
			}
			if found := findDescendant(c, tag); found != nil {
				return found
			}
		}
	}
	return nil
}

// getMacroParam extracts the value of an ac:parameter with the given ac:name.
func getMacroParam(n *html.Node, paramName string) string {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && (c.Data == "ac:parameter" || c.Data == "ac:default-parameter") {
			name := getAttr(c, "ac:name")
			if name == paramName || (paramName == "" && c.Data == "ac:default-parameter") {
				return getTextContent(c)
			}
		}
	}
	return ""
}

// getRichTextBody finds the ac:rich-text-body child of a macro node.
func getRichTextBody(n *html.Node) *html.Node {
	return findChild(n, "ac:rich-text-body")
}

// getPlainTextBody extracts the text content of the ac:plain-text-body child.
func getPlainTextBody(n *html.Node) string {
	ptb := findChild(n, "ac:plain-text-body")
	if ptb == nil {
		return ""
	}
	return getTextContent(ptb)
}

// getTextContent returns the concatenated text content of a node and its descendants.
func getTextContent(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var sb strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		sb.WriteString(getTextContent(c))
	}
	return sb.String()
}

// getLinkText extracts the display text from ac:plain-text-link-body or ac:link-body.
func getLinkText(ctx converter.Context, w converter.Writer, n *html.Node) string {
	if ptlb := findChild(n, "ac:plain-text-link-body"); ptlb != nil {
		return getTextContent(ptlb)
	}
	if lb := findChild(n, "ac:link-body"); lb != nil {
		return getTextContent(lb)
	}
	return ""
}
```

- [ ] **Step 4: Register the plugin in Convert**

Modify `internal/confluence/converter.go` — update the `Convert` function to add the Confluence plugin:

```go
func Convert(storageXHTML string) (string, error) {
	prepared := preprocess(storageXHTML)

	conv := converter.NewConverter(
		converter.WithPlugins(
			base.NewBasePlugin(),
			commonmark.NewCommonmarkPlugin(),
			table.NewTablePlugin(),
			newConfluencePlugin(),
		),
	)

	md, err := conv.ConvertString(prepared)
	if err != nil {
		return "", err
	}

	return collapseBlankLines(md), nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd /home/agent/projects/mcp-docs && go test ./internal/confluence/ -v`
Expected: ALL pass

- [ ] **Step 6: Commit**

```bash
git add internal/confluence/plugin.go internal/confluence/converter.go internal/confluence/converter_test.go
git commit -m "feat(confluence): add XHTML plugin for macros, links, images, tasks"
```

---

### Task 5: Confluence plugin — link, image, and task list tests

**Files:**
- Modify: `internal/confluence/converter_test.go`

- [ ] **Step 1: Write tests for links, images, and task lists**

Append to `internal/confluence/converter_test.go`:

```go
func TestConvertInternalLink(t *testing.T) {
	input := `<ac:link>
<ri:page ri:content-title="Getting Started" ri:space-key="DOC"/>
<ac:plain-text-link-body><![CDATA[Read the guide]]></ac:plain-text-link-body>
</ac:link>`

	got, err := Convert(input)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if !strings.Contains(got, "[Read the guide](Getting Started)") {
		t.Errorf("wrong link output.\ngot:\n%s", got)
	}
}

func TestConvertAttachmentLink(t *testing.T) {
	input := `<ac:link>
<ri:attachment ri:filename="report.pdf"/>
<ac:plain-text-link-body><![CDATA[Download Report]]></ac:plain-text-link-body>
</ac:link>`

	got, err := Convert(input)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if !strings.Contains(got, "[Download Report](report.pdf)") {
		t.Errorf("wrong link output.\ngot:\n%s", got)
	}
}

func TestConvertExternalLinkViaRI(t *testing.T) {
	input := `<ac:link>
<ri:url ri:value="https://example.com"/>
<ac:plain-text-link-body><![CDATA[Example]]></ac:plain-text-link-body>
</ac:link>`

	got, err := Convert(input)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if !strings.Contains(got, "[Example](https://example.com)") {
		t.Errorf("wrong link output.\ngot:\n%s", got)
	}
}

func TestConvertImageAttachment(t *testing.T) {
	input := `<ac:image ac:width="600">
<ri:attachment ri:filename="architecture.png"/>
</ac:image>`

	got, err := Convert(input)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if !strings.Contains(got, "![architecture.png](architecture.png)") {
		t.Errorf("wrong image output.\ngot:\n%s", got)
	}
}

func TestConvertImageURL(t *testing.T) {
	input := `<ac:image>
<ri:url ri:value="https://example.com/logo.png"/>
</ac:image>`

	got, err := Convert(input)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if !strings.Contains(got, "![image](https://example.com/logo.png)") {
		t.Errorf("wrong image output.\ngot:\n%s", got)
	}
}

func TestConvertTaskList(t *testing.T) {
	input := `<ac:task-list>
<ac:task>
<ac:task-id>1</ac:task-id>
<ac:task-status>incomplete</ac:task-status>
<ac:task-body>Write tests</ac:task-body>
</ac:task>
<ac:task>
<ac:task-id>2</ac:task-id>
<ac:task-status>complete</ac:task-status>
<ac:task-body>Review PR</ac:task-body>
</ac:task>
</ac:task-list>`

	got, err := Convert(input)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if !strings.Contains(got, "- [ ] Write tests") {
		t.Errorf("missing incomplete task.\ngot:\n%s", got)
	}
	if !strings.Contains(got, "- [x] Review PR") {
		t.Errorf("missing complete task.\ngot:\n%s", got)
	}
}

func TestConvertLayout(t *testing.T) {
	input := `<ac:layout>
<ac:layout-section ac:type="two_equal">
<ac:layout-cell><p>Left column</p></ac:layout-cell>
<ac:layout-cell><p>Right column</p></ac:layout-cell>
</ac:layout-section>
</ac:layout>`

	got, err := Convert(input)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if !strings.Contains(got, "Left column") {
		t.Errorf("missing left column.\ngot:\n%s", got)
	}
	if !strings.Contains(got, "Right column") {
		t.Errorf("missing right column.\ngot:\n%s", got)
	}
}

func TestConvertRealisticPage(t *testing.T) {
	input := `<h1>API Documentation</h1>
<p>This page describes the <strong>REST API</strong>.</p>
<ac:structured-macro ac:name="toc" ac:schema-version="1">
<ac:parameter ac:name="maxLevel">3</ac:parameter>
</ac:structured-macro>
<h2>Authentication</h2>
<ac:structured-macro ac:name="info" ac:schema-version="1">
<ac:rich-text-body><p>You need a valid API token.</p></ac:rich-text-body>
</ac:structured-macro>
<p>See <ac:link><ri:page ri:content-title="Getting Tokens"/><ac:plain-text-link-body><![CDATA[token docs]]></ac:plain-text-link-body></ac:link> for details.</p>
<h2>Examples</h2>
<ac:structured-macro ac:name="code" ac:schema-version="1">
<ac:parameter ac:name="language">bash</ac:parameter>
<ac:plain-text-body><![CDATA[curl -H "Authorization: Bearer $TOKEN" https://api.example.com/v1/users]]></ac:plain-text-body>
</ac:structured-macro>
<ac:structured-macro ac:name="warning" ac:schema-version="1">
<ac:rich-text-body><p>Do <strong>not</strong> share your token.</p></ac:rich-text-body>
</ac:structured-macro>`

	got, err := Convert(input)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}

	checks := []string{
		"# API Documentation",
		"**REST API**",
		"## Authentication",
		"**Info:**",
		"API token",
		"[token docs](Getting Tokens)",
		"## Examples",
		"```bash",
		"curl",
		"**Warning:**",
	}
	for _, check := range checks {
		if !strings.Contains(got, check) {
			t.Errorf("missing %q in output.\ngot:\n%s", check, got)
		}
	}

	// TOC should be removed
	if strings.Contains(got, "maxLevel") {
		t.Errorf("TOC should be removed.\ngot:\n%s", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they pass**

Run: `cd /home/agent/projects/mcp-docs && go test ./internal/confluence/ -v`
Expected: ALL pass

- [ ] **Step 3: Commit**

```bash
git add internal/confluence/converter_test.go
git commit -m "test(confluence): add link, image, task list, layout, and realistic page tests"
```

---

### Task 6: Golden file tests with public Confluence fixtures

**Files:**
- Create: `internal/confluence/testdata/` (directory)
- Modify: `internal/confluence/converter_test.go`

- [ ] **Step 1: Download XHTML fixtures from public Confluence**

```bash
cd /home/agent/projects/mcp-docs
mkdir -p internal/confluence/testdata

# Atlassian's own macro documentation pages
curl -s "https://confluence.atlassian.com/rest/api/content/1627457156?expand=body.storage" | \
  python3 -c "import sys,json; print(json.load(sys.stdin)['body']['storage']['value'])" \
  > internal/confluence/testdata/atlassian-admonitions.xhtml

curl -s "https://confluence.atlassian.com/rest/api/content/1627457080?expand=body.storage" | \
  python3 -c "import sys,json; print(json.load(sys.stdin)['body']['storage']['value'])" \
  > internal/confluence/testdata/atlassian-code-block.xhtml
```

- [ ] **Step 2: Generate golden files**

Write a test helper that generates golden files and create the initial goldens:

Append to `internal/confluence/converter_test.go`:

```go
import (
	"flag"
	"os"
	"path/filepath"
)

var updateGolden = flag.Bool("update-golden", false, "update golden files")

func TestGoldenFiles(t *testing.T) {
	fixtures, err := filepath.Glob("testdata/*.xhtml")
	if err != nil {
		t.Fatal(err)
	}
	if len(fixtures) == 0 {
		t.Skip("no .xhtml fixtures in testdata/")
	}

	for _, fixture := range fixtures {
		name := strings.TrimSuffix(filepath.Base(fixture), ".xhtml")
		t.Run(name, func(t *testing.T) {
			input, err := os.ReadFile(fixture)
			if err != nil {
				t.Fatal(err)
			}

			got, err := Convert(string(input))
			if err != nil {
				t.Fatalf("Convert: %v", err)
			}

			goldenPath := fixture[:len(fixture)-len(".xhtml")] + ".golden"

			if *updateGolden {
				if err := os.WriteFile(goldenPath, []byte(got), 0644); err != nil {
					t.Fatal(err)
				}
				t.Logf("updated %s", goldenPath)
				return
			}

			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("reading golden file (run with -update-golden to create): %v", err)
			}

			if got != string(want) {
				t.Errorf("output does not match golden file %s.\n--- GOT ---\n%s\n--- WANT ---\n%s", goldenPath, got, string(want))
			}
		})
	}
}
```

- [ ] **Step 3: Generate initial golden files**

Run: `cd /home/agent/projects/mcp-docs && go test ./internal/confluence/ -run TestGoldenFiles -update-golden -v`
Expected: golden files created in testdata/

- [ ] **Step 4: Verify golden tests pass on re-run**

Run: `cd /home/agent/projects/mcp-docs && go test ./internal/confluence/ -run TestGoldenFiles -v`
Expected: PASS

- [ ] **Step 5: Review golden files for sanity**

Read the generated `.golden` files and verify the markdown output is clean, readable, and correct. Manually inspect that:
- Code blocks have correct fencing and language annotations
- Admonitions are blockquoted with proper prefixes
- Links are properly formatted
- No raw XHTML elements leak through

- [ ] **Step 6: Commit**

```bash
git add internal/confluence/testdata/ internal/confluence/converter_test.go
git commit -m "test(confluence): add golden file tests with public Confluence fixtures"
```

---

### Task 7: Confluence provider — Resolve

**Files:**
- Create: `internal/source/confluence.go`
- Create: `internal/source/confluence_test.go`

- [ ] **Step 1: Write failing tests for Resolve**

Create `internal/source/confluence_test.go`:

```go
package source

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/codanael/docserve/internal/config"
)

// confluencePageResult represents one page in a Confluence search response.
type confluencePageResult struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Version struct {
		Number int `json:"number"`
	} `json:"version"`
	Ancestors []struct {
		ID string `json:"id"`
	} `json:"ancestors"`
}

// confluenceSearchResponse is the CQL search response envelope.
type confluenceSearchResponse struct {
	Results []confluencePageResult `json:"results"`
	Start   int                   `json:"start"`
	Limit   int                   `json:"limit"`
	Size    int                   `json:"size"`
	Links   map[string]string     `json:"_links"`
}

func TestConfluenceResolveStableHash(t *testing.T) {
	root := confluencePageResult{
		ID:    "100",
		Title: "Root Page",
	}
	root.Version.Number = 5

	child := confluencePageResult{
		ID:    "200",
		Title: "Child Page",
	}
	child.Version.Number = 3
	child.Ancestors = []struct {
		ID string `json:"id"`
	}{{ID: "100"}}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/api/content/100" && r.URL.Query().Get("expand") != "":
			// Root page fetch
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(root)
		case r.URL.Path == "/rest/api/content/search":
			// CQL descendant search
			resp := confluenceSearchResponse{
				Results: []confluencePageResult{child},
				Start:   0,
				Limit:   200,
				Size:    1,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cfg := config.SourceConfig{
		Provider: "confluence",
		BaseURL:  srv.URL,
	}
	p := NewConfluenceProvider(cfg, srv.Client())

	sha1, err := p.Resolve(context.Background(), "100")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(sha1) != 40 {
		t.Errorf("SHA length = %d, want 40", len(sha1))
	}

	// Second call should produce the same hash.
	sha2, err := p.Resolve(context.Background(), "100")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if sha1 != sha2 {
		t.Errorf("hash not stable: %q != %q", sha1, sha2)
	}
}

func TestConfluenceResolveHashChangesOnVersionBump(t *testing.T) {
	version := 3

	root := confluencePageResult{
		ID:    "100",
		Title: "Root",
	}
	root.Version.Number = 1

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/api/content/100":
			root.Version.Number = 1
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(root)
		case r.URL.Path == "/rest/api/content/search":
			child := confluencePageResult{
				ID:    "200",
				Title: "Child",
			}
			child.Version.Number = version
			child.Ancestors = []struct {
				ID string `json:"id"`
			}{{ID: "100"}}
			resp := confluenceSearchResponse{
				Results: []confluencePageResult{child},
				Start:   0,
				Limit:   200,
				Size:    1,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cfg := config.SourceConfig{
		Provider: "confluence",
		BaseURL:  srv.URL,
	}
	p := NewConfluenceProvider(cfg, srv.Client())

	sha1, err := p.Resolve(context.Background(), "100")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	// Bump child version
	version = 4
	sha2, err := p.Resolve(context.Background(), "100")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if sha1 == sha2 {
		t.Error("hash should change when page version changes")
	}
}

func TestConfluenceResolveDepthFilter(t *testing.T) {
	root := confluencePageResult{ID: "100", Title: "Root"}
	root.Version.Number = 1

	child := confluencePageResult{ID: "200", Title: "Child"}
	child.Version.Number = 1
	child.Ancestors = []struct {
		ID string `json:"id"`
	}{{ID: "100"}}

	grandchild := confluencePageResult{ID: "300", Title: "Grandchild"}
	grandchild.Version.Number = 1
	grandchild.Ancestors = []struct {
		ID string `json:"id"`
	}{{ID: "100"}, {ID: "200"}}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/api/content/100":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(root)
		case r.URL.Path == "/rest/api/content/search":
			resp := confluenceSearchResponse{
				Results: []confluencePageResult{child, grandchild},
				Start:   0,
				Limit:   200,
				Size:    2,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	depth1 := 1
	cfg := config.SourceConfig{
		Provider: "confluence",
		BaseURL:  srv.URL,
		Depth:    &depth1,
	}
	p := NewConfluenceProvider(cfg, srv.Client())

	shaDepth1, err := p.Resolve(context.Background(), "100")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	// With unlimited depth, hash should differ because grandchild is included
	cfg.Depth = nil
	p2 := NewConfluenceProvider(cfg, srv.Client())

	shaUnlimited, err := p2.Resolve(context.Background(), "100")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if shaDepth1 == shaUnlimited {
		t.Error("hash with depth=1 should differ from unlimited (grandchild excluded)")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/agent/projects/mcp-docs && go test ./internal/source/ -run "TestConfluence" -v`
Expected: compilation error (ConfluenceProvider doesn't exist)

- [ ] **Step 3: Implement ConfluenceProvider struct and Resolve**

Create `internal/source/confluence.go`:

```go
package source

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/codanael/docserve/internal/config"
)

const defaultConfluenceConcurrency = 3

// ConfluenceProvider fetches documentation from a Confluence Data Center instance.
type ConfluenceProvider struct {
	cfg    config.SourceConfig
	client *http.Client
	apiURL string // base URL for REST API (e.g., https://confluence.example.com)
	depth  int    // -1 = unlimited
}

// NewConfluenceProvider constructs a ConfluenceProvider.
func NewConfluenceProvider(cfg config.SourceConfig, client *http.Client) *ConfluenceProvider {
	if client == nil {
		client = &http.Client{}
	}
	client = WrapClientAuth(client, cfg.Auth)

	depth := -1
	if cfg.Depth != nil {
		depth = *cfg.Depth
	}

	return &ConfluenceProvider{
		cfg:    cfg,
		client: client,
		apiURL: strings.TrimRight(cfg.BaseURL, "/"),
		depth:  depth,
	}
}

// confluencePage holds the metadata needed for Resolve and Fetch.
type confluencePage struct {
	ID        string
	Title     string
	Version   int
	Ancestors []string // ancestor page IDs, from root to parent
	Body      string   // storage format XHTML (only populated during Fetch)
}

// Resolve returns a deterministic hash of the page tree state.
func (p *ConfluenceProvider) Resolve(ctx context.Context, ref string) (string, error) {
	pages, err := p.fetchPageTree(ctx, ref, false)
	if err != nil {
		return "", fmt.Errorf("confluence resolve: %w", err)
	}

	// Sort by ID for deterministic ordering.
	sort.Slice(pages, func(i, j int) bool {
		return pages[i].ID < pages[j].ID
	})

	// Hash all page IDs and versions.
	h := sha256.New()
	for _, page := range pages {
		fmt.Fprintf(h, "%s:%d\n", page.ID, page.Version)
	}

	return fmt.Sprintf("%x", h.Sum(nil))[:40], nil
}

// Fetch downloads pages and writes them as markdown files to destDir.
func (p *ConfluenceProvider) Fetch(ctx context.Context, sha string, paths []string, destDir string) error {
	// Implemented in Task 8
	return fmt.Errorf("not implemented")
}

// fetchPageTree retrieves the root page and its descendants, filtered by depth.
// If withBody is true, body.storage is expanded (used by Fetch).
func (p *ConfluenceProvider) fetchPageTree(ctx context.Context, rootID string, withBody bool) ([]confluencePage, error) {
	// Fetch root page.
	root, err := p.fetchPage(ctx, rootID, withBody)
	if err != nil {
		return nil, fmt.Errorf("fetch root page %s: %w", rootID, err)
	}
	rootDepth := len(root.Ancestors)

	// Fetch descendants via CQL.
	descendants, err := p.searchDescendants(ctx, rootID, withBody)
	if err != nil {
		return nil, fmt.Errorf("search descendants of %s: %w", rootID, err)
	}

	// Filter by depth.
	var pages []confluencePage
	pages = append(pages, *root)

	for _, d := range descendants {
		relativeDepth := len(d.Ancestors) - rootDepth
		if p.depth >= 0 && relativeDepth > p.depth {
			continue
		}
		pages = append(pages, d)
	}

	return pages, nil
}

// fetchPage retrieves a single page by ID.
func (p *ConfluenceProvider) fetchPage(ctx context.Context, pageID string, withBody bool) (*confluencePage, error) {
	expand := "version,ancestors"
	if withBody {
		expand = "body.storage,version,ancestors"
	}

	u := fmt.Sprintf("%s/rest/api/content/%s?expand=%s", p.apiURL, pageID, expand)

	body, err := p.doGet(ctx, u)
	if err != nil {
		return nil, err
	}
	defer body.Close()

	var raw struct {
		ID      string `json:"id"`
		Title   string `json:"title"`
		Version struct {
			Number int `json:"number"`
		} `json:"version"`
		Ancestors []struct {
			ID string `json:"id"`
		} `json:"ancestors"`
		Body struct {
			Storage struct {
				Value string `json:"value"`
			} `json:"storage"`
		} `json:"body"`
	}
	if err := json.NewDecoder(body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decoding page %s: %w", pageID, err)
	}

	ancestors := make([]string, len(raw.Ancestors))
	for i, a := range raw.Ancestors {
		ancestors[i] = a.ID
	}

	return &confluencePage{
		ID:        raw.ID,
		Title:     raw.Title,
		Version:   raw.Version.Number,
		Ancestors: ancestors,
		Body:      raw.Body.Storage.Value,
	}, nil
}

// searchDescendants fetches all descendant pages of rootID via CQL, with pagination.
func (p *ConfluenceProvider) searchDescendants(ctx context.Context, rootID string, withBody bool) ([]confluencePage, error) {
	expand := "version,ancestors"
	if withBody {
		expand = "body.storage,version,ancestors"
	}

	var all []confluencePage
	start := 0
	limit := 200

	for {
		cql := fmt.Sprintf("ancestor=%s AND type=page", rootID)
		u := fmt.Sprintf("%s/rest/api/content/search?cql=%s&expand=%s&start=%d&limit=%d",
			p.apiURL, url.QueryEscape(cql), expand, start, limit)

		body, err := p.doGet(ctx, u)
		if err != nil {
			return nil, err
		}

		var resp struct {
			Results []struct {
				ID      string `json:"id"`
				Title   string `json:"title"`
				Version struct {
					Number int `json:"number"`
				} `json:"version"`
				Ancestors []struct {
					ID string `json:"id"`
				} `json:"ancestors"`
				Body struct {
					Storage struct {
						Value string `json:"value"`
					} `json:"storage"`
				} `json:"body"`
			} `json:"results"`
			Start int               `json:"start"`
			Limit int               `json:"limit"`
			Size  int               `json:"size"`
			Links map[string]string  `json:"_links"`
		}
		if err := json.NewDecoder(body).Decode(&resp); err != nil {
			body.Close()
			return nil, fmt.Errorf("decoding search results: %w", err)
		}
		body.Close()

		for _, r := range resp.Results {
			ancestors := make([]string, len(r.Ancestors))
			for i, a := range r.Ancestors {
				ancestors[i] = a.ID
			}
			all = append(all, confluencePage{
				ID:        r.ID,
				Title:     r.Title,
				Version:   r.Version.Number,
				Ancestors: ancestors,
				Body:      r.Body.Storage.Value,
			})
		}

		if resp.Size < limit {
			break
		}
		start += resp.Size
	}

	return all, nil
}

// doGet performs a GET request with retry logic for 429/503.
func (p *ConfluenceProvider) doGet(ctx context.Context, rawURL string) (io.ReadCloser, error) {
	maxRetries := 3
	delays := []int{1, 2, 4} // seconds

	for attempt := 0; attempt <= maxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, fmt.Errorf("building request: %w", err)
		}
		req.Header.Set("Accept", "application/json")

		resp, err := p.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("confluence request: %w", err)
		}

		switch resp.StatusCode {
		case http.StatusOK:
			return resp.Body, nil
		case http.StatusTooManyRequests, http.StatusServiceUnavailable:
			resp.Body.Close()
			if attempt < maxRetries {
				delay := delays[attempt]
				// Respect Retry-After header if present.
				if ra := resp.Header.Get("Retry-After"); ra != "" {
					if d, err := parseRetryAfter(ra); err == nil && d > 0 {
						delay = d
					}
				}
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-sleepChan(delay):
				}
				continue
			}
			return nil, fmt.Errorf("confluence API returned %d after %d retries", resp.StatusCode, maxRetries)
		default:
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, fmt.Errorf("confluence API returned %d: %s", resp.StatusCode, string(body))
		}
	}

	return nil, fmt.Errorf("confluence request: exhausted retries")
}

Full `doGet` with retry logic at the bottom of `confluence.go`:

```go
import "time"

var sleepFunc = time.Sleep

func (p *ConfluenceProvider) doGet(ctx context.Context, rawURL string) (io.ReadCloser, error) {
	maxRetries := 3
	delays := []int{1, 2, 4}

	for attempt := 0; attempt <= maxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, fmt.Errorf("building request: %w", err)
		}
		req.Header.Set("Accept", "application/json")

		resp, err := p.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("confluence request: %w", err)
		}

		switch resp.StatusCode {
		case http.StatusOK:
			return resp.Body, nil
		case http.StatusTooManyRequests, http.StatusServiceUnavailable:
			resp.Body.Close()
			if attempt < maxRetries {
				delay := delays[attempt]
				if ra := resp.Header.Get("Retry-After"); ra != "" {
					if parsed, err := strconv.Atoi(ra); err == nil && parsed > 0 {
						delay = parsed
					}
				}
				sleepFunc(time.Duration(delay) * time.Second)
				continue
			}
			return nil, fmt.Errorf("confluence API returned %d after %d retries", resp.StatusCode, maxRetries)
		default:
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, fmt.Errorf("confluence API returned %d: %s", resp.StatusCode, string(body))
		}
	}

	return nil, fmt.Errorf("confluence request: exhausted retries")
}
```

Add `"strconv"` and `"time"` to the import block. Also add `"path/filepath"` and `"os"` (needed by Fetch in Task 8) and `"github.com/codanael/docserve/internal/confluence"`.

Note: the `Fetch` method is still a placeholder returning `"not implemented"` — that is expected. It will be implemented in Task 8.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/agent/projects/mcp-docs && go test ./internal/source/ -run "TestConfluence" -v`
Expected: ALL pass

- [ ] **Step 5: Commit**

```bash
git add internal/source/confluence.go internal/source/confluence_test.go
git commit -m "feat(confluence): implement ConfluenceProvider with Resolve and retry logic"
```

---

### Task 8: Confluence provider — Fetch with rate-limited concurrency

**Files:**
- Modify: `internal/source/confluence.go`
- Modify: `internal/source/confluence_test.go`

- [ ] **Step 1: Write failing tests for Fetch**

Append to `internal/source/confluence_test.go`:

```go
func TestConfluenceFetch(t *testing.T) {
	root := confluencePageResult{ID: "100", Title: "Architecture"}
	root.Version.Number = 1

	child := confluencePageResult{ID: "200", Title: "Backend"}
	child.Version.Number = 2
	child.Ancestors = []struct {
		ID string `json:"id"`
	}{{ID: "100"}}

	grandchild := confluencePageResult{ID: "300", Title: "API REST"}
	grandchild.Version.Number = 1
	grandchild.Ancestors = []struct {
		ID string `json:"id"`
	}{{ID: "100"}, {ID: "200"}}

	pageContent := map[string]string{
		"100": `<h1>Architecture</h1><p>Overview of the system.</p>`,
		"200": `<h2>Backend</h2><p>Backend services documentation.</p>`,
		"300": `<h2>API REST</h2><ac:structured-macro ac:name="code" ac:schema-version="1"><ac:parameter ac:name="language">go</ac:parameter><ac:plain-text-body><![CDATA[func main() {}]]></ac:plain-text-body></ac:structured-macro>`,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Single page fetch (with body)
		for _, pg := range []confluencePageResult{root, child, grandchild} {
			if r.URL.Path == "/rest/api/content/"+pg.ID {
				resp := map[string]interface{}{
					"id":    pg.ID,
					"title": pg.Title,
					"version": map[string]interface{}{
						"number": pg.Version.Number,
					},
					"ancestors": pg.Ancestors,
					"body": map[string]interface{}{
						"storage": map[string]interface{}{
							"value": pageContent[pg.ID],
						},
					},
				}
				json.NewEncoder(w).Encode(resp)
				return
			}
		}

		// CQL search
		if r.URL.Path == "/rest/api/content/search" {
			resp := confluenceSearchResponse{
				Results: []confluencePageResult{child, grandchild},
				Start:   0,
				Limit:   200,
				Size:    2,
			}
			// Add body content to results
			type resultWithBody struct {
				confluencePageResult
				Body struct {
					Storage struct {
						Value string `json:"value"`
					} `json:"storage"`
				} `json:"body"`
			}

			results := []map[string]interface{}{}
			for _, pg := range []confluencePageResult{child, grandchild} {
				results = append(results, map[string]interface{}{
					"id":        pg.ID,
					"title":     pg.Title,
					"version":   map[string]interface{}{"number": pg.Version.Number},
					"ancestors": pg.Ancestors,
					"body": map[string]interface{}{
						"storage": map[string]interface{}{
							"value": pageContent[pg.ID],
						},
					},
				})
			}
			json.NewEncoder(w).Encode(map[string]interface{}{
				"results": results,
				"start":   0,
				"limit":   200,
				"size":    2,
			})
			return
		}

		http.NotFound(w, r)
	}))
	defer srv.Close()

	cfg := config.SourceConfig{
		Provider: "confluence",
		BaseURL:  srv.URL,
	}
	p := NewConfluenceProvider(cfg, srv.Client())

	destDir := t.TempDir()
	if err := p.Fetch(context.Background(), "fakeSHA", []string{}, destDir); err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	// Verify files exist at correct paths.
	assertFileExists(t, destDir, "Architecture.md")
	assertFileExists(t, destDir, "Architecture/Backend.md")
	assertFileExists(t, destDir, "Architecture/Backend/API REST.md")

	// Verify content is markdown (not XHTML).
	content, err := os.ReadFile(filepath.Join(destDir, "Architecture/Backend/API REST.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "```go") {
		t.Errorf("expected fenced code block in output.\ngot:\n%s", string(content))
	}
	if strings.Contains(string(content), "ac:structured-macro") {
		t.Errorf("raw XHTML leaked into output.\ngot:\n%s", string(content))
	}
}

func TestConfluenceFetchSanitizesTitles(t *testing.T) {
	root := confluencePageResult{ID: "100", Title: "Docs: A/B Test"}
	root.Version.Number = 1

	pageContent := map[string]string{
		"100": `<p>Content</p>`,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/rest/api/content/100" {
			resp := map[string]interface{}{
				"id": "100", "title": root.Title,
				"version":   map[string]interface{}{"number": 1},
				"ancestors": []interface{}{},
				"body":      map[string]interface{}{"storage": map[string]interface{}{"value": pageContent["100"]}},
			}
			json.NewEncoder(w).Encode(resp)
			return
		}
		if r.URL.Path == "/rest/api/content/search" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"results": []interface{}{},
				"start": 0, "limit": 200, "size": 0,
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	cfg := config.SourceConfig{Provider: "confluence", BaseURL: srv.URL}
	p := NewConfluenceProvider(cfg, srv.Client())

	destDir := t.TempDir()
	if err := p.Fetch(context.Background(), "sha", []string{}, destDir); err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	// Title "Docs: A/B Test" should be sanitized to "Docs_ A_B Test"
	assertFileExists(t, destDir, "Docs_ A_B Test.md")
}

func TestConfluenceFetchRetryOn503(t *testing.T) {
	attempts := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/rest/api/content/100" {
			attempts++
			if attempts <= 2 {
				w.WriteHeader(http.StatusServiceUnavailable)
				w.Write([]byte(`{"message":"overloaded"}`))
				return
			}
			resp := map[string]interface{}{
				"id": "100", "title": "Root",
				"version":   map[string]interface{}{"number": 1},
				"ancestors": []interface{}{},
				"body":      map[string]interface{}{"storage": map[string]interface{}{"value": "<p>ok</p>"}},
			}
			json.NewEncoder(w).Encode(resp)
			return
		}
		if r.URL.Path == "/rest/api/content/search" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"results": []interface{}{},
				"start": 0, "limit": 200, "size": 0,
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	// Override sleep to be instant in tests
	origSleep := sleepFunc
	sleepFunc = func(d time.Duration) {}
	defer func() { sleepFunc = origSleep }()

	cfg := config.SourceConfig{Provider: "confluence", BaseURL: srv.URL}
	p := NewConfluenceProvider(cfg, srv.Client())

	destDir := t.TempDir()
	if err := p.Fetch(context.Background(), "sha", []string{}, destDir); err != nil {
		t.Fatalf("Fetch should succeed after retries: %v", err)
	}

	if attempts < 3 {
		t.Errorf("expected at least 3 attempts, got %d", attempts)
	}
}
```

Add `"strings"` and `"time"` to the import block if not already present.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/agent/projects/mcp-docs && go test ./internal/source/ -run "TestConfluenceFetch" -v`
Expected: FAIL ("not implemented")

- [ ] **Step 3: Implement Fetch method**

Replace the placeholder `Fetch` in `internal/source/confluence.go`:

```go
// Fetch downloads pages, converts to markdown, and writes to destDir.
func (p *ConfluenceProvider) Fetch(ctx context.Context, sha string, paths []string, destDir string) error {
	pages, err := p.fetchPageTree(ctx, p.cfg.Ref, true)
	if err != nil {
		return fmt.Errorf("confluence fetch: %w", err)
	}

	if len(pages) == 0 {
		return nil
	}

	// Find root page to compute relative paths.
	rootID := p.cfg.Ref
	var rootAncestors []string
	var rootTitle string
	for _, pg := range pages {
		if pg.ID == rootID {
			rootAncestors = pg.Ancestors
			rootTitle = pg.Title
			break
		}
	}

	// Build ancestor ID → title map for path construction.
	titleMap := make(map[string]string, len(pages))
	for _, pg := range pages {
		titleMap[pg.ID] = pg.Title
	}

	// Process pages with bounded concurrency.
	sem := make(chan struct{}, defaultConfluenceConcurrency)
	errs := make(chan error, len(pages))

	for _, pg := range pages {
		sem <- struct{}{} // acquire
		go func(pg confluencePage) {
			defer func() { <-sem }() // release

			// Compute file path.
			relPath := p.buildPagePath(pg, rootID, rootTitle, rootAncestors, titleMap)
			fullPath := filepath.Join(destDir, relPath+".md")

			// Convert XHTML to markdown.
			md, err := confluence.Convert(pg.Body)
			if err != nil {
				errs <- fmt.Errorf("converting page %s (%q): %w", pg.ID, pg.Title, err)
				return
			}

			// Write file.
			if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
				errs <- fmt.Errorf("creating dir for page %s: %w", pg.ID, err)
				return
			}
			if err := os.WriteFile(fullPath, []byte(md), 0644); err != nil {
				errs <- fmt.Errorf("writing page %s: %w", pg.ID, err)
				return
			}

			errs <- nil
		}(pg)
	}

	// Wait for all goroutines.
	for i := 0; i < len(pages); i++ {
		if err := <-errs; err != nil {
			return err
		}
	}

	return nil
}

// buildPagePath computes the relative file path for a page based on the title hierarchy.
func (p *ConfluenceProvider) buildPagePath(pg confluencePage, rootID, rootTitle string, rootAncestors []string, titleMap map[string]string) string {
	if pg.ID == rootID {
		return sanitizeTitle(rootTitle)
	}

	// Compute path segments from ancestors.
	// pg.Ancestors is [space root, ..., parent]. We want the portion after rootID.
	var segments []string
	segments = append(segments, sanitizeTitle(rootTitle))

	inSubtree := false
	for _, aid := range pg.Ancestors {
		if aid == rootID {
			inSubtree = true
			continue
		}
		if inSubtree {
			if title, ok := titleMap[aid]; ok {
				segments = append(segments, sanitizeTitle(title))
			}
		}
	}
	segments = append(segments, sanitizeTitle(pg.Title))

	return filepath.Join(segments...)
}

// sanitizeTitle replaces characters that are invalid in file paths.
func sanitizeTitle(title string) string {
	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
	)
	return replacer.Replace(title)
}
```

Add the confluence import to the file:

```go
import (
	// ... existing imports ...
	"path/filepath"

	"github.com/codanael/docserve/internal/confluence"
)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/agent/projects/mcp-docs && go test ./internal/source/ -run "TestConfluence" -v`
Expected: ALL pass

- [ ] **Step 5: Run all project tests**

Run: `cd /home/agent/projects/mcp-docs && go test ./... -v`
Expected: ALL pass

- [ ] **Step 6: Commit**

```bash
git add internal/source/confluence.go internal/source/confluence_test.go
git commit -m "feat(confluence): implement Fetch with concurrent page download and XHTML conversion"
```

---

### Task 9: Register Confluence in the provider factory

**Files:**
- Modify: `internal/source/provider.go:21-30`

- [ ] **Step 1: Add the case in NewProvider**

In `internal/source/provider.go`, add the `"confluence"` case:

```go
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
```

- [ ] **Step 2: Verify the project builds**

Run: `cd /home/agent/projects/mcp-docs && go build ./...`
Expected: no errors

- [ ] **Step 3: Run all tests**

Run: `cd /home/agent/projects/mcp-docs && go test ./...`
Expected: ALL pass

- [ ] **Step 4: Commit**

```bash
git add internal/source/provider.go
git commit -m "feat(confluence): register provider in factory"
```

---

### Task 10: Integration test — full Resolve → Fetch → Index → Search cycle

**Files:**
- Create: `internal/source/confluence_integration_test.go`

- [ ] **Step 1: Write the integration test**

Create `internal/source/confluence_integration_test.go`:

```go
//go:build integration

package source_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codanael/docserve/internal/config"
	"github.com/codanael/docserve/internal/index"
	"github.com/codanael/docserve/internal/source"
)

func TestConfluenceIntegration(t *testing.T) {
	// Set up a fake Confluence server with a 3-page tree.
	pages := map[string]map[string]interface{}{
		"100": {
			"id": "100", "title": "Documentation",
			"version":   map[string]interface{}{"number": 5},
			"ancestors": []interface{}{},
			"body": map[string]interface{}{"storage": map[string]interface{}{
				"value": `<h1>Documentation</h1><p>Welcome to our docs.</p>`,
			}},
		},
		"200": {
			"id": "200", "title": "Getting Started",
			"version": map[string]interface{}{"number": 3},
			"ancestors": []interface{}{
				map[string]interface{}{"id": "100"},
			},
			"body": map[string]interface{}{"storage": map[string]interface{}{
				"value": `<h2>Getting Started</h2>
<ac:structured-macro ac:name="info" ac:schema-version="1">
<ac:rich-text-body><p>This guide helps you set up the project.</p></ac:rich-text-body>
</ac:structured-macro>
<ac:structured-macro ac:name="code" ac:schema-version="1">
<ac:parameter ac:name="language">bash</ac:parameter>
<ac:plain-text-body><![CDATA[npm install && npm start]]></ac:plain-text-body>
</ac:structured-macro>`,
			}},
		},
		"300": {
			"id": "300", "title": "API Reference",
			"version": map[string]interface{}{"number": 7},
			"ancestors": []interface{}{
				map[string]interface{}{"id": "100"},
			},
			"body": map[string]interface{}{"storage": map[string]interface{}{
				"value": `<h2>API Reference</h2><p>Endpoints for the REST API.</p>
<table><tbody>
<tr><th><p>Method</p></th><th><p>Path</p></th></tr>
<tr><td><p>GET</p></td><td><p>/api/users</p></td></tr>
</tbody></table>`,
			}},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Single page endpoint
		for id, data := range pages {
			if r.URL.Path == "/rest/api/content/"+id {
				json.NewEncoder(w).Encode(data)
				return
			}
		}

		// Search endpoint
		if r.URL.Path == "/rest/api/content/search" {
			results := []interface{}{pages["200"], pages["300"]}
			json.NewEncoder(w).Encode(map[string]interface{}{
				"results": results,
				"start":   0,
				"limit":   200,
				"size":    2,
			})
			return
		}

		http.NotFound(w, r)
	}))
	defer srv.Close()

	// Open an in-memory store.
	store, err := index.OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()

	// Configure and create provider.
	cfg := config.SourceConfig{
		Name:     "test-confluence",
		Provider: "confluence",
		BaseURL:  srv.URL,
		Ref:      "100",
		Paths:    []string{""},
	}

	prov := source.NewConfluenceProvider(cfg, srv.Client())

	// Run the fetcher pipeline.
	dataDir := t.TempDir()
	fetcher := source.NewFetcher(store, dataDir)

	result, err := fetcher.FetchSource(context.Background(), cfg, prov)
	if err != nil {
		t.Fatalf("FetchSource: %v", err)
	}

	if !result.Updated {
		t.Error("expected Updated=true on first fetch")
	}
	if result.ChunkCount == 0 {
		t.Error("expected chunks to be indexed")
	}

	// Verify library was stored.
	lib, err := store.GetLibrary("test-confluence")
	if err != nil {
		t.Fatalf("GetLibrary: %v", err)
	}
	if lib.CommitSHA == "" {
		t.Error("CommitSHA should not be empty")
	}

	// Search for content that was in the Confluence pages.
	results, err := store.Search("test-confluence", "npm install", 5, 4000)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected search results for 'npm install'")
	}

	// Search for table content.
	results, err = store.Search("test-confluence", "api users", 5, 4000)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected search results for 'api users'")
	}

	// Second fetch with same data should be a no-op.
	result2, err := fetcher.FetchSource(context.Background(), cfg, prov)
	if err != nil {
		t.Fatalf("FetchSource (2nd): %v", err)
	}
	if result2.Updated {
		t.Error("second fetch should be a no-op (Updated=false)")
	}
}
```

- [ ] **Step 2: Run the integration test**

Run: `cd /home/agent/projects/mcp-docs && go test -tags=integration ./internal/source/ -run TestConfluenceIntegration -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/source/confluence_integration_test.go
git commit -m "test(confluence): add integration test for full fetch-index-search cycle"
```

---

### Task 11: Final verification

**Files:** None (verification only)

- [ ] **Step 1: Run all unit tests**

Run: `cd /home/agent/projects/mcp-docs && go test ./...`
Expected: ALL pass

- [ ] **Step 2: Run integration tests**

Run: `cd /home/agent/projects/mcp-docs && go test -tags=integration ./...`
Expected: ALL pass

- [ ] **Step 3: Run linter**

Run: `cd /home/agent/projects/mcp-docs && make lint`
Expected: no errors (or only pre-existing ones)

- [ ] **Step 4: Build binary**

Run: `cd /home/agent/projects/mcp-docs && make build`
Expected: binary builds successfully

- [ ] **Step 5: Verify example config**

Create a test config file and validate it parses correctly:

```bash
cd /home/agent/projects/mcp-docs
cat > /tmp/test-confluence.yaml << 'EOF'
sources:
  - name: my-confluence-docs
    provider: confluence
    base_url: https://confluence.example.com
    ref: "123456"
    depth: 3
    schedule: "0 */4 * * *"
    auth:
      type: bearer
      token_env: CONFLUENCE_PAT
EOF

# This should parse without errors (will fail on missing env var for fetch, but config parsing is the test)
./docserve fetch -config /tmp/test-confluence.yaml 2>&1 | head -5
```

Expected: config parses, error only on Confluence API connection (not config validation)
