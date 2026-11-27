package directory

import (
	"context"
	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog/log"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type EventKind uint8

func (self EventKind) String() string {
	switch self {
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
	case Rescan:
		return "Rescan"
	}
	return "Unknown"
}

const (
	Create = 1
	Write  = 2
	Remove = 4
	Rename = 8
	Chmod  = 16
	// Rescan means events were dropped, so the root has to be walked again.
	Rescan = 32
)

func fromFsnotify(op fsnotify.Op) EventKind {
	if op.Has(fsnotify.Create) {
		return Create
	} else if op.Has(fsnotify.Write) {
		return Write
	} else if op.Has(fsnotify.Remove) {
		return Remove
	} else if op.Has(fsnotify.Rename) {
		return Rename
	} else if op.Has(fsnotify.Chmod) {
		return Chmod
	}
	return 0
}

// Event is one change under the root. Path is relative to the root, in slash

// form; it is empty on a Rescan.

type Event struct {
	Kind EventKind
	Path string
}

type Watcher struct {
	root      string
	w         *fsnotify.Watcher
	ignoredFn func(string) bool
	onChange  func(Event)
	watched   map[string]struct{}
}

func (self *Watcher) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-self.w.Events:
			if !ok {
				return
			}
			self.onEvent(ev)
		case err, ok := <-self.w.Errors:
			if !ok {
				return
			}
			log.Warn().Err(err).Msg("fsnotify")
			self.onChange(Event{Kind: Rescan})
		}
	}
}

func (self *Watcher) onEvent(ev fsnotify.Event) {
	rel, err := filepath.Rel(self.root, ev.Name)
	if err != nil {
		return
	}
	rel = filepath.ToSlash(rel)
	if self.ignored(rel) {
		return
	}
	for _, op := range []fsnotify.Op{fsnotify.Create, fsnotify.Write, fsnotify.Remove, fsnotify.Rename, fsnotify.Chmod} {
		if ev.Op.Has(op) {
			self.onChange(Event{Kind: fromFsnotify(op), Path: rel})
		}
	}
	// A new directory has to be watched too, and its contents may already
	// have appeared before the watch landed.
	if ev.Op&(fsnotify.Create|fsnotify.Write) != 0 {
		st, err := os.Stat(ev.Name)
		if err == nil && st.IsDir() {
			if err = self.addTree(ev.Name); err != nil {
				log.Warn().Err(err).Msgf("watch %s", ev.Name)
				self.onChange(Event{Kind: Rescan})
			}
		}
	}
	if ev.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
		self.removeTree(ev.Name)
	}
}

func (self *Watcher) addTree(dir string) error {
	return filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(self.root, p)
		if err != nil {
			return err
		}
		if p != dir && self.ignored(filepath.ToSlash(rel)) {
			return filepath.SkipDir
		}
		return self.add(p)
	})
}

func (self *Watcher) add(dir string) error {
	if _, ok := self.watched[dir]; ok {
		return nil
	}
	if err := self.w.Add(dir); err != nil {
		return err
	}
	self.watched[dir] = struct{}{}
	return nil
}

func (self *Watcher) removeTree(root string) {
	for dir := range self.watched {
		if dir == root || strings.HasPrefix(dir, root+string(os.PathSeparator)) {
			_ = self.w.Remove(dir)
			delete(self.watched, dir)
		}
	}
}

func (self *Watcher) ignored(p string) bool {
	if strings.HasPrefix(filepath.Base(p), ".sync_") {
		return true
	}
	if self.ignoredFn != nil {
		return self.ignoredFn(p)
	}
	return false
}

// RunWatcher watches root and every directory under it until ctx is

// cancelled, reporting each change through onChange.

func RunWatcher(ctx context.Context, root string, ignored func(string) bool, onChange func(Event)) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		log.Warn().Err(err).Msg("fsnotify")
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
	if err = r.addTree(root); err != nil {
		log.Warn().Err(err).Msgf("watch %s", root)
		return
	}
	r.run(ctx)
}
