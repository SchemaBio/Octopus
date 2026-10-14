package retention

import (
	"context"
	"testing"
	"time"
)

func TestStopCancelsAndDrainsOnlyItsTask(t *testing.T) {
	a, doneA := Track(context.Background(), t.Name()+"a")
	b, doneB := Track(context.Background(), t.Name()+"b")
	defer doneB()
	finished := make(chan error, 1)
	go func() { finished <- Stop(context.Background(), t.Name()+"a") }()
	select {
	case <-a.Done():
	case <-time.After(time.Second):
		t.Fatal("work not cancelled")
	}
	select {
	case <-finished:
		t.Fatal("erasure started before worker drained")
	default:
	}
	if b.Err() != nil {
		t.Fatal("another task cancelled")
	}
	doneA()
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	late, done := Track(context.Background(), t.Name()+"a")
	defer done()
	if late.Err() == nil {
		t.Fatal("late work accepted")
	}
}
