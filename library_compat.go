package main

import "github.com/xAmarnath/gogram/telegram"

// botCommandScope preserves the default scope and per-user command menus
// using the new library's typed scope instead of the old any-based overload.
func botCommandScope(peer *telegram.InputPeer) *telegram.BotCommandScope {
	var scope telegram.BotCommandScope = &telegram.BotCommandScopeDefault{}
	if peer != nil && *peer != nil {
		scope = &telegram.BotCommandScopePeer{Peer: *peer}
	}
	return &scope
}
