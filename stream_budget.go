package main

import (
	"context"
	"sync"
	"time"
)

const streamTaskTimeout = 90 * time.Second

type streamTaskBudget struct {
	mu         sync.Mutex
	ctx        context.Context
	cancel     context.CancelCauseFunc
	base       time.Duration
	deadline   time.Time
	latestWait int64
	now        func() time.Time
	waitUntil  func() int64
	timer      *time.Timer
	closed     bool
}

func newStreamTaskBudget(parent context.Context, total time.Duration, waits ...func() int64) *streamTaskBudget {
	return newStreamTaskBudgetWithClock(parent, total, time.Now, true, waits...)
}

func newStreamTaskBudgetWithClock(parent context.Context, total time.Duration, now func() time.Time, watch bool, waits ...func() int64) *streamTaskBudget {
	ctx, cancel := context.WithCancelCause(parent)
	budget := &streamTaskBudget{ctx: ctx, cancel: cancel, base: total, deadline: now().Add(total), now: now}
	if len(waits) > 0 {
		budget.waitUntil = waits[0]
	}
	if watch {
		budget.timer = time.AfterFunc(total, budget.expire)
	}
	return budget
}

func (budget *streamTaskBudget) Context() context.Context { return budget.ctx }

func (budget *streamTaskBudget) extendLocked(waitUntil int64) bool {
	if waitUntil <= budget.latestWait {
		return false
	}
	budget.latestWait = waitUntil
	candidate := time.Unix(waitUntil, 0).Add(budget.base)
	if candidate.After(budget.deadline) {
		budget.deadline = candidate
		return true
	}
	return false
}

func (budget *streamTaskBudget) Extend(waitUntil int64) bool {
	budget.mu.Lock()
	defer budget.mu.Unlock()
	if budget.closed || budget.ctx.Err() != nil {
		return false
	}
	return budget.extendLocked(waitUntil)
}

func (budget *streamTaskBudget) Remaining(waits ...int64) time.Duration {
	budget.mu.Lock()
	defer budget.mu.Unlock()
	if budget.waitUntil != nil {
		budget.extendLocked(budget.waitUntil())
	}
	if len(waits) > 0 {
		budget.extendLocked(waits[0])
	}
	remaining := budget.deadline.Sub(budget.now())
	if remaining < 0 {
		return 0
	}
	return remaining
}

func (budget *streamTaskBudget) expire() {
	budget.mu.Lock()
	defer budget.mu.Unlock()
	if budget.closed || budget.ctx.Err() != nil {
		return
	}
	if budget.waitUntil != nil {
		budget.extendLocked(budget.waitUntil())
	}
	remaining := budget.deadline.Sub(budget.now())
	if remaining > 0 {
		budget.timer.Reset(remaining)
		return
	}
	budget.cancel(context.DeadlineExceeded)
}

func (budget *streamTaskBudget) Close() {
	budget.mu.Lock()
	defer budget.mu.Unlock()
	budget.closed = true
	if budget.timer != nil {
		budget.timer.Stop()
	}
	budget.cancel(context.Canceled)
}

func streamTaskError(ctx context.Context, fallback error) error {
	if cause := context.Cause(ctx); cause != nil {
		return cause
	}
	return fallback
}

func streamTaskBudgetRemaining(task *Task) time.Duration {
	if task == nil || task.Budget == nil {
		return streamTaskTimeout
	}
	waitUntil := int64(0)
	if infos != nil {
		waitUntil = infos.WaitUntil.Load()
	}
	remaining := task.Budget.Remaining(waitUntil)
	if remaining <= 0 {
		return time.Nanosecond
	}
	return remaining
}

// waitContent consumes one completed task. Timers use the shared deadline and
// always recheck it after firing, so a new FloodWait cannot cause an early exit.
func (task *Task) waitContent(ctx context.Context) ([]byte, bool, error) {
	budget := task.Budget
	if budget == nil {
		budget = newStreamTaskBudgetWithClock(ctx, streamTaskTimeout, time.Now, false)
		defer budget.Close()
	}
	timer := time.NewTimer(budget.Remaining())
	defer timer.Stop()
	for {
		// A finished chunk remains valid if it waited behind a slow writer.
		select {
		case content, ok := <-task.Content:
			return content, ok, nil
		default:
		}
		select {
		case <-ctx.Done():
			return nil, false, streamTaskError(ctx, ctx.Err())
		case <-budget.Context().Done():
			// Workers publish content or their error before releasing the budget.
			// Prefer that completed result when both notifications are ready.
			select {
			case content, ok := <-task.Content:
				return content, ok, nil
			default:
				return nil, false, streamTaskError(budget.Context(), budget.Context().Err())
			}
		case content, ok := <-task.Content:
			return content, ok, nil
		case <-timer.C:
			if remaining := budget.Remaining(); remaining > 0 {
				timer.Reset(remaining)
				continue
			}
			return nil, false, context.DeadlineExceeded
		}
	}
}
