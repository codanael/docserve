package confluence

import (
	"bytes"
	"log"
	"strings"

	"github.com/JohannesKaufmann/dom"
	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"golang.org/x/net/html"
)

type confluencePlugin struct{}

func newConfluencePlugin() *confluencePlugin {
	return &confluencePlugin{}
}

func (p *confluencePlugin) Name() string {
	return "confluence"
}

func (p *confluencePlugin) Init(conv *converter.Converter) error {
	// Register a single renderer that dispatches on tag name.
	conv.Register.Renderer(p.handleRender, converter.PriorityEarly)

	// Register tag types for Confluence elements so the fallback
	// renderer doesn't insert spurious blank lines.
	blockTags := []string{
		"ac:structured-macro", "ac:rich-text-body", "ac:plain-text-body",
		"ac:task-list", "ac:task", "ac:task-body",
		"ac:layout", "ac:layout-section", "ac:layout-cell",
	}
	for _, tag := range blockTags {
		conv.Register.TagType(tag, converter.TagTypeBlock, converter.PriorityEarly)
	}

	inlineTags := []string{
		"ac:link", "ac:image", "ac:emoticon",
		"ac:parameter", "ac:plain-text-link-body", "ac:link-body",
		"ri:page", "ri:attachment", "ri:url", "ri:user", "ri:space",
		"ri:content-entity", "ri:shortcut",
	}
	for _, tag := range inlineTags {
		conv.Register.TagType(tag, converter.TagTypeInline, converter.PriorityEarly)
	}

	return nil
}

func (p *confluencePlugin) handleRender(ctx converter.Context, w converter.Writer, n *html.Node) converter.RenderStatus {
	name := dom.NodeName(n)

	switch name {
	case "ac:structured-macro":
		return p.renderStructuredMacro(ctx, w, n)
	case "ac:link":
		return p.renderLink(ctx, w, n)
	case "ac:image":
		return p.renderImage(ctx, w, n)
	case "ac:emoticon":
		return converter.RenderSuccess // remove
	case "ac:task-list":
		return p.renderTaskList(ctx, w, n)
	case "ac:task":
		return p.renderTask(ctx, w, n)
	case "ac:layout", "ac:layout-section", "ac:layout-cell":
		ctx.RenderChildNodes(ctx, w, n)
		return converter.RenderSuccess
	case "ac:parameter":
		return converter.RenderSuccess // consumed by parent
	case "ac:rich-text-body":
		ctx.RenderChildNodes(ctx, w, n)
		return converter.RenderSuccess
	case "ac:plain-text-body":
		w.WriteString(getTextContent(n)) //nolint:errcheck
		return converter.RenderSuccess
	}

	// ri:* elements are consumed by parent handlers
	if strings.HasPrefix(name, "ri:") {
		return converter.RenderSuccess
	}

	return converter.RenderTryNext
}

// --- Structured Macro ---

// macros that should be removed entirely (navigation/metadata)
var removedMacros = map[string]bool{
	"toc": true, "anchor": true, "attachments": true, "children": true,
	"contentbylabel": true, "excerpt-include": true, "include": true,
	"jiraissues": true, "details": true,
}

// admonition macros
var admonitionMacros = map[string]string{
	"info": "Info", "note": "Note", "warning": "Warning", "tip": "Tip",
}

func (p *confluencePlugin) renderStructuredMacro(ctx converter.Context, w converter.Writer, n *html.Node) converter.RenderStatus {
	macroName := getAttr(n, "ac:name")

	// Removed macros
	if removedMacros[macroName] {
		return converter.RenderSuccess
	}

	switch macroName {
	case "code":
		return p.renderCodeBlock(ctx, w, n)
	case "panel":
		return p.renderPanel(ctx, w, n)
	case "expand":
		return p.renderExpand(ctx, w, n)
	case "status":
		return p.renderStatus(ctx, w, n)
	case "excerpt":
		return p.renderExcerpt(ctx, w, n)
	case "section", "column":
		// Flatten: render body content directly
		if body := getRichTextBody(n); body != nil {
			ctx.RenderChildNodes(ctx, w, body)
		}
		return converter.RenderSuccess
	}

	// Admonitions
	if label, ok := admonitionMacros[macroName]; ok {
		return p.renderAdmonition(ctx, w, n, label)
	}

	// Unknown macro: render body if present, else remove
	if body := getRichTextBody(n); body != nil {
		log.Printf("[confluence] unknown macro %q — rendering body content", macroName)
		ctx.RenderChildNodes(ctx, w, body)
		return converter.RenderSuccess
	}

	log.Printf("[confluence] unknown macro %q — removing (no body)", macroName)
	return converter.RenderSuccess
}

