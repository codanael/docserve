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

func preprocess(input string) string {
	input = preprocessCDATA(input)
	input = strings.ReplaceAll(input, "&nbsp;", "&#160;")
	return "<div>" + input + "</div>"
}

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
			newConfluencePlugin(),
		),
	)

	md, err := conv.ConvertString(prepared)
	if err != nil {
		return "", err
	}

	return collapseBlankLines(md), nil
}
