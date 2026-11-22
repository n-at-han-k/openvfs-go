package directory

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

func newTestWatcher(t *testing.T, root string, ignored func(string) bool, onChange func(Event)) *Watcher {
	t.Helper()
	w, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return &Watcher{root: root, w: w, ignoredFn: ignored, onChange: onChange, watched: make(map[string]struct{})}
}

func expectHint(t *testing.T, events <-chan Event, want string) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case ev := <-events:
			if ev.Path == want {
				return
			}
		case <-deadline:
			t.Fatalf("no hint for %q", want)
		}
	}
}

func TestIgnored(t *testing.T) {
	r := &Watcher{ignoredFn: func(p string) bool { return p == "excluded/x.txt" }}
	for _, tt := range []struct {
		path string
		want bool
	}{
		{".sync_db-journal", true},
		{"notes.txt", false},
		{"excluded/x.txt", true},
	} {
		if got := r.ignored(tt.path); got != tt.want {
			t.Errorf("ignored(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestAddTreeSkipsIgnoredSubtrees(t *testing.T) {
	root := t.TempDir()
	r := newTestWatcher(t, root, func(rel string) bool { return rel == "excluded" }, nil)
	for _, dir := range []string{"keep", "excluded/child"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(dir)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.addTree(root); err != nil {
		t.Fatal(err)
	}

	for _, dir := range []string{root, filepath.Join(root, "keep")} {
		if _, ok := r.watched[dir]; !ok {
			t.Errorf("watched = %v, missing %q", r.watched, dir)
		}
	}
	for _, dir := range []string{"excluded", "excluded/child"} {
		if _, ok := r.watched[filepath.Join(root, filepath.FromSlash(dir))]; ok {
			t.Errorf("unexpected watch for %q", dir)
		}
	}
}

func TestRemoveTree(t *testing.T) {
	root := t.TempDir()
	r := newTestWatcher(t, root, nil, nil)
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(filepath.Join(sub, "child"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := r.addTree(root); err != nil {
		t.Fatal(err)
	}

	r.removeTree(sub)
	for _, dir := range []string{sub, filepath.Join(sub, "child")} {
		if _, ok := r.watched[dir]; ok {
			t.Errorf("watched = %v, %q should be gone", r.watched, dir)
		}
	}
	if _, ok := r.watched[root]; !ok {
		t.Error("root watch should remain")
	}
}

func TestWatcherPublishesHints(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	root := t.TempDir()
	events := make(chan Event, 64)
	go RunWatcher(ctx, root, nil, func(ev Event) { events <- ev })
	time.Sleep(100 * time.Millisecond)

	file := filepath.Join(root, "a.txt")
	if err := os.WriteFile(file, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	expectHint(t, events, "a.txt")

	sub := filepath.Join(root, "newdir")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	expectHint(t, events, "newdir")

	nested := filepath.Join(sub, "nested.txt")
	if err := os.WriteFile(nested, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	expectHint(t, events, "newdir/nested.txt")
}