func (p *confluencePlugin) renderCodeBlock(_ converter.Context, w converter.Writer, n *html.Node) converter.RenderStatus {
	lang := getMacroParam(n, "language")
	body := getPlainTextBody(n)

	w.WriteString("\n\n```" + lang + "\n") //nolint:errcheck
	w.WriteString(body)                  //nolint:errcheck
	if body != "" && !strings.HasSuffix(body, "\n") {
		w.WriteString("\n") //nolint:errcheck
	}
	w.WriteString("```\n\n") //nolint:errcheck
	return converter.RenderSuccess
}

func (p *confluencePlugin) renderAdmonition(ctx converter.Context, w converter.Writer, n *html.Node, label string) converter.RenderStatus {
	body := getRichTextBody(n)
	content := ""
	if body != nil {
		content = renderChildrenToString(ctx, body)
	}
	content = strings.TrimSpace(content)

	w.WriteString("\n\n> **" + label + ":** " + content + "\n\n") //nolint:errcheck
	return converter.RenderSuccess
}

func (p *confluencePlugin) renderPanel(ctx converter.Context, w converter.Writer, n *html.Node) converter.RenderStatus {
	title := getMacroParam(n, "title")
	body := getRichTextBody(n)
	content := ""
	if body != nil {
		content = renderChildrenToString(ctx, body)
	}
	content = strings.TrimSpace(content)

	w.WriteString("\n\n") //nolint:errcheck
	if title != "" {
		w.WriteString("> **" + title + "**\n> " + content + "\n\n") //nolint:errcheck
	} else {
		w.WriteString("> " + content + "\n\n") //nolint:errcheck
	}
	return converter.RenderSuccess
}

func (p *confluencePlugin) renderExpand(ctx converter.Context, w converter.Writer, n *html.Node) converter.RenderStatus {
	title := getMacroParam(n, "title")
	if title == "" {
		title = "Click to expand..."
	}
	body := getRichTextBody(n)
	content := ""
	if body != nil {
		content = renderChildrenToString(ctx, body)
	}
	content = strings.TrimSpace(content)

	w.WriteString("\n\n**" + title + "**\n\n" + content + "\n\n") //nolint:errcheck
	return converter.RenderSuccess
}

func (p *confluencePlugin) renderStatus(_ converter.Context, w converter.Writer, n *html.Node) converter.RenderStatus {
	title := getMacroParam(n, "title")
	w.WriteString("`[" + title + "]`") //nolint:errcheck
	return converter.RenderSuccess
}

func (p *confluencePlugin) renderExcerpt(ctx converter.Context, w converter.Writer, n *html.Node) converter.RenderStatus {
	if body := getRichTextBody(n); body != nil {
		ctx.RenderChildNodes(ctx, w, body)
	}
	return converter.RenderSuccess
}

// --- Links ---

func (p *confluencePlugin) renderLink(ctx converter.Context, w converter.Writer, n *html.Node) converter.RenderStatus {
	linkText := getLinkText(ctx, n)

	// ri:page link
	if page := findChild(n, "ri:page"); page != nil {
		title := getAttr(page, "ri:content-title")
		if linkText == "" {
			linkText = title
		}
		w.WriteString("[" + linkText + "](" + title + ")") //nolint:errcheck
		return converter.RenderSuccess
	}

	// ri:attachment link
	if att := findChild(n, "ri:attachment"); att != nil {
		filename := getAttr(att, "ri:filename")
		if linkText == "" {
			linkText = filename
		}
		w.WriteString("[" + linkText + "](" + filename + ")") //nolint:errcheck
		return converter.RenderSuccess
	}

	// ri:url link (external)
	if urlNode := findChild(n, "ri:url"); urlNode != nil {
		href := getAttr(urlNode, "ri:value")
		if linkText == "" {
			linkText = href
		}
		w.WriteString("[" + linkText + "](" + href + ")") //nolint:errcheck
		return converter.RenderSuccess
	}

	// Fallback: render children
	ctx.RenderChildNodes(ctx, w, n)
	return converter.RenderSuccess
}

