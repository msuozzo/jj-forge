//go:build stress

package forge

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/msuozzo/jj-forge/internal/jj"
)

func stressRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("jj"); err != nil {
		t.Skip("jj not found in PATH")
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	for _, args := range [][]string{
		{"git", "init"},
		{"config", "set", "--repo", "user.name", "Stress"},
		{"config", "set", "--repo", "user.email", "stress@example.com"},
		{"config", "set", "--repo", "forge.check-command", "exit 0"},
	} {
		cmd := exec.Command("jj", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("jj %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func stressInt(key string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(key)); err == nil {
		return v
	}
	return def
}

// TestStress_ConcurrentVerdictWriters has several writers, each with its own
// ConfigManager as separate jj-forge processes would, store verdicts for
// distinct changes. Every verdict should survive.
func TestStress_ConcurrentVerdictWriters(t *testing.T) {
	dir := stressRepo(t)
	writers := stressInt("STRESS_WRITERS", 4)
	perWriter := stressInt("STRESS_PER_WRITER", 10)

	var wg sync.WaitGroup
	for w := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m := NewConfigManager(jj.NewClient(dir))
			for i := range perWriter {
				v := CheckVerdict{ChangeID: fmt.Sprintf("w%dc%d", w, i), Verdict: CheckVerdictPass, CommitID: "abc"}
				if err := m.SetCheckVerdict(v); err != nil {
					t.Errorf("SetCheckVerdict: %v", err)
				}
			}
		}()
	}
	wg.Wait()

	got, err := NewConfigManager(jj.NewClient(dir)).GetCheckVerdicts()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d of %d verdicts survived", len(got), writers*perWriter)
	if len(got) != writers*perWriter {
		t.Errorf("lost %d verdicts", writers*perWriter-len(got))
	}
}

// TestStress_ReviewRecordsSurviveVerdictWrites adds review records one at a
// time while other writers churn check verdicts, as a review open would while
// a detached check runs. Every review record should survive.
func TestStress_ReviewRecordsSurviveVerdictWrites(t *testing.T) {
	dir := stressRepo(t)
	records := stressInt("STRESS_RECORDS", 20)
	churners := stressInt("STRESS_WRITERS", 2)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	for c := range churners {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m := NewConfigManager(jj.NewClient(dir))
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
				}
				v := CheckVerdict{ChangeID: fmt.Sprintf("churn%d", c), Verdict: CheckVerdictPass, CommitID: strconv.Itoa(i)}
				if err := m.SetCheckVerdict(v); err != nil {
					t.Errorf("SetCheckVerdict: %v", err)
				}
				// Waiters poll for the lock, so a writer that retakes it the
				// moment it lets go can starve them. No jj-forge command writes
				// like that (check runs batch their verdicts), so pause as a
				// real process would between writes.
				time.Sleep(10 * time.Millisecond)
			}
		}()
	}

	m := NewConfigManager(jj.NewClient(dir))
	for i := range records {
		rec := ReviewRecord{ChangeID: fmt.Sprintf("change%d", i), ForgeID: fmt.Sprintf("pr/%d", i), URL: "https://example.com", Status: ReviewStateOpen}
		if err := m.AddReviewRecord(rec); err != nil {
			t.Errorf("AddReviewRecord: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	close(stop)
	wg.Wait()

	got, err := NewConfigManager(jj.NewClient(dir)).GetReviewRecords()
	if err != nil {
		t.Fatal(err)
	}
	cmd, err := NewConfigManager(jj.NewClient(dir)).GetCheckCommand()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d of %d review records survived, check-command=%q", len(got), records, cmd)
	if len(got) != records {
		t.Errorf("lost %d review records", records-len(got))
	}
	if cmd != "exit 0" {
		t.Errorf("forge.check-command = %q, want %q", cmd, "exit 0")
	}
}
