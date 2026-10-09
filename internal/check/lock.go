package check

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/msuozzo/jj-forge/internal/filelock"
	"github.com/msuozzo/jj-forge/internal/ui"
)

const lockFileName = "check.lock"

// lockPollInterval is how often a waiting check retries the lock.
const lockPollInterval = 500 * time.Millisecond

// acquireLockWait takes the check lock in dir, retrying while another process
// holds it and showing the holder on the tracker.
func acquireLockWait(ctx context.Context, dir string, tracker *ui.TaskTracker) (*filelock.Lock, error) {
	setAllMessages := func(msg string) {
		for i := range tracker.Len() {
			tracker.SetMessage(i, msg)
		}
	}
	waited := false
	lastPID := 0
	lock, err := filelock.Acquire(ctx, filepath.Join(dir, lockFileName), lockPollInterval, func(held *filelock.HeldError) {
		waited = true
		if held.PID != 0 && held.PID != lastPID {
			setAllMessages(fmt.Sprintf("waiting for lock (pid %d)", held.PID))
			lastPID = held.PID
		}
	})
	if err == nil && waited {
		setAllMessages("")
	}
	return lock, err
}
