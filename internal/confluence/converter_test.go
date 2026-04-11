package confluence

import (
	"strings"
	"testing"
)

func TestPreprocessCDATA(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "simple CDATA",
			input: `<![CDATA[hello world]]>`,
			want:  `hello world`,
		},
		{
			name:  "CDATA with special chars",
			input: `<![CDATA[a < b & c > d]]>`,
			want:  `a &lt; b &amp; c &gt; d`,
		},
		{
			name:  "no CDATA",
			input: `<p>hello</p>`,
			want:  `<p>hello</p>`,
		},
		{
			name:  "multiple CDATA sections",
			input: `<![CDATA[first]]> and <![CDATA[second]]>`,
			want:  `first and second`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := preprocessCDATA(tt.input)
			if got != tt.want {
				t.Errorf("preprocessCDATA()\ngot:  %q\nwant: %q", got, tt.want)
			}
		})
	}
}

func TestConvert(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "heading",
			input: `<h2>Hello World</h2>`,
			want:  `## Hello World`,
		},
		{
			name:  "paragraph with bold",
			input: `<p>This is <strong>bold</strong> text.</p>`,
			want:  `This is **bold** text.`,
		},
		{
			name:  "unordered list",
			input: `<ul><li>one</li><li>two</li><li>three</li></ul>`,
			want:  "- one\n- two\n- three",
		},
		{
			name:  "external link",
			input: `<p><a href="https://example.com">Example</a></p>`,
			want:  `[Example](https://example.com)`,
		},
		{
			name: "table",
			input: `<table>
				<tr><th>Name</th><th>Value</th></tr>
				<tr><td>a</td><td>1</td></tr>
			</table>`,
			want: "| Name | Value |\n|------|-------|\n| a    | 1     |",
		},
		{
			name: "code block with language",
			input: `<ac:structured-macro ac:name="code">
				<ac:parameter ac:name="language">java</ac:parameter>
				<ac:plain-text-body><![CDATA[public class Main {}]]></ac:plain-text-body>
			</ac:structured-macro>`,
			want: "```java\npublic class Main {}\n```",
		},
		{
			name: "code block without language",
			input: `<ac:structured-macro ac:name="code">
				<ac:plain-text-body><![CDATA[echo hello]]></ac:plain-text-body>
			</ac:structured-macro>`,
			want: "```\necho hello\n```",
		},
		{
			name: "info admonition",
			input: `<ac:structured-macro ac:name="info">
				<ac:rich-text-body><p>Some info here</p></ac:rich-text-body>
			</ac:structured-macro>`,
			want: `> **Info:** Some info here`,
		},
		{
			name: "note admonition",
			input: `<ac:structured-macro ac:name="note">
				<ac:rich-text-body><p>A note</p></ac:rich-text-body>
			</ac:structured-macro>`,
			want: `> **Note:** A note`,
		},
		{
			name: "warning admonition",
			input: `<ac:structured-macro ac:name="warning">
				<ac:rich-text-body><p>Be careful</p></ac:rich-text-body>
			</ac:structured-macro>`,
			want: `> **Warning:** Be careful`,
		},
		{
			name: "tip admonition",
			input: `<ac:structured-macro ac:name="tip">
				<ac:rich-text-body><p>A tip</p></ac:rich-text-body>
			</ac:structured-macro>`,
			want: `> **Tip:** A tip`,
		},
		{
			name: "panel with title",
			input: `<ac:structured-macro ac:name="panel">
				<ac:parameter ac:name="title">Important</ac:parameter>
				<ac:rich-text-body><p>Panel content</p></ac:rich-text-body>
			</ac:structured-macro>`,
			want: "> **Important**\n> Panel content",
		},
		{
			name: "expand section",
			input: `<ac:structured-macro ac:name="expand">
				<ac:parameter ac:name="title">Details</ac:parameter>
				<ac:rich-text-body><p>Expanded content</p></ac:rich-text-body>
			</ac:structured-macro>`,
			want: "**Details**\n\nExpanded content",
		},
		{
			name: "status badge",
			input: `<ac:structured-macro ac:name="status">
				<ac:parameter ac:name="title">IN PROGRESS</ac:parameter>
			</ac:structured-macro>`,
			want: "`[IN PROGRESS]`",
		},
		{
			name:  "toc removed",
			input: `<p>Before</p><ac:structured-macro ac:name="toc"></ac:structured-macro><p>After</p>`,
			want:  "Before\n\nAfter",
		},
		{
			name:  "anchor removed",
			input: `<p>Before</p><ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">myanchor</ac:parameter></ac:structured-macro><p>After</p>`,
			want:  "Before\n\nAfter",
		},
		{
			name: "unknown macro with body",
			input: `<ac:structured-macro ac:name="fancy-widget">
				<ac:rich-text-body><p>Widget content</p></ac:rich-text-body>
			</ac:structured-macro>`,
			want: "Widget content",
		},
		{
			name:  "unknown macro without body",
			input: `<p>Before</p><ac:structured-macro ac:name="fancy-widget"></ac:structured-macro><p>After</p>`,
			want:  "Before\n\nAfter",
		},
		{
			name: "internal link ri:page",
			input: `<ac:link><ri:page ri:content-title="Getting Started" /><ac:plain-text-link-body><![CDATA[Start Here]]></ac:plain-text-link-body></ac:link>`,
			want:  "[Start Here](Getting Started)",
		},
		{
			name: "attachment link",
			input: `<ac:link><ri:attachment ri:filename="report.pdf" /><ac:plain-text-link-body><![CDATA[Download Report]]></ac:plain-text-link-body></ac:link>`,
			want:  "[Download Report](report.pdf)",
		},
		{
			name: "external link via ri:url",
			input: `<ac:link><ri:url ri:value="https://example.com" /><ac:plain-text-link-body><![CDATA[Example]]></ac:plain-text-link-body></ac:link>`,
			want:  "[Example](https://example.com)",
		},
		{
			name:  "image from attachment",
			input: `<ac:image><ri:attachment ri:filename="diagram.png" /></ac:image>`,
			want:  "![diagram.png](diagram.png)",
		},
		{
			name:  "image from URL",
			input: `<ac:image><ri:url ri:value="https://example.com/img.png" /></ac:image>`,
			want:  "![image](https://example.com/img.png)",
		},
		{
			name: "task list",
			input: `<ac:task-list>
				<ac:task>
					<ac:task-id>1</ac:task-id>
					<ac:task-status>complete</ac:task-status>
					<ac:task-body>Done task</ac:task-body>
				</ac:task>
				<ac:task>
					<ac:task-id>2</ac:task-id>
					<ac:task-status>incomplete</ac:task-status>
					<ac:task-body>Todo task</ac:task-body>
				</ac:task>
			</ac:task-list>`,
			want: "- [x] Done task\n- [ ] Todo task",
		},
		{
			name: "layout flattening",
			input: `<ac:layout><ac:layout-section ac:type="two_equal">
				<ac:layout-cell><p>Left</p></ac:layout-cell>
				<ac:layout-cell><p>Right</p></ac:layout-cell>
			</ac:layout-section></ac:layout>`,
			want: "Left\n\nRight",
		},
		{
			name: "realistic full page",
			input: `<h1>Project Setup</h1>
<ac:structured-macro ac:name="toc"></ac:structured-macro>
<p>Welcome to the <strong>project</strong> setup guide.</p>
<ac:structured-macro ac:name="info">
	<ac:rich-text-body><p>Ensure you have admin access.</p></ac:rich-text-body>
</ac:structured-macro>
<h2>Installation</h2>
<ac:structured-macro ac:name="code">
	<ac:parameter ac:name="language">bash</ac:parameter>
	<ac:plain-text-body><![CDATA[npm install my-package]]></ac:plain-text-body>
</ac:structured-macro>
<p>See <ac:link><ri:page ri:content-title="Configuration Guide" /><ac:plain-text-link-body><![CDATA[Configuration Guide]]></ac:plain-text-link-body></ac:link> for details.</p>
<ac:structured-macro ac:name="warning">
	<ac:rich-text-body><p>Do not run in production without testing.</p></ac:rich-text-body>
</ac:structured-macro>
<ac:task-list>
	<ac:task>
		<ac:task-id>1</ac:task-id>
		<ac:task-status>complete</ac:task-status>
		<ac:task-body>Install dependencies</ac:task-body>
	</ac:task>
	<ac:task>
		<ac:task-id>2</ac:task-id>
		<ac:task-status>incomplete</ac:task-status>
		<ac:task-body>Run tests</ac:task-body>
	</ac:task>
</ac:task-list>`,
			want: `# Project Setup

Welcome to the **project** setup guide.

> **Info:** Ensure you have admin access.

## Installation

` + "```bash\nnpm install my-package\n```" + `

See [Configuration Guide](Configuration Guide) for details.

> **Warning:** Do not run in production without testing.

- [x] Install dependencies
- [ ] Run tests`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Convert(tt.input)
			if err != nil {
				t.Fatalf("Convert() error: %v", err)
			}

			got = strings.TrimSpace(got)
			want := strings.TrimSpace(tt.want)

			if got != want {
				t.Errorf("Convert()\ngot:\n%s\n\nwant:\n%s", got, want)
			}
		})
	}
}
