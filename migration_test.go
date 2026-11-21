package main

import (
	"context"
	"errors"
	"github.com/xAmarnath/gogram/telegram"
	"testing"
	"time"
)

var _ connectClient = (*telegram.Client)(nil)

func TestCommandScopes(t *testing.T) {
	scope := botCommandScope(nil)
	if _, ok := (*scope).(*telegram.BotCommandScopeDefault); !ok {
		t.Fatalf("nil scope: %T", *scope)
	}
	var peer telegram.InputPeer = &telegram.InputPeerUser{UserID: 42, AccessHash: 99}
	scope = botCommandScope(&peer)
	got, ok := (*scope).(*telegram.BotCommandScopePeer)
	if !ok || got.Peer != peer {
		t.Fatalf("peer scope: %+v", *scope)
	}
}
func TestDownloadUsesCancelableContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := &telegram.Client{}
	_, _, err := c.DownloadChunkContext(ctx, nil, 0, 1048576, 1048576, &telegram.DownloadOptions{RequestTimeout: 8 * time.Second, WorkerPool: nil})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
