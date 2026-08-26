package jobs

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

// TestHandlerPanicFailsJobNotProcess proves the crash-chain fix: a
// panicking job handler must land in the failed counter while the
// worker system keeps running.
func TestHandlerPanicFailsJobNotProcess(t *testing.T) {
	s := NewSystem(slog.New(slog.DiscardHandler))
	s.Register("PanickyJob", QueueBackgroundTask, func(ctx context.Context, name string, data any) error {
		panic("boom")
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	defer s.Stop()

	if err := s.Queue("PanickyJob", nil); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		c, st := s.Counts()[QueueBackgroundTask].Counts, s.Counts()[QueueBackgroundTask].Status
		_ = st
		if c.Failed >= 1 && c.Active == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("panicking job never recorded as failed")
}

func TestStopDrainsWorkers(t *testing.T) {
	s := NewSystem(slog.New(slog.DiscardHandler))
	ran := make(chan struct{}, 1)
	s.Register("QuickJob", QueueBackgroundTask, func(ctx context.Context, name string, data any) error {
		ran <- struct{}{}
		return nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	s.Start(ctx)
	_ = s.Queue("QuickJob", nil)
	select {
	case <-ran:
	case <-time.After(3 * time.Second):
		t.Fatal("job never ran")
	}
	cancel()
	s.Stop()
	// Queueing after Stop must not panic; the job simply never runs.
	_ = s.Queue("QuickJob", nil)
}
