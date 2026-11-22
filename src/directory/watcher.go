package directory

import (
	"context"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/fsnotify/fsnotify"
)

type EventKind uint8

const (
	EventCreated EventKind = iota
	EventModified
	EventDeleted
	EventRenamed
	EventRescan // events were lost
	EventPoll   // periodic tick from the poller
)

type Event struct {
	Kind EventKind
	Path string
}

// Watcher watches root recursively and calls onChange for every event. It has
// no debounce and no buffered change set: the callback owner decides what to
// do with each notification.
type Watcher struct {
	root      string
	w         *fsnotify.Watcher
	ignoredFn func(string) bool
	onChange  func(Event)
	watched   map[string]struct{} // owned by the run goroutine
}

// RunWatcher sets up recursive watches on root and processes events until ctx
// is cancelled. The caller starts it as a goroutine.
func RunWatcher(ctx context.Context, root string, ignored func(string) bool, onChange func(Event)) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("fsnotify: %v", err)
		return
	}
	defer w.Close()

	r := &Watcher{
		root:      root,
		w:         w,
		ignoredFn: ignored,
		onChange:  onChange,
		watched:   make(map[string]struct{}),
	}
	if err := r.addTree(root); err != nil {
		log.Printf("watch %s: %v", root, err)
		return
	}
	r.run(ctx)
}

func (r *Watcher) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-r.w.Events:
			if !ok {
				return
			}
			r.onEvent(ev)
		case err, ok := <-r.w.Errors:
			if !ok {
				return
			}
			// Overflow or exhausted watches: events were lost.
			log.Printf("fsnotify: %v", err)
			r.onChange(Event{Kind: EventRescan})
		}
	}
}

func (r *Watcher) onEvent(ev fsnotify.Event) {
	rel, err := filepath.Rel(r.root, ev.Name)
	if err != nil {
		return
	}
	rel = filepath.ToSlash(rel)
	if r.ignored(rel) {
		return
	}
	kind := EventModified
	switch {
	case ev.Op.Has(fsnotify.Create):
		kind = EventCreated
	case ev.Op.Has(fsnotify.Remove):
		kind = EventDeleted
	case ev.Op.Has(fsnotify.Rename):
		kind = EventRenamed
	}
	r.onChange(Event{Kind: kind, Path: rel})

	// A new or moved-in directory needs its own watch (IN_CREATE | IN_MOVED_TO).
	if ev.Op&(fsnotify.Create|fsnotify.Write) != 0 {
		if st, err := os.Stat(ev.Name); err == nil && st.IsDir() {
			if err := r.addTree(ev.Name); err != nil {
				log.Printf("watch %s: %v", ev.Name, err)
				r.onChange(Event{Kind: EventRescan})
			}
		}
	}
	// Deleted or moved-away directory: drop its watch and all descendants.
	if ev.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
		r.removeTree(ev.Name)
	}
}

// addTree registers a watch for dir and every non-ignored directory below it.
func (r *Watcher) addTree(dir string) error {
	return filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(r.root, p)
		if err != nil {
			return err
		}
		if p != dir && r.ignored(filepath.ToSlash(rel)) {
			return filepath.SkipDir
		}
		return r.add(p)
	})
}

func (r *Watcher) add(dir string) error {
	if _, ok := r.watched[dir]; ok {
		return nil
	}
	if err := r.w.Add(dir); err != nil {
		return err
	}
	r.watched[dir] = struct{}{}
	return nil
}

func (r *Watcher) removeTree(root string) {
	for dir := range r.watched {
		if dir == root || strings.HasPrefix(dir, root+string(os.PathSeparator)) {
			_ = r.w.Remove(dir)
			delete(r.watched, dir)
		}
	}
}

func (r *Watcher) ignored(p string) bool {
	if strings.HasPrefix(filepath.Base(p), ".sync_") {
		return true // journal/temp files
	}
	if r.ignoredFn != nil {
		return r.ignoredFn(p)
	}
	return false
}
