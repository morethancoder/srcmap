package updater_test

import (
	"testing"
	"time"

	"github.com/morethancoder/srcmap/internal/updater"
)

func TestFingerprintUnchanged(t *testing.T) {
	store := updater.NewFingerprintStore()
	store.Set("file1.ts", "abc123")
	store.Set("file2.ts", "def456")

	current := map[string]string{
		"file1.ts": "abc123",
		"file2.ts": "def456",
	}

	diff := updater.ComputeDiff(store, current)
	if !diff.IsEmpty() {
		t.Errorf("expected no changes, got new=%d changed=%d removed=%d",
			len(diff.New), len(diff.Changed), len(diff.Removed))
	}
}

func TestFingerprintChanged(t *testing.T) {
	store := updater.NewFingerprintStore()
	store.Set("file1.ts", "abc123")
	store.Set("file2.ts", "def456")
	store.Set("file3.ts", "ghi789")

	current := map[string]string{
		"file1.ts": "abc123",  // unchanged
		"file2.ts": "xyz999",  // changed
		"file4.ts": "new111",  // new
		// file3.ts removed
	}

	diff := updater.ComputeDiff(store, current)

	if len(diff.New) != 1 || diff.New[0] != "file4.ts" {
		t.Errorf("new: got %v, want [file4.ts]", diff.New)
	}
	if len(diff.Changed) != 1 || diff.Changed[0] != "file2.ts" {
		t.Errorf("changed: got %v, want [file2.ts]", diff.Changed)
	}
	if len(diff.Removed) != 1 || diff.Removed[0] != "file3.ts" {
		t.Errorf("removed: got %v, want [file3.ts]", diff.Removed)
	}
}

func TestStalenessDetection(t *testing.T) {
	// 48h ago with 24h interval → stale
	lastChecked := time.Now().Add(-48 * time.Hour)
	if !updater.CheckStaleness(lastChecked, 24*time.Hour) {
		t.Error("expected stale for 48h-old check with 24h interval")
	}

	// 1h ago with 24h interval → not stale
	recentCheck := time.Now().Add(-1 * time.Hour)
	if updater.CheckStaleness(recentCheck, 24*time.Hour) {
		t.Error("expected not stale for 1h-old check with 24h interval")
	}

	// Zero time → always stale
	if !updater.CheckStaleness(time.Time{}, 24*time.Hour) {
		t.Error("expected stale for zero time")
	}
}

func TestChangelogEntry(t *testing.T) {
	diff := &updater.DiffResult{
		ChangedSymbols: []string{"sendMessage"},
	}

	entry := updater.FormatChangelogEntry(diff)
	if entry == "" {
		t.Error("expected non-empty changelog entry")
	}
	if entry == "No changes detected." {
		t.Error("should detect the changed symbol")
	}
}

func TestDiffLayerTotalChanges(t *testing.T) {
	layer := &updater.DiffLayer{
		New:     []string{"a", "b"},
		Changed: []string{"c"},
		Removed: []string{"d", "e", "f"},
	}
	if layer.TotalChanges() != 6 {
		t.Errorf("expected 6 total changes, got %d", layer.TotalChanges())
	}
}

func TestParseInterval(t *testing.T) {
	tests := []struct {
		input string
		want  time.Duration
	}{
		{"24h", 24 * time.Hour},
		{"12h", 12 * time.Hour},
		{"7d", 7 * 24 * time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := updater.ParseInterval(tt.input)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFingerprintFromContent(t *testing.T) {
	hash1 := updater.Fingerprint([]byte("hello world"))
	hash2 := updater.Fingerprint([]byte("hello world"))
	hash3 := updater.Fingerprint([]byte("hello world!"))

	if hash1 != hash2 {
		t.Error("same content should produce same hash")
	}
	if hash1 == hash3 {
		t.Error("different content should produce different hash")
	}
	if hash1 == "" {
		t.Error("hash should not be empty")
	}
}
