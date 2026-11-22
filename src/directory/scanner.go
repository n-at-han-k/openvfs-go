package directory

import (
	"io/fs"
	"path/filepath"
	"sort"

	"example.com/project/src/journal"
)

type Diff struct {
	Added    []Event
	Modified []Event
	Removed  []Event
}

func (d Diff) Events() []Event {
	all := make([]Event, 0, len(d.Added)+len(d.Modified)+len(d.Removed))
	all = append(all, d.Added...)
	all = append(all, d.Modified...)
	all = append(all, d.Removed...)
	sort.Slice(all, func(i, j int) bool { return all[i].Path < all[j].Path })
	return all
}

func calculateDiff(prev map[string]journal.SyncJournalFileRecord, fresh map[string]LocalInfo) Diff {
	var diff Diff
	for path, info := range fresh {
		old, ok := prev[path]
		switch {
		case !ok:
			diff.Added = append(diff.Added, Event{Kind: Create, Path: path})
		case old.FileSize != info.Size() || old.ModTime != info.ModTime() ||
			old.Type != info.Type() || old.Inode != info.Inode():
			diff.Modified = append(diff.Modified, Event{Kind: Write, Path: path})
		}
	}
	for path := range prev {
		if _, ok := fresh[path]; !ok {
			diff.Removed = append(diff.Removed, Event{Kind: Remove, Path: path})
		}
	}
	return diff
}

type Scanner struct {
	root    string
	ignored func(string) bool
}

func NewScanner(root string, ignored func(string) bool) *Scanner {
	return &Scanner{root: root, ignored: ignored}
}

func (s *Scanner) Scan(onChange func(Event)) error {
	return filepath.WalkDir(s.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(s.root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if s.ignored != nil && s.ignored(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info := NewLocalInfo(p)
		if !info.IsValid() || info.Type() == journal.ItemTypeUnsupported {
			return nil
		}
		onChange(Event{Kind: LocalEntry, Path: rel, Local: info})
		return nil
	})
}
