package retention

import (
	"context"
	"sync"
)

type work struct {
	blocked bool
	next    uint64
	active  map[uint64]context.CancelFunc
	drained chan struct{}
}

var registry = struct {
	sync.Mutex
	tasks map[string]*work
}{tasks: map[string]*work{}}

// Track permits cancellation and draining of in-process work before erasure.
func Track(ctx context.Context, task string) (context.Context, func()) {
	registry.Lock()
	w := registry.tasks[task]
	if w == nil {
		w = &work{active: map[uint64]context.CancelFunc{}}
		registry.tasks[task] = w
	}
	child, cancel := context.WithCancel(ctx)
	if w.blocked {
		registry.Unlock()
		cancel()
		return child, func() {}
	}
	if len(w.active) == 0 {
		w.drained = make(chan struct{})
	}
	w.next++
	id := w.next
	w.active[id] = cancel
	registry.Unlock()
	var once sync.Once
	return child, func() {
		once.Do(func() {
			cancel()
			registry.Lock()
			delete(w.active, id)
			if len(w.active) == 0 {
				close(w.drained)
				if !w.blocked {
					delete(registry.tasks, task)
				}
			}
			registry.Unlock()
		})
	}
}

func Stop(ctx context.Context, task string) error {
	registry.Lock()
	w := registry.tasks[task]
	if w == nil {
		w = &work{blocked: true, active: map[uint64]context.CancelFunc{}}
		registry.tasks[task] = w
	}
	w.blocked = true
	for _, cancel := range w.active {
		cancel()
	}
	if len(w.active) == 0 {
		registry.Unlock()
		return nil
	}
	drained := w.drained
	registry.Unlock()
	select {
	case <-drained:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
