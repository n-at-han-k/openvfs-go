package directory

import (
	"os"
	"path/filepath"
	"testing"
)

func newTestScanner(t *testing.T, root string, ignored func(string) bool) *Scanner {
	t.Helper()
	j, err := OpenJournal(filepath.Join(t.TempDir(), "sync.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = j.Close() })
	return NewScanner(root, ignored, j)
}

func TestDiff(t *testing.T) {
	prev := map[string]Meta{
		"gone.txt": {},
		"same.txt": {Size: 1},
		"changed.txt": {Size: 1},
	}
	fresh := map[string]Meta{
		"new.txt":    {Size: 2},
		"same.txt":   {Size: 1},
		"changed.txt": {Size: 2},
	}

	diff := calculateDiff(prev, fresh)
	if len(diff.Added) != 1 || diff.Added[0].Path != "new.txt" {
		t.Errorf("Added = %v", diff.Added)
	}
	if len(diff.Modified) != 1 || diff.Modified[0].Path != "changed.txt" {
		t.Errorf("Modified = %v", diff.Modified)
	}
	if len(diff.Removed) != 1 || diff.Removed[0].Path != "gone.txt" {
		t.Errorf("Removed = %v", diff.Removed)
	}

	events := diff.Events()
	if len(events) != 3 || events[0].Path != "changed.txt" || events[1].Path != "gone.txt" || events[2].Path != "new.txt" {
		t.Errorf("Events = %v, want sorted by path", events)
	}
}

func TestScanDiff(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "a.txt")
	if err := os.WriteFile(file, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newTestScanner(t, root, nil)

	got, err := s.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != (Event{Kind: EventCreated, Path: "a.txt"}) {
		t.Fatalf("first scan = %v, want one created a.txt", got)
	}

	if err := os.WriteFile(file, []byte("much longer content now"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = s.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != (Event{Kind: EventModified, Path: "a.txt"}) {
		t.Fatalf("second scan = %v, want one modified a.txt", got)
	}

	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	got, err = s.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != (Event{Kind: EventDeleted, Path: "a.txt"}) {
		t.Fatalf("third scan = %v, want one deleted a.txt", got)
	}
}

func TestScanIgnores(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"keep.txt", ".sync-snapshot.json"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s := newTestScanner(t, root, func(rel string) bool { return rel == ".sync-snapshot.json" })

	got, err := s.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Path != "keep.txt" {
		t.Fatalf("events = %v, want only keep.txt", got)
	}
}
