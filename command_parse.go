package main

import (
	"strings"
	"unicode"
)

// Parse one Telegram command token. Arguments keep their original spacing and
// case; an addressed command is accepted only for this bot.
func parseBotCommand(text, botUsername string) (name, args string) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return "", ""
	}
	token := text
	if i := strings.IndexFunc(text, unicode.IsSpace); i >= 0 {
		token = text[:i]
		args = strings.TrimSpace(text[i:])
	}
	if command, target, addressed := strings.Cut(token, "@"); addressed {
		if botUsername == "" || target == "" || !strings.EqualFold(target, strings.TrimPrefix(botUsername, "@")) {
			return "", ""
		}
		token = command
	}
	name = strings.ToLower(token)
	if len(name) < 2 || len(name) > 33 {
		return "", ""
	}
	for _, r := range name[1:] {
		if !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '_' {
			return "", ""
		}
	}
	return name, args
}
