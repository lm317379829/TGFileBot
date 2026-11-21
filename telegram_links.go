package main

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	telegramLinkStartRe = regexp.MustCompile(`(?i)(?:https?://)?t\.me/`)
	telegramLinkPathRe  = regexp.MustCompile(`^/(c/([0-9]+)|([a-zA-Z0-9_]+))/([0-9]+)/?$`)
)

// Preserve hackLinks' six fields while parsing each URL in isolation. A comment
// belongs to that URL's exact query key, never to neighboring prose or links.
func findTelegramLinks(text string) [][]string {
	var links [][]string
	starts := telegramLinkStartRe.FindAllStringIndex(text, -1)
	for i, span := range starts {
		if span[0] > 0 {
			previous, _ := utf8.DecodeLastRuneInString(text[:span[0]])
			if previous <= unicode.MaxASCII && (unicode.IsLetter(previous) || unicode.IsDigit(previous) || strings.ContainsRune("_@./", previous)) {
				continue
			}
		}
		end := len(text)
		if i+1 < len(starts) {
			end = starts[i+1][0]
		}
		candidate := text[span[0]:end]
		if stop := strings.IndexFunc(candidate, func(r rune) bool {
			return unicode.IsSpace(r) || strings.ContainsRune("<>\"`", r)
		}); stop >= 0 {
			candidate = candidate[:stop]
		}
		candidate = strings.TrimRight(candidate, ".,;!)]}，。；！）】》")
		normalized := candidate
		if !strings.HasPrefix(strings.ToLower(normalized), "http://") && !strings.HasPrefix(strings.ToLower(normalized), "https://") {
			normalized = "https://" + normalized
		}
		parsed, err := url.Parse(normalized)
		if err != nil || !strings.EqualFold(parsed.Host, "t.me") || parsed.User != nil {
			continue
		}
		fields := telegramLinkPathRe.FindStringSubmatch(parsed.Path)
		if len(fields) != 5 {
			continue
		}
		mid, err := strconv.ParseInt(fields[4], 10, 32)
		if err != nil || mid <= 0 {
			continue
		}
		comment := ""
		for _, key := range []string{"comment", "thread"} {
			value := parsed.Query().Get(key)
			if n, err := strconv.ParseInt(value, 10, 32); err == nil && n > 0 {
				comment = value
				break
			}
		}
		links = append(links, []string{candidate, fields[1], fields[2], fields[3], fields[4], comment})
	}
	return links
}
