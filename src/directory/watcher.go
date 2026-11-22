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
	Create EventKind = 1 << iota
	Write
	Remove
	Rename
	Chmod
	Poll
	LocalEntry
)

func (k EventKind) String() string {
	switch k {
	case Create:
		return "Create"
	case Write:
		return "Write"
	case Remove:
		return "Remove"
	case Rename:
		return "Rename"
	case Chmod:
		return "Chmod"
	case Poll:
		return "Poll"
	case LocalEntry:
		return "LocalEntry"
	}
	return "Unknown"
}

func fromFsnotify(op fsnotify.Op) EventKind {
	switch {
	case op.Has(fsnotify.Create):
		return Create
	case op.Has(fsnotify.Write):
		return Write
	case op.Has(fsnotify.Remove):
		return Remove
	case op.Has(fsnotify.Rename):
		return Rename
	case op.Has(fsnotify.Chmod):
		return Chmod
	}
	return 0
}

type Event struct {
	Kind EventKind
	Path string

	Local LocalInfo
}

type Watcher struct {
	root      string
	w         *fsnotify.Watcher
	ignoredFn func(string) bool
	onChange  func(Event)
	watched   map[string]struct{}
}

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
			log.Printf("fsnotify: %v", err)
			r.onChange(Event{Kind: Poll})
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

	for _, op := range []fsnotify.Op{fsnotify.Create, fsnotify.Write, fsnotify.Remove, fsnotify.Rename, fsnotify.Chmod} {
		if ev.Op.Has(op) {
			r.onChange(Event{Kind: fromFsnotify(op), Path: rel})
		}
	}

	if ev.Op&(fsnotify.Create|fsnotify.Write) != 0 {
		if st, err := os.Stat(ev.Name); err == nil && st.IsDir() {
			if err := r.addTree(ev.Name); err != nil {
				log.Printf("watch %s: %v", ev.Name, err)
				r.onChange(Event{Kind: Poll})
			}
		}
	}
	if ev.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
		r.removeTree(ev.Name)
	}
}

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
		return true
	}
	if r.ignoredFn != nil {
		return r.ignoredFn(p)
	}
	return false
}
