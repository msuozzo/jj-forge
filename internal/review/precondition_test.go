package review

import (
	"errors"
	"testing"

	"github.com/msuozzo/jj-forge/internal/forge"
	"github.com/msuozzo/jj-forge/internal/jj"
)

func TestRequireReviewExists(t *testing.T) {
	rev := &jj.Rev{ID: "aaaaaaaaaaaa"}

	if err := RequireReviewExists(rev, nil); err == nil {
		t.Error("expected error for nil record")
	}
	if err := RequireReviewExists(rev, &forge.ReviewRecord{ForgeID: "pr/1"}); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRequireReviewOpen(t *testing.T) {
	rev := &jj.Rev{ID: "aaaaaaaaaaaa"}

	tests := []struct {
		name    string
		record  *forge.ReviewRecord
		wantErr bool
	}{
		{"nil record", nil, false},
		{"open", &forge.ReviewRecord{ForgeID: "pr/1", Status: forge.ReviewStateOpen}, false},
		{"merged", &forge.ReviewRecord{ForgeID: "pr/1", Status: forge.ReviewStateMerged}, true},
		{"closed", &forge.ReviewRecord{ForgeID: "pr/1", Status: forge.ReviewStateClosed}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := RequireReviewOpen(rev, tt.record)
			if (err != nil) != tt.wantErr {
				t.Errorf("RequireReviewOpen() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRequireReviewNotClosed(t *testing.T) {
	rev := &jj.Rev{ID: "aaaaaaaaaaaa"}

	tests := []struct {
		name    string
		record  *forge.ReviewRecord
		wantErr bool
	}{
		{"nil record", nil, false},
		{"open", &forge.ReviewRecord{ForgeID: "pr/1", Status: forge.ReviewStateOpen}, false},
		{"merged", &forge.ReviewRecord{ForgeID: "pr/1", Status: forge.ReviewStateMerged}, false},
		{"closed", &forge.ReviewRecord{ForgeID: "pr/1", Status: forge.ReviewStateClosed}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := RequireReviewNotClosed(rev, tt.record)
			if (err != nil) != tt.wantErr {
				t.Errorf("RequireReviewNotClosed() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRequireReviewNotMerged(t *testing.T) {
	rev := &jj.Rev{ID: "aaaaaaaaaaaa"}

	tests := []struct {
		name    string
		record  *forge.ReviewRecord
		wantErr bool
	}{
		{"nil record", nil, false},
		{"open", &forge.ReviewRecord{ForgeID: "pr/1", Status: forge.ReviewStateOpen}, false},
		{"merged", &forge.ReviewRecord{ForgeID: "pr/1", Status: forge.ReviewStateMerged}, true},
		{"closed", &forge.ReviewRecord{ForgeID: "pr/1", Status: forge.ReviewStateClosed}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := RequireReviewNotMerged(rev, tt.record)
			if (err != nil) != tt.wantErr {
				t.Errorf("RequireReviewNotMerged() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRequireUploaded(t *testing.T) {
	check := RequireUploaded("og")

	uploaded := &jj.Rev{ID: "aaaaaaaaaaaa", RemoteBookmarks: []string{"og/push-aaaaaaaaaaaa"}}
	if err := check(uploaded, nil); err != nil {
		t.Errorf("unexpected error for uploaded rev: %v", err)
	}

	notUploaded := &jj.Rev{ID: "aaaaaaaaaaaa", RemoteBookmarks: []string{}}
	err := check(notUploaded, nil)
	if err == nil {
		t.Fatal("expected error for not-uploaded rev")
	}
	if !errors.Is(err, ErrNotUploaded) {
		t.Errorf("expected ErrNotUploaded, got: %v", err)
	}
}

func TestRequireNoParentTrailer(t *testing.T) {
	noTrailer := &jj.Rev{ID: "aaaaaaaaaaaa", Description: "feat: test\n"}
	if err := RequireNoParentTrailer(noTrailer, nil); err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	withTrailer := &jj.Rev{ID: "aaaaaaaaaaaa", Description: "feat: test\n\nforge-parent: pppppppppppp\n"}
	err := RequireNoParentTrailer(withTrailer, nil)
	if err == nil {
		t.Fatal("expected error for rev with parent trailer")
	}
	if !errors.Is(err, ErrHasParentTrailer) {
		t.Errorf("expected ErrHasParentTrailer, got: %v", err)
	}
}

func TestRequireHasDescription(t *testing.T) {
	withDesc := &jj.Rev{ID: "aaaaaaaaaaaa", Description: "feat: test\n"}
	if err := RequireHasDescription(withDesc, nil); err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	empty := &jj.Rev{ID: "aaaaaaaaaaaa", Description: ""}
	if err := RequireHasDescription(empty, nil); err == nil {
		t.Error("expected error for empty description")
	}

	whitespace := &jj.Rev{ID: "aaaaaaaaaaaa", Description: "   \n\t  "}
	if err := RequireHasDescription(whitespace, nil); err == nil {
		t.Error("expected error for whitespace-only description")
	}
}

func TestValidate_MultipleErrors(t *testing.T) {
	rev := &jj.Rev{
		ID:              "aaaaaaaaaaaa",
		Description:     "feat: test\n\nforge-parent: pppppppppppp\n",
		RemoteBookmarks: []string{},
	}
	record := &forge.ReviewRecord{ForgeID: "pr/1", Status: forge.ReviewStateMerged}

	err := Validate(rev, record,
		RequireReviewOpen,
		RequireUploaded("og"),
		RequireNoParentTrailer,
	)
	if err == nil {
		t.Fatal("expected multiple errors, got nil")
	}
	if !errors.Is(err, ErrNotUploaded) {
		t.Error("expected ErrNotUploaded in joined error")
	}
	if !errors.Is(err, ErrHasParentTrailer) {
		t.Error("expected ErrHasParentTrailer in joined error")
	}
}

func TestValidate_NoErrors(t *testing.T) {
	rev := &jj.Rev{
		ID:              "aaaaaaaaaaaa",
		Description:     "feat: test\n",
		RemoteBookmarks: []string{"og/push-aaaaaaaaaaaa"},
	}
	record := &forge.ReviewRecord{ForgeID: "pr/1", Status: forge.ReviewStateOpen}

	err := Validate(rev, record,
		RequireReviewExists,
		RequireReviewOpen,
		RequireUploaded("og"),
		RequireNoParentTrailer,
	)
	if err != nil {
		t.Errorf("expected no errors, got: %v", err)
	}
}
