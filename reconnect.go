package main

import (
	"context"
	"errors"
	"sync"
	"time"
)

// tcpWakeState coalesces work for one client category. Its mutex protects only
// bookkeeping; no request holds it while the shared connection is probed.
type tcpWakeState struct {
	sync.Mutex
	call   *tcpWakeCall
	client connectClient
}
type tcpWakeCall struct {
	client connectClient
	done   chan struct{}
	err    error
}

func (infos *Infos) wakeTCPClientContext(ctx context.Context, client connectClient, cate string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if client == nil {
		return errors.New("client 不能为 nil")
	}
	state := infos.connectMu(cate)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		state.Lock()
		if state.call == nil {
			stat := infos.connectStat(cate)
			if state.client == client && !stat.TCPDead.Load() && stat.since() < 5*time.Second {
				state.Unlock()
				return nil
			}
			call := &tcpWakeCall{client: client, done: make(chan struct{}), err: errors.New("TCP 探活任务异常结束")}
			state.call = call
			safeGo(func() {
				// Shared maintenance survives cancellation of an individual HTTP waiter.
				defer func() { state.Lock(); state.call = nil; state.client = client; close(call.done); state.Unlock() }()
				workCtx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
				defer cancel()
				call.err = infos.probeTCP(workCtx, client, cate)
			})
		}
		call := state.call
		state.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-call.done:
			if err := ctx.Err(); err != nil {
				return err
			}
			if call.client == client {
				return call.err
			}
		}
	}
}

func (infos *Infos) probeTCP(ctx context.Context, client connectClient, cate string) error {
	stat := infos.connectStat(cate)
	ping := func() (time.Duration, error) {
		pingCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		return client.PingContext(pingCtx)
	}
	latency, err := ping()
	if err == nil {
		stat.wake(latency.Milliseconds())
		return nil
	}
	for attempt := 1; attempt <= 3; attempt++ {
		if err = ctx.Err(); err != nil {
			break
		}
		// MTProto.Reconnect resets transport without stopping the Telegram client's
		// update dispatcher, shared sender cache, or other background services.
		err = client.Reconnect(ctx, true)
		if err == nil {
			latency, err = ping()
			if err == nil {
				stat.wake(latency.Milliseconds())
				return nil
			}
		}
		if attempt < 3 {
			timer := time.NewTimer(time.Duration(attempt) * 500 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				err = ctx.Err()
			case <-timer.C:
			}
		}
	}
	stat.markDead()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}