// --- Images ---

func (p *confluencePlugin) renderImage(_ converter.Context, w converter.Writer, n *html.Node) converter.RenderStatus {
	// ri:attachment image
	if att := findChild(n, "ri:attachment"); att != nil {
		filename := getAttr(att, "ri:filename")
		w.WriteString("![" + filename + "](" + filename + ")") //nolint:errcheck
		return converter.RenderSuccess
	}

	// ri:url image
	if urlNode := findChild(n, "ri:url"); urlNode != nil {
		href := getAttr(urlNode, "ri:value")
		w.WriteString("![image](" + href + ")") //nolint:errcheck
		return converter.RenderSuccess
	}

	return converter.RenderSuccess
}

// --- Task Lists ---

func (p *confluencePlugin) renderTaskList(ctx converter.Context, w converter.Writer, n *html.Node) converter.RenderStatus {
	w.WriteString("\n\n") //nolint:errcheck
	ctx.RenderChildNodes(ctx, w, n)
	w.WriteString("\n") //nolint:errcheck
	return converter.RenderSuccess
}

func (p *confluencePlugin) renderTask(ctx converter.Context, w converter.Writer, n *html.Node) converter.RenderStatus {
	status := findChild(n, "ac:task-status")
	checked := status != nil && strings.TrimSpace(dom.CollectText(status)) == "complete"

	body := findChild(n, "ac:task-body")
	content := ""
	if body != nil {
		content = strings.TrimSpace(renderChildrenToString(ctx, body))
	}

	if checked {
		w.WriteString("- [x] " + content + "\n") //nolint:errcheck
	} else {
		w.WriteString("- [ ] " + content + "\n") //nolint:errcheck
	}
	return converter.RenderSuccess
}

// --- Helpers ---

func getAttr(n *html.Node, key string) string {
	for _, attr := range n.Attr {
		if attr.Key == key {
			return attr.Val
		}
	}
	return ""
}

// findChild finds the first descendant element with the given tag name.
// We search recursively because Go's HTML parser does not treat custom
// elements as void/self-closing, so <ri:page ... /> may swallow
// subsequent siblings as children.
func findChild(n *html.Node, tag string) *html.Node {
	return dom.FindFirstNode(n, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == tag
	})
}

func getMacroParam(n *html.Node, paramName string) string {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == "ac:parameter" && getAttr(c, "ac:name") == paramName {
			return strings.TrimSpace(dom.CollectText(c))
		}
	}
	return ""
}

func getRichTextBody(n *html.Node) *html.Node {
	return findChild(n, "ac:rich-text-body")
}

func getPlainTextBody(n *html.Node) string {
	child := findChild(n, "ac:plain-text-body")
	if child == nil {
		return ""
	}
	return dom.CollectText(child)
}

func getTextContent(n *html.Node) string {
	return dom.CollectText(n)
}

func getLinkText(ctx converter.Context, n *html.Node) string {
	// First try ac:plain-text-link-body
	if ptlb := findChild(n, "ac:plain-text-link-body"); ptlb != nil {
		return strings.TrimSpace(dom.CollectText(ptlb))
	}
	// Then try ac:link-body (rich text)
	if lb := findChild(n, "ac:link-body"); lb != nil {
		return strings.TrimSpace(renderChildrenToString(ctx, lb))
	}
	return ""
}

func renderChildrenToString(ctx converter.Context, n *html.Node) string {
	var buf bytes.Buffer
	ctx.RenderChildNodes(ctx, &buf, n)
	return buf.String()
}

// Ensure confluencePlugin satisfies the Plugin interface.
var _ converter.Plugin = (*confluencePlugin)(nil)

// Ensure bytes.Buffer satisfies converter.Writer (it does by stdlib).
var _ converter.Writer = (*bytes.Buffer)(nil)

