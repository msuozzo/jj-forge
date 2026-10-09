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

// acquireLockWait takes the check lock in dir, waiting while another process
// holds it and saying which one.
func acquireLockWait(ctx context.Context, dir string, u *ui.UI) (*filelock.Lock, error) {
	announced := false
	lastPID := 0
	return filelock.Acquire(ctx, filepath.Join(dir, lockFileName), lockPollInterval, func(held *filelock.HeldError) {
		if announced && (held.PID == 0 || held.PID == lastPID) {
			return
		}
		if held.PID != 0 {
			fmt.Fprintf(u, "Waiting for the check run in pid %d...\n", held.PID)
		} else {
			fmt.Fprintf(u, "Waiting for another check run...\n")
		}
		announced = true
		lastPID = held.PID
	})
}
