package directory

import (
	"io/fs"
	"path/filepath"
	"sort"
)

// Meta is the state stored for each path in the journal.
type Meta struct {
	Size    int64
	ModTime int64 // UnixNano
	Mode    fs.FileMode
}

// Diff is the result of comparing the journal against a fresh scan.
type Diff struct {
	Added    []Event
	Modified []Event
	Removed  []Event
}

// Events returns all changes in one sorted stream, ready for the bus.
func (d Diff) Events() []Event {
	all := make([]Event, 0, len(d.Added)+len(d.Modified)+len(d.Removed))
	all = append(all, d.Added...)
	all = append(all, d.Modified...)
	all = append(all, d.Removed...)
	sort.Slice(all, func(i, j int) bool { return all[i].Path < all[j].Path })
	return all
}

// Scanner walks the tree, builds a Diff against the journal and writes the
// new state back. It is a plain object: main.go decides when to call Scan().
type Scanner struct {
	root    string
	ignored func(string) bool
	journal *Journal
}

func NewScanner(root string, ignored func(string) bool, journal *Journal) *Scanner {
	return &Scanner{root: root, ignored: ignored, journal: journal}
}

// Scan reads the previous state from the journal, diffs it against the
// filesystem, updates the journal and returns the resulting events.
func (s *Scanner) Scan() ([]Event, error) {
	prev, err := s.journal.Records()
	if err != nil {
		return nil, err
	}
	fresh, err := s.walk()
	if err != nil {
		return nil, err
	}

	diff := calculateDiff(prev, fresh)
	for _, event := range diff.Added {
		if err := s.journal.SetRecord(event.Path, fresh[event.Path]); err != nil {
			return nil, err
		}
	}
	for _, event := range diff.Modified {
		if err := s.journal.SetRecord(event.Path, fresh[event.Path]); err != nil {
			return nil, err
		}
	}
	for _, event := range diff.Removed {
		if err := s.journal.DeleteRecord(event.Path); err != nil {
			return nil, err
		}
	}
	return diff.Events(), nil
}

func calculateDiff(prev, fresh map[string]Meta) Diff {
	var diff Diff
	for path, m := range fresh {
		old, ok := prev[path]
		switch {
		case !ok:
			diff.Added = append(diff.Added, Event{Kind: Create, Path: path})
		case old != m:
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

func (s *Scanner) walk() (map[string]Meta, error) {
	fresh := make(map[string]Meta)
	err := filepath.WalkDir(s.root, func(p string, d fs.DirEntry, err error) error {
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
		info, err := d.Info()
		if err != nil {
			return err
		}
		fresh[rel] = Meta{Size: info.Size(), ModTime: info.ModTime().UnixNano(), Mode: info.Mode()}
		return nil
	})
	return fresh, err
}
