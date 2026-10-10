package service

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"
)

// Admit before loading overlays or decoding large result requests. The engine's
// own limiter alone would be too late to protect ordinary APIs from that work.
var resultOperationSlots = make(chan struct{}, 2)

type resultLeaseKey struct{}

func admitResultOperation(ctx context.Context) (context.Context, func(), error) {
	if ctx.Value(resultLeaseKey{}) == true {
		return ctx, func() {}, nil
	}
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	select {
	case resultOperationSlots <- struct{}{}:
		return context.WithValue(ctx, resultLeaseKey{}, true), func() { <-resultOperationSlots }, nil
	case <-ctx.Done():
		return ctx, nil, ctx.Err()
	case <-timer.C:
		return ctx, nil, fmt.Errorf("query_capacity")
	}
}

type leasedExportBody struct {
	io.ReadCloser
	release func()
	once    sync.Once
	err     error
}

func (b *leasedExportBody) Close() error {
	b.once.Do(func() { defer b.release(); b.err = b.ReadCloser.Close() })
	return b.err
}
