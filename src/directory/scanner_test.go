package directory

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"example.com/project/src/journal"
)

func collect(t *testing.T, s *Scanner) []Event {
	t.Helper()
	var got []Event
	if err := s.Scan(func(ev Event) { got = append(got, ev) }); err != nil {
		t.Fatal(err)
	}
	sort.Slice(got, func(i, j int) bool { return got[i].Path < got[j].Path })
	return got
}

func TestScanEmitsLocalEntries(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"a.txt":     "hi",
		"sub/b.txt": "longer body",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got := collect(t, NewScanner(root, nil))
	if len(got) != 2 || got[0].Path != "a.txt" || got[1].Path != "sub/b.txt" {
		t.Fatalf("paths = %v, want a.txt and sub/b.txt", got)
	}
	for _, ev := range got {
		if ev.Kind != LocalEntry {
			t.Errorf("%s: Kind = %v, want LocalEntry", ev.Path, ev.Kind)
		}
		if !ev.Local.IsValid() || ev.Local.Type() != journal.ItemTypeFile {
			t.Errorf("%s: Local = %+v", ev.Path, ev.Local)
		}
		if ev.Local.Inode() == 0 || ev.Local.ModTime() == 0 {
			t.Errorf("%s: Local is missing stat data: %+v", ev.Path, ev.Local)
		}
	}
	if got[0].Local.Size() != 2 || got[1].Local.Size() != 11 {
		t.Errorf("sizes = %d, %d; want 2, 11", got[0].Local.Size(), got[1].Local.Size())
	}

	if err := os.Remove(filepath.Join(root, "a.txt")); err != nil {
		t.Fatal(err)
	}
	got = collect(t, NewScanner(root, nil))
	if len(got) != 1 || got[0].Path != "sub/b.txt" {
		t.Fatalf("after remove = %v, want only sub/b.txt", got)
	}
}

func TestScanIgnores(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"keep.txt", ".sync-snapshot.json"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s := NewScanner(root, func(rel string) bool { return rel == ".sync-snapshot.json" })

	got := collect(t, s)
	if len(got) != 1 || got[0].Path != "keep.txt" {
		t.Fatalf("events = %v, want only keep.txt", got)
	}
}

func TestScanSkipsIgnoredSubtrees(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "skipme", "deep"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "skipme", "deep", "x.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "keep.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewScanner(root, func(rel string) bool { return rel == "skipme" })

	got := collect(t, s)
	if len(got) != 1 || got[0].Path != "keep.txt" {
		t.Fatalf("events = %v, want only keep.txt", got)
	}
}

func TestDiff(t *testing.T) {
	prev := map[string]journal.SyncJournalFileRecord{
		"gone.txt":    {},
		"same.txt":    {FileSize: 1},
		"changed.txt": {FileSize: 1},
	}
	fresh := map[string]LocalInfo{
		"new.txt":     {size: 2},
		"same.txt":    {size: 1},
		"changed.txt": {size: 2},
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
