package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"example.com/project/src/directory"
)

func expectEvent(t *testing.T, ch chan directory.Event, scanner *directory.Scanner, kind directory.EventKind, path string) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case ev := <-ch:
			if ev.Kind == directory.Poll {
				found, err := scanner.Scan()
				if err != nil {
					t.Fatal(err)
				}
				for _, change := range found {
					ch <- change
				}
				continue
			}
			if ev.Kind == kind && ev.Path == path {
				return
			}
		case <-deadline:
			t.Fatalf("no %v event for %q", kind, path)
		}
	}
}

func TestIntegrationWatchToChanges(t *testing.T) {
	root := t.TempDir()
	dbPath := ".sync-journal.db"
	ignored := func(rel string) bool { return rel == dbPath }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := make(chan directory.Event, 256)
	journal, err := directory.OpenJournal(filepath.Join(root, dbPath))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	scanner := directory.NewScanner(root, ignored, journal)

	go directory.RunWatcher(ctx, root, ignored, func(ev directory.Event) {
		select {
		case events <- ev:
		case <-ctx.Done():
		}
	})
	go directory.RunPoller(ctx, 100*time.Millisecond, func(ev directory.Event) {
		select {
		case events <- ev:
		case <-ctx.Done():
		}
	})
	time.Sleep(100 * time.Millisecond) // let the watches register

	file := filepath.Join(root, "a.txt")
	if err := os.WriteFile(file, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	expectEvent(t, events, scanner, directory.Create, "a.txt")

	if err := os.WriteFile(file, []byte("much longer content now"), 0o644); err != nil {
		t.Fatal(err)
	}
	expectEvent(t, events, scanner, directory.Write, "a.txt")

	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	expectEvent(t, events, scanner, directory.Remove, "a.txt")
}
