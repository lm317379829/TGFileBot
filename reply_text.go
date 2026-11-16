package main

import (
	"errors"
	"fmt"
	"html"
	"strings"
)

// Counting the encoded HTML bytes is conservative: the visible text, including
// UTF-16 surrogate pairs, stays below Telegram's 4096-character limit as well.
const maxReplyHTMLBytes = 4000

func logReplyPages(lines []string) []string {
	if len(lines) == 0 {
		return nil
	}
	var pages []string
	var page strings.Builder
	page.WriteString(fmt.Sprintf("<b>📜 系统日志 (最后 %d 行)</b>\n\n<pre>", len(lines)))
	hasText := false
	flush := func() {
		if hasText {
			pages = append(pages, page.String()+"</pre>")
		}
		page.Reset()
		page.WriteString("<pre>")
		hasText = false
	}
	appendText := func(text string) {
		for _, r := range text {
			escaped := html.EscapeString(string(r))
			if page.Len()+len(escaped)+len("</pre>") > maxReplyHTMLBytes {
				flush()
			}
			page.WriteString(escaped)
			hasText = true
		}
	}
	for _, line := range lines {
		appendText(line)
		appendText("\n")
	}
	flush()
	return pages
}

func linkReplyPages(links []string) ([]string, error) {
	if len(links) == 0 {
		return nil, nil
	}
	heading := fmt.Sprintf("<b>🔗 成功提取 %d 个链接</b>\n\n", len(links))
	var pages []string
	var page strings.Builder
	page.WriteString(heading)
	for i, link := range links {
		escaped := html.EscapeString(link)
		entry := fmt.Sprintf("<b>%d.</b> <code>%s</code>\n👉 <a href=\"%s&amp;download=true\">点击直接下载</a>\n\n", i+1, escaped, escaped)
		if len(heading)+len(entry) > maxReplyHTMLBytes {
			// Preserve one copyable URL when repeating it in a clickable anchor
			// would be too large. Never split a URL or an HTML entity.
			entry = fmt.Sprintf("<b>%d.</b> <code>%s</code>\n\n", i+1, escaped)
			if len(heading)+len(entry) > maxReplyHTMLBytes {
				return nil, errors.New("单个链接过长，无法放入 Telegram 消息")
			}
		}
		if page.Len()+len(entry) > maxReplyHTMLBytes {
			pages = append(pages, page.String())
			page.Reset()
			page.WriteString(heading)
		}
		page.WriteString(entry)
	}
	pages = append(pages, page.String())
	return pages, nil
}
