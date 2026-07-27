package terminal

import (
	"context"
	"time"
)

const shellStartupWait = 5 * time.Second

func warnWhenSilent(ctx context.Context, firstOutput <-chan struct{}, wait time.Duration, warn func()) {
	timer := time.NewTimer(wait)
	defer timer.Stop()

	select {
	case <-firstOutput:
	case <-ctx.Done():
	case <-timer.C:
		select {
		case <-firstOutput:
		default:
			warn()
		}
	}
}
