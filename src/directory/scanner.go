package directory

import (
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"example.com/project/src/journal"
)

type Scanner struct {
	root    string
	ignored func(string) bool
	journal *journal.Journal

	queryLocal  QueryMode
	queryServer QueryMode

	ignoreHiddenFiles bool

	processFile func(path PathTuple, localEntry LocalInfo, serverEntry RemoteFileData,
		dbEntry journal.SyncJournalFileRecord, queryLocal, queryServer QueryMode)

	remoteEntries func(dir string) ([]RemoteFileData, error)
}

func NewScanner(root string, ignored func(string) bool, db *journal.Journal) *Scanner {
	return &Scanner{root: root, ignored: ignored, journal: db}
}

type dirEntries struct {
	dbEntry     journal.SyncJournalFileRecord
	serverEntry RemoteFileData
	localEntry  LocalInfo
}

func (s *Scanner) Scan(onChange func(Event)) error {
	return s.process(PathTuple{}, onChange)
}

func (s *Scanner) process(current PathTuple, onChange func(Event)) error {
	queryServer := s.queryServer
	if s.remoteEntries == nil {
		queryServer = ParentNotChanged
	}

	entries := make(map[string]*dirEntries)
	at := func(name string) *dirEntries {
		if e, ok := entries[name]; ok {
			return e
		}
		e := &dirEntries{}
		entries[name] = e
		return e
	}

	if queryServer == NormalQuery {
		list, err := s.remoteEntries(current.Server)
		if err != nil {
			return err
		}
		for _, e := range list {
			at(e.Name()).serverEntry = e
		}
	}

	recs, err := s.journal.ListFilesInPath(current.Original)
	if err != nil {
		return err
	}
	for _, rec := range recs {
		at(rec.Name()).dbEntry = rec
	}

	if s.queryLocal == NormalQuery {
		dir := filepath.Join(s.root, filepath.FromSlash(current.Local))
		dirents, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, dirent := range dirents {
			info := NewLocalInfoFromDirEntry(dir, dirent)
			if info.Type() == journal.ItemTypeUnsupported || !info.IsValid() {
				continue
			}
			at(info.Name()).localEntry = info
		}
	}

	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)

	var subdirs []PathTuple
	for _, name := range names {
		e := entries[name]
		path := current.AddName(name)

		isHidden := e.localEntry.IsHidden() || strings.HasPrefix(name, ".")

		isDirectory := e.localEntry.IsDirectory() || e.serverEntry.IsDirectory()

		if s.handleExcluded(path.Target, isHidden) {
			if !e.localEntry.IsValid() && e.dbEntry.IsValid() {
				log.Printf("discovery: removing db entry for non existing ignored file: %s", path.Original)
				if err := s.journal.DeleteRecord(path.Original); err != nil {
					return err
				}
			}
			continue
		}

		// TODO: _queryServer == InBlackList and the selective sync blacklist
		// divert to processBlacklisted() upstream.

		if onChange != nil && e.localEntry.IsValid() && !e.localEntry.IsDirectory() {
			onChange(Event{Kind: LocalEntry, Path: path.Target, Local: e.localEntry})
		}

		if s.processFile != nil {
			s.processFile(path, e.localEntry, e.serverEntry, e.dbEntry, s.queryLocal, queryServer)
		}

		if isDirectory {
			subdirs = append(subdirs, path)
		}
	}

	for _, sub := range subdirs {
		if err := s.process(sub, onChange); err != nil {
			return err
		}
	}
	return nil
}

func (s *Scanner) handleExcluded(path string, isHidden bool) bool {
	if isHidden && s.ignoreHiddenFiles {
		return true
	}
	return s.ignored != nil && s.ignored(path)
}

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
