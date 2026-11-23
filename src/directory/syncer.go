package directory

import (
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha3"
	"encoding/hex"
	"hash"
	"hash/adler32"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"example.com/project/src/journal"
)

type SyncRequest struct {
	Cause Event
}

type Syncer struct {
	scanner  *Scanner
	journal  *journal.Journal
	localDir string
	requests chan SyncRequest
	onChange func(Event)

	// ProcessDirectoryJob::_queryServer / _queryLocal / _pinState.
	queryServer QueryMode
	queryLocal  QueryMode
	pinState    PinState

	// ProcessDirectoryJob::_childModified / _childIgnored.
	childModified bool
	childIgnored  bool

	// DiscoveryPhase::_renamedItemsLocal / _renamedItemsRemote / _deletedItem /
	// _forbiddenDeletes / _anotherSyncNeeded.
	renamedItemsLocal  map[string]string
	renamedItemsRemote map[string]string
	deletedItem        map[string]*SyncFileItem
	forbiddenDeletes   map[string]struct{}
	anotherSyncNeeded  bool

	// vfs hooks. Nil means Vfs::Mode::Off, which is what the plain
	// (non-placeholder) backend behaves like.
	needsMetadataUpdate     func(item *SyncFileItem) bool
	isDehydratedPlaceholder func(absPath string) bool
	vfsEnabled              bool

	// DiscoveryPhase::itemDiscovered. Nil drops the item.
	onItemDiscovered func(item *SyncFileItem)
}

func NewSyncer(root string, ignored func(string) bool, db *journal.Journal, onChange func(Event)) *Syncer {
	s := &Syncer{
		scanner:  NewScanner(root, ignored, db),
		journal:  db,
		localDir: root,
		// Buffered by one: see Request. A run in flight plus one pending is
		// all the depth a coalescing queue needs.
		requests:           make(chan SyncRequest, 1),
		onChange:           onChange,
		renamedItemsLocal:  make(map[string]string),
		renamedItemsRemote: make(map[string]string),
		deletedItem:        make(map[string]*SyncFileItem),
		forbiddenDeletes:   make(map[string]struct{}),
	}
	// The scan's job is the per-directory join; deciding what each merged name
	// means is the reconcile's.
	s.scanner.processFile = s.processFile
	return s
}

func (s *Syncer) Request(req SyncRequest) bool {
	select {
	case s.requests <- req:
		return true
	default:
		return false
	}
}

func (s *Syncer) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case req := <-s.requests:
			s.sync(ctx, req)
		}
	}
}

func (s *Syncer) sync(ctx context.Context, req SyncRequest) {
	// Phase 1: local discovery. Every entry goes onto the bus as it is found.
	err := s.scanner.Scan(func(entry Event) {
		select {
		case <-ctx.Done():
		default:
			s.onChange(entry)
		}
	})
	if err != nil {
		log.Printf("sync (%v): local discovery failed: %v", req.Cause.Kind, err)
		return
	}

	// TODO Phase 2: remote discovery — the Depth 1 PROPFIND per directory,
	// emitting RemoteEntry events.
	// TODO Phase 3: reconcile — call processFileAnalyzeRemoteInfo per path with
	// the journal record, the LocalInfo and the RemoteFileData.
}

// ===========================================================================
// SyncInstruction — csync.h:53-70. What to do with an item, as opposed to
// EventKind, which is what happened to it.
// ===========================================================================

type SyncInstruction uint16

const (
	InstructionNone           SyncInstruction = 1 << 1 // Nothing to do
	InstructionRemove         SyncInstruction = 1 << 2 // The file need to be removed
	InstructionRename         SyncInstruction = 1 << 3 // The file need to be renamed
	InstructionNew            SyncInstruction = 1 << 4 // The file is new compared to the db
	InstructionConflict       SyncInstruction = 1 << 5 // Download because it is a conflict
	InstructionIgnore         SyncInstruction = 1 << 6 // Like None but logged to the user
	InstructionSync           SyncInstruction = 1 << 7 // Push to the other remote
	InstructionError          SyncInstruction = 1 << 8
	InstructionTypeChange     SyncInstruction = 1 << 9  // Like New, but deletes the other entity first
	InstructionUpdateMetadata SyncInstruction = 1 << 10 // Write the etag, no propagation
)

func (i SyncInstruction) String() string {
	switch i {
	case InstructionNone:
		return "None"
	case InstructionRemove:
		return "Remove"
	case InstructionRename:
		return "Rename"
	case InstructionNew:
		return "New"
	case InstructionConflict:
		return "Conflict"
	case InstructionIgnore:
		return "Ignore"
	case InstructionSync:
		return "Sync"
	case InstructionError:
		return "Error"
	case InstructionTypeChange:
		return "TypeChange"
	case InstructionUpdateMetadata:
		return "UpdateMetadata"
	}
	return "Unknown"
}

// Direction is SyncFileItem::Direction (syncfileitem.h:46-50). Paired with the
// instruction it is what OwncloudPropagator::createJob switches on: Sync+Up is
// an upload, Sync+Down a download.
type Direction uint8

const (
	DirectionNone Direction = 0
	DirectionUp   Direction = 1
	DirectionDown Direction = 2
)

func (d Direction) String() string {
	switch d {
	case DirectionUp:
		return "Up"
	case DirectionDown:
		return "Down"
	}
	return "None"
}

// QueryMode is ProcessDirectoryJob::QueryMode (discovery.h:54-60).
type QueryMode uint8

const (
	NormalQuery      QueryMode = iota
	ParentDontExist            // Do not query this folder because it does not exist
	ParentNotChanged           // No need to query: unchanged from what is in the DB
	InBlackList                // Do not query: it is in the blacklist (remote only)
)

// PinState is the subset of OCC::PinState the reconcile needs.
type PinState uint8

const (
	PinStateUnspecified PinState = iota
	PinStateAlwaysLocal
	PinStateOnlineOnly
)

// ===========================================================================
// PathTuple — discovery.h:98-127. A same path may have different values
// locally or on the server when renames are involved. None of these start or
// end with a slash; they are all relative to the sync root.
// ===========================================================================

type PathTuple struct {
	Original string // Path as in the DB (before the sync)
	Target   string // Path that will be the result after the sync
	Server   string // Path on the server (before the sync)
	Local    string // Path locally (before the sync)
}

func pathAppend(base, name string) string {
	if base == "" {
		return name
	}
	return base + "/" + name
}

// AddName is PathTuple::addName().
func (p PathTuple) AddName(name string) PathTuple {
	out := PathTuple{Original: pathAppend(p.Original, name)}
	build := func(other string) string {
		if other == p.Original {
			return out.Original
		}
		return pathAppend(other, name)
	}
	out.Target = build(p.Target)
	out.Server = build(p.Server)
	out.Local = build(p.Local)
	return out
}

// ===========================================================================
// SyncFileItem — syncfileitem.h. The in-flight item: merged local, remote and
// journal state plus the decision. It carries server-derived fields from
// discovery, through a transfer that overwrites them from the response, to the
// one journal write at the end.
// ===========================================================================

type SyncFileItem struct {
	// localName is the syncfolder-relative path the operation is about. For a
	// rename it is the source and RenameTarget is the target.
	localName string

	// RenameTarget is the name localName should become. Use Destination().
	RenameTarget string

	// OriginalFile is the db path of this item, which differs from localName
	// and RenameTarget when parts of the path were renamed.
	OriginalFile string

	Type                  journal.ItemType
	Direction             Direction
	ServerHasIgnoredFiles bool
	HasBlacklistEntry     bool

	Status          ItemStatus
	IsRestoration   bool
	IsSelectiveSync bool
	HTTPErrorCode   uint16
	RemotePerm      journal.RemotePermissions
	ErrorString     string

	// Used by the propagator.
	ModTime int64
	Etag    string
	Size    int64
	Inode   uint64
	FileID  string

	// ChecksumHeader is the value for the 'new' side, matching Size and
	// ModTime. Local checksum for .eml mtime/size changes and for potential
	// local renames; remote checksum for conflicts.
	ChecksumHeader string

	// The size and modtime of the file getting overwritten: on disk for
	// downloads, on the server for uploads.
	PreviousSize    int64
	PreviousModtime int64

	instruction SyncInstruction
}

// ItemStatus is SyncFileItem::Status (syncfileitem.h:53-).
type ItemStatus uint8

const (
	StatusNoStatus ItemStatus = iota
	StatusFatalError
	StatusNormalError
	StatusSoftError
	StatusSuccess
	StatusConflict
	StatusFileIgnored
	StatusRestoration
	StatusBlacklistedError
	StatusExcluded
)

func NewSyncFileItem(localName string) *SyncFileItem {
	return &SyncFileItem{
		localName:   localName,
		Type:        journal.ItemTypeUnsupported,
		Direction:   DirectionNone,
		instruction: InstructionNone,
	}
}

// SyncFileItemFromRecord is SyncFileItem::fromSyncJournalFileRecord().
func SyncFileItemFromRecord(rec journal.SyncJournalFileRecord) *SyncFileItem {
	item := NewSyncFileItem(rec.Path)
	if !rec.IsValid() {
		// A null record yields a null item. Go's zero ItemType is File, while
		// the C++ default-constructed record's is Unsupported, so this cannot
		// fall through to the copies below.
		return item
	}
	item.Inode = rec.Inode
	item.ModTime = rec.ModTime
	item.Type = rec.Type
	item.Etag = rec.Etag
	item.FileID = rec.FileID
	item.Size = rec.FileSize
	item.RemotePerm = rec.RemotePerm
	item.ServerHasIgnoredFiles = rec.ServerHasIgnoredFiles
	item.ChecksumHeader = rec.ChecksumHeader
	return item
}

func (i *SyncFileItem) LocalName() string            { return i.localName }
func (i *SyncFileItem) SetLocalName(name string)     { i.localName = name }
func (i *SyncFileItem) Instruction() SyncInstruction { return i.instruction }

// SetInstruction is SyncFileItem::setInstruction(): only one instruction is
// allowed, so the value must be a single bit.
func (i *SyncFileItem) SetInstruction(instruction SyncInstruction) {
	i.instruction = instruction
}

// Destination is SyncFileItem::destination().
func (i *SyncFileItem) Destination() string {
	if i.RenameTarget != "" {
		return i.RenameTarget
	}
	return i.localName
}

func (i *SyncFileItem) IsDirectory() bool { return i.Type == journal.ItemTypeDirectory }

func (i *SyncFileItem) IsVirtualFile() bool {
	return i.Type == journal.ItemTypeVirtualFile || i.Type == journal.ItemTypeVirtualFileDownload
}

// ===========================================================================
// DiscoveryPhase rename bookkeeping — discoveryphase.cpp:76-142.
// ===========================================================================

// isRenamed is DiscoveryPhase::isRenamed() (discoveryphase.h:160).
func (s *Syncer) isRenamed(p string) bool {
	if _, ok := s.renamedItemsLocal[p]; ok {
		return true
	}
	_, ok := s.renamedItemsRemote[p]
	return ok
}

// adjustRenamedPath is OCC::adjustRenamedPath(): rewrite a path through the
// longest already-renamed parent.
func (s *Syncer) adjustRenamedPath(original string, d Direction) string {
	renamed := s.renamedItemsLocal
	if d == DirectionDown {
		renamed = s.renamedItemsRemote
	}
	slashPos := len(original)
	for {
		slashPos = strings.LastIndex(original[:max(slashPos-1, 0)], "/")
		if slashPos <= 0 {
			break
		}
		if to, ok := renamed[original[:slashPos]]; ok {
			return to + original[slashPos:]
		}
	}
	return original
}

// findAndCancelDeletedJob is DiscoveryPhase::findAndCancelDeletedJob(): if the
// same path was already discovered as a delete, cancel that delete and report
// its etag, which turns a DELETE+NEW pair into a rename.
func (s *Syncer) findAndCancelDeletedJob(originalPath string) (bool, string) {
	item, ok := s.deletedItem[originalPath]
	if !ok {
		return false, ""
	}
	var oldEtag string
	if item.Instruction() == InstructionIgnore && item.Type == journal.ItemTypeVirtualFile {
		// Re-creation of virtual files counts as a delete: restoration after a
		// prohibited move, or an item parked in an error state. Report true so
		// no further action is taken that could lose data.
		oldEtag = item.Etag
	} else {
		item.SetInstruction(InstructionNone)
		oldEtag = item.Etag
	}
	delete(s.deletedItem, originalPath)
	// TODO: _queuedDeletedDirectories — needs the per-directory job.
	return true, oldEtag
}

// ===========================================================================
// processFile — discovery.cpp:227-287. Reconcile one name's three inputs. It
// delegates to the two analyze functions; the scan decides which names get
// here.
// ===========================================================================

func (s *Syncer) processFile(path PathTuple, localEntry LocalInfo,
	serverEntry RemoteFileData, dbEntry journal.SyncJournalFileRecord,
	queryLocal, queryServer QueryMode) {

	// The modes are the scan's, decided per directory. They live here because
	// the analyze functions read them off the job upstream.
	s.queryLocal, s.queryServer = queryLocal, queryServer

	if s.isRenamed(path.Original) {
		// Already claimed as a rename by another name in this run.
		return
	}

	item := SyncFileItemFromRecord(dbEntry)
	item.SetLocalName(path.Target)
	item.OriginalFile = path.Original
	item.PreviousSize = dbEntry.FileSize
	item.PreviousModtime = dbEntry.ModTime

	if item.Type == journal.ItemTypeVirtualFileDownload {
		// The item shall only have this type if the db request for the virtual
		// download was successful; that decision is made further down or in
		// processFileAnalyzeRemoteInfo.
		item.Type = journal.ItemTypeVirtualFile
	} else if item.Type == journal.ItemTypeVirtualFileDehydration {
		// Similarly, db entries with a dehydration request denote a regular
		// file until the request is processed.
		item.Type = journal.ItemTypeFile
	}

	if serverEntry.IsValid() {
		s.processFileAnalyzeRemoteInfo(item, path, localEntry, serverEntry, dbEntry)
		return
	}

	// Downloading a virtual file is like a server action and can happen even
	// if nothing changed server-side. Normally setting the VirtualFileDownload
	// flag means local and remote will be rediscovered; this is a fallback for
	// a similar check in processFileAnalyzeRemoteInfo.
	if s.queryServer == ParentNotChanged && dbEntry.IsValid() &&
		(dbEntry.Type == journal.ItemTypeVirtualFileDownload ||
			localEntry.Type() == journal.ItemTypeVirtualFileDownload) &&
		(localEntry.IsValid() || s.queryLocal == ParentNotChanged) {
		item.Direction = DirectionDown
		item.SetInstruction(InstructionSync)
		item.Type = journal.ItemTypeVirtualFileDownload
	}

	s.processFileAnalyzeLocalInfo(item, path, localEntry, serverEntry, dbEntry, s.queryServer)
}

// ===========================================================================
// processFileAnalyzeRemoteInfo — discovery.cpp:304-539. Runs when the server
// reported an entry; typically flows into processFileAnalyzeLocalInfo.
// ===========================================================================

func (s *Syncer) processFileAnalyzeRemoteInfo(item *SyncFileItem, path PathTuple,
	localEntry LocalInfo, serverEntry RemoteFileData, dbEntry journal.SyncJournalFileRecord,
) {
	item.ChecksumHeader = serverEntry.ChecksumHeader()
	item.FileID = serverEntry.FileID()
	item.RemotePerm = serverEntry.RemotePerm()
	if serverEntry.IsDirectory() {
		item.Type = journal.ItemTypeDirectory
	} else {
		item.Type = journal.ItemTypeFile
	}
	item.Etag = serverEntry.Etag()

	// Check for missing server data.
	if serverEntry.Error() != "" {
		item.SetInstruction(InstructionError)
		s.childIgnored = true
		item.ErrorString = serverEntry.Error()
		log.Printf("discovery: %s", item.ErrorString)
		s.emitItemDiscovered(item)
		return
	}

	// The file is known in the db already.
	if dbEntry.IsValid() {
		switch {
		case serverEntry.IsDirectory() != dbEntry.IsDirectory():
			// If the type of the entity changed, it's like NEW, but needs to
			// delete the other entity first.
			item.SetInstruction(InstructionTypeChange)
			item.Direction = DirectionDown
			item.ModTime = serverEntry.ModTime()
			item.Size = serverEntry.Size()
			if dbEntry.IsDirectory() {
				if s.vfsEnabled && s.pinState != PinStateAlwaysLocal {
					item.Type = journal.ItemTypeVirtualFile
				}
			}

		case (dbEntry.Type == journal.ItemTypeVirtualFileDownload ||
			localEntry.Type() == journal.ItemTypeVirtualFileDownload) &&
			(localEntry.IsValid() || s.queryLocal == ParentNotChanged):
			// The check for the localEntry existing is important: otherwise it
			// breaks the case where a file is moved and simultaneously tagged
			// for download in the db.
			item.Direction = DirectionDown
			item.SetInstruction(InstructionSync)
			item.Type = journal.ItemTypeVirtualFileDownload
			item.Size = serverEntry.Size()

		case dbEntry.Etag != serverEntry.Etag():
			item.Direction = DirectionDown
			item.ModTime = serverEntry.ModTime()
			item.Size = serverEntry.Size()
			switch {
			case serverEntry.IsDirectory():
				if !dbEntry.IsDirectory() {
					log.Printf("discovery: %s: server says directory, db does not", path.Original)
				}
				item.SetInstruction(InstructionUpdateMetadata)
			case !localEntry.IsValid() && s.queryLocal != ParentNotChanged:
				// Deleted locally, changed on server.
				item.SetInstruction(InstructionNew)
			default:
				item.SetInstruction(InstructionSync)
			}

		case dbEntry.RemotePerm != serverEntry.RemotePerm() || dbEntry.FileID != serverEntry.FileID():
			item.SetInstruction(InstructionUpdateMetadata)
			item.Direction = DirectionDown

		default:
			s.processFileAnalyzeLocalInfo(item, path, localEntry, serverEntry, dbEntry, ParentNotChanged)
			return
		}

		s.processFileAnalyzeLocalInfo(item, path, localEntry, serverEntry, dbEntry, s.queryServer)
		return
	}

	// Unknown in db: new file on the server.
	item.SetInstruction(InstructionNew)
	item.Direction = DirectionDown
	item.ModTime = serverEntry.ModTime()
	item.Size = serverEntry.Size()

	postProcessServerNew := func(path PathTuple) {
		// Turn new remote files into virtual files if the option is enabled.
		if !localEntry.IsValid() && item.Type == journal.ItemTypeFile &&
			s.vfsEnabled && s.pinState != PinStateAlwaysLocal {
			item.Type = journal.ItemTypeVirtualFile
		}
		s.processFileAnalyzeLocalInfo(item, path, localEntry, serverEntry, dbEntry, s.queryServer)
	}

	// Potential NEW/NEW conflict is handled in AnalyzeLocal.
	if localEntry.IsValid() {
		postProcessServerNew(path)
		return
	}

	// Not in db or locally: either new or a rename. Look for a record with the
	// same file id.
	candidates, err := s.journal.FileRecordsByFileID(serverEntry.FileID())
	if err != nil {
		s.dbError(err)
		return
	}

	done := false
	for _, base := range candidates {
		if done {
			break
		}
		if !base.IsValid() {
			continue
		}

		// Remote rename of a virtual file we have locally scheduled for
		// download: consider this NEW but mark it for download.
		if base.Type == journal.ItemTypeVirtualFileDownload {
			item.Type = journal.ItemTypeVirtualFileDownload
			done = true
			continue
		}
		// Remote rename targets a file that shall be locally dehydrated: don't
		// worry about the rename, consider it DELETE + NEW(virtual).
		if base.Type == journal.ItemTypeVirtualFileDehydration {
			done = true
			continue
		}

		// Some things prohibit rename detection entirely. Since the same
		// checks are not repeated in reconcile, we can't just skip the
		// candidate but have to give up completely.
		if base.IsDirectory() != item.IsDirectory() {
			log.Printf("discovery: file types different, not a rename")
			done = true
			continue
		}
		if !serverEntry.IsDirectory() && base.Etag != serverEntry.Etag() {
			// File with different etag: don't rename, download it again.
			log.Printf("discovery: file etag different, not a rename")
			done = true
			continue
		}

		// Now we know there is a sane rename candidate.
		originalPath := base.Path
		if s.isRenamed(originalPath) {
			log.Printf("discovery: folder already has a rename entry, skipping")
			continue
		}

		originalPathAdjusted := s.adjustRenamedPath(originalPath, DirectionUp)
		abs := filepath.Join(s.localDir, filepath.FromSlash(originalPathAdjusted))

		if !base.IsDirectory() {
			info := NewLocalInfo(abs)
			if !info.IsValid() {
				log.Printf("discovery: local file does not exist anymore: %s", originalPathAdjusted)
				continue
			}
			// NOTE: this prohibits some VFS renames from being detected, since
			// a suffix file's size differs from the db size. That's ok, they
			// will DELETE+NEW.
			if info.ModTime() != base.ModTime || info.Size() != base.FileSize || info.IsDirectory() {
				log.Printf("discovery: the file has changed locally, not a rename: %s", originalPath)
				continue
			}
		} else {
			if info := NewLocalInfo(abs); !info.IsValid() || !info.IsDirectory() {
				log.Printf("discovery: local directory does not exist anymore: %s", originalPathAdjusted)
				continue
			}
		}

		// Renames of virtuals are possible.
		if base.IsVirtualFile() {
			item.Type = journal.ItemTypeVirtualFile
		}

		wasDeletedOnServer, _ := s.findAndCancelDeletedJob(originalPath)

		postProcessRename := func(path *PathTuple) {
			adjustedOriginalPath := s.adjustRenamedPath(originalPath, DirectionUp)
			s.renamedItemsRemote[originalPath] = path.Target
			item.ModTime = base.ModTime
			item.Inode = base.Inode
			item.SetInstruction(InstructionRename)
			item.Direction = DirectionDown
			item.RenameTarget = path.Target
			item.SetLocalName(adjustedOriginalPath)
			item.OriginalFile = originalPath
			path.Original = originalPath
			path.Local = adjustedOriginalPath
			log.Printf("discovery: rename detected (down) %s -> %s", item.LocalName(), item.RenameTarget)
		}

		if wasDeletedOnServer {
			postProcessRename(&path)
			done = true
		} else {
			// Upstream issues a RequestEtagJob here to learn whether the
			// original is really gone on the server: 207 means it still
			// exists, so this is a NEW; 404 means it is a rename. Without an
			// HTTP client there is no way to ask, so fall back to NEW, which
			// is the safe answer (it costs a re-download, never data).
			// TODO: port the RequestEtagJob branch once the remote query lands.
			log.Printf("discovery: cannot confirm remote rename of %s without a server query, treating as new", originalPath)
			done = true
		}
	}

	if item.Instruction() == InstructionNew {
		postProcessServerNew(path)
		return
	}
	s.processFileAnalyzeLocalInfo(item, path, localEntry, serverEntry, dbEntry, s.queryServer)
}

// ===========================================================================
// processFileAnalyzeLocalInfo — discovery.cpp:540-873. Reconciles the local
// side, having already folded in whatever the server said.
// ===========================================================================

func (s *Syncer) processFileAnalyzeLocalInfo(item *SyncFileItem, path PathTuple,
	localEntry LocalInfo, serverEntry RemoteFileData, dbEntry journal.SyncJournalFileRecord,
	recurseQueryServer QueryMode,
) {
	noServerEntry := (s.queryServer != ParentNotChanged && !serverEntry.IsValid()) ||
		(s.queryServer == ParentNotChanged && !dbEntry.IsValid())

	if noServerEntry {
		recurseQueryServer = ParentDontExist
	}

	serverModified := func() bool {
		modifiedOnServer := item.Instruction()&
			(InstructionNew|InstructionSync|InstructionRename|InstructionTypeChange) != 0

		// Decay server modifications to UPDATE_METADATA if the local virtual exists.
		hasLocalVirtual := localEntry.IsVirtualFile() ||
			(s.queryLocal == ParentNotChanged && dbEntry.IsVirtualFile())
		virtualFileDownload := item.Type == journal.ItemTypeVirtualFileDownload ||
			item.Instruction() == InstructionTypeChange

		if modifiedOnServer && !virtualFileDownload && hasLocalVirtual {
			item.SetInstruction(InstructionUpdateMetadata)
			modifiedOnServer = false
			item.Type = journal.ItemTypeVirtualFile
		}
		if dbEntry.IsVirtualFile() && !virtualFileDownload {
			item.Type = journal.ItemTypeVirtualFile
		}
		return modifiedOnServer
	}()

	s.childModified = s.childModified || serverModified

	finalize := func(path PathTuple, recurseQueryServer QueryMode) {
		recurse := item.IsDirectory() || localEntry.IsDirectory() || serverEntry.IsDirectory()
		// Even with a local directory: if the remote is a file propagated as a
		// conflict there is no need to recurse into it.
		if item.Instruction() == InstructionConflict && !item.IsDirectory() {
			recurse = false
		}
		if s.queryLocal != NormalQuery && s.queryServer != NormalQuery {
			recurse = false
		}

		recurseQueryLocal := ParentDontExist
		switch {
		case s.queryLocal == ParentNotChanged:
			recurseQueryLocal = ParentNotChanged
		case localEntry.IsDirectory() || item.Instruction() == InstructionRename:
			recurseQueryLocal = NormalQuery
		}
		s.processFileFinalize(item, path, recurse, recurseQueryLocal, recurseQueryServer)
	}

	if !localEntry.IsValid() {
		switch {
		case s.queryLocal == ParentNotChanged && dbEntry.IsValid():
			// Not modified locally (ParentNotChanged).
			if noServerEntry {
				// Not on the server: removed on the server, delete locally.
				item.SetInstruction(InstructionRemove)
				item.Direction = DirectionDown
			} else if dbEntry.Type == journal.ItemTypeVirtualFileDehydration {
				// Dehydration requested.
				item.Direction = DirectionDown
				item.SetInstruction(InstructionSync)
				item.Type = journal.ItemTypeVirtualFileDehydration
			}
		case noServerEntry:
			// Not locally, not on the server. The entry is stale!
			log.Printf("discovery: stale DB entry: %s", path.Original)
			if err := s.journal.DeleteRecord(path.Original); err != nil {
				s.dbError(err)
			}
			// TODO: upstream deletes recursively here.
			return
		case !serverModified:
			// Removed locally: also remove on the server.
			if !dbEntry.ServerHasIgnoredFiles {
				item.SetInstruction(InstructionRemove)
				item.Direction = DirectionUp
			}
		}

		finalize(path, recurseQueryServer)
		return
	}

	// localEntry is valid from here on.
	item.Inode = localEntry.Inode()

	if dbEntry.IsValid() {
		typeChange := localEntry.IsDirectory() != dbEntry.IsDirectory()
		switch {
		case !typeChange && localEntry.IsVirtualFile():
			if noServerEntry {
				item.SetInstruction(InstructionRemove)
				item.Direction = DirectionDown
			}

		case !typeChange && ((dbEntry.ModTime == localEntry.ModTime() &&
			dbEntry.FileSize == localEntry.Size()) || localEntry.IsDirectory()):
			// Local file unchanged.
			switch {
			case noServerEntry:
				item.SetInstruction(InstructionRemove)
				item.Direction = DirectionDown
			case dbEntry.Type == journal.ItemTypeVirtualFileDehydration ||
				localEntry.Type() == journal.ItemTypeVirtualFileDehydration:
				item.Direction = DirectionDown
				item.SetInstruction(InstructionSync)
				item.Type = journal.ItemTypeVirtualFileDehydration
			case !serverModified && (dbEntry.Inode != localEntry.Inode() || s.vfsNeedsMetadataUpdate(item)):
				item.SetInstruction(InstructionUpdateMetadata)
				item.Direction = DirectionDown
			}

		case serverModified:
			// There's a local change and a server change: conflict! For
			// in-place placeholders that is not necessary: they could be
			// replaced by real files and should then trigger a regular SYNC
			// upwards when there's no server change.
			s.processFileConflict(item, path, localEntry, serverEntry, dbEntry)

		case typeChange:
			item.SetInstruction(InstructionTypeChange)
			item.Direction = DirectionUp
			item.ChecksumHeader = ""
			item.Size = localEntry.Size()
			item.ModTime = localEntry.ModTime()
			if localEntry.IsDirectory() {
				item.Type = journal.ItemTypeDirectory
			} else {
				item.Type = journal.ItemTypeFile
			}
			s.childModified = true

		default:
			// Local file was changed.
			item.SetInstruction(InstructionSync)
			if noServerEntry {
				// Special case: deleted on server, modified on client, so NEW.
				item.SetInstruction(InstructionNew)
			}
			item.Direction = DirectionUp
			item.ChecksumHeader = ""
			item.Size = localEntry.Size()
			item.ModTime = localEntry.ModTime()
			s.childModified = true

			// Checksum comparison at this stage is only enabled for .eml
			// files, see #4754 #4755.
			isEmlFile := strings.HasSuffix(strings.ToLower(path.Original), ".eml")
			if isEmlFile && dbEntry.FileSize == localEntry.Size() && dbEntry.ChecksumHeader != "" {
				abs := filepath.Join(s.localDir, filepath.FromSlash(path.Local))
				if computeLocalChecksum(dbEntry.ChecksumHeader, abs, item) &&
					item.ChecksumHeader == dbEntry.ChecksumHeader {
					log.Printf("discovery: NOTE: checksums are identical, file did not actually change: %s", path.Local)
					item.SetInstruction(InstructionUpdateMetadata)
				}
			}
		}

		finalize(path, recurseQueryServer)
		return
	}

	// dbEntry is invalid from here on.
	if localEntry.IsVirtualFile() && !noServerEntry {
		// Somehow there is a missing DB entry while the virtual file already
		// exists. The instruction should already be set correctly.
		if item.Instruction() != InstructionUpdateMetadata {
			log.Printf("discovery: %s: virtual file without db entry has instruction %v", path.Original, item.Instruction())
		}
		finalize(path, recurseQueryServer)
		return
	} else if serverModified {
		s.processFileConflict(item, path, localEntry, serverEntry, dbEntry)
		finalize(path, recurseQueryServer)
		return
	}

	// New local file or rename.
	item.SetInstruction(InstructionNew)
	item.Direction = DirectionUp
	item.ChecksumHeader = ""
	item.Size = localEntry.Size()
	item.ModTime = localEntry.ModTime()
	switch {
	case localEntry.IsDirectory():
		item.Type = journal.ItemTypeDirectory
	case localEntry.IsVirtualFile():
		item.Type = journal.ItemTypeVirtualFile
	default:
		item.Type = journal.ItemTypeFile
	}
	s.childModified = true

	postProcessLocalNew := func(path PathTuple) {
		if !localEntry.IsVirtualFile() {
			return
		}
		abs := filepath.Join(s.localDir, filepath.FromSlash(path.Local))
		if s.vfsIsDehydratedPlaceholder(abs) {
			log.Printf("discovery: wiping virtual file without db entry for %s", path.Local)
			item.SetInstruction(InstructionRemove)
			item.Direction = DirectionDown
		} else {
			log.Printf("discovery: virtual file without db entry for %s but looks odd, keeping", path.Local)
			item.SetInstruction(InstructionIgnore)
		}
	}

	// Check if it is a move.
	base, err := s.journal.FileRecordByInode(localEntry.Inode())
	if err != nil {
		s.dbError(err)
		return
	}
	originalPath := base.Path

	// Gradually check the conditions for accepting a move candidate.
	moveCheck := func() bool {
		if !base.IsValid() {
			log.Printf("discovery: not a move, no item in db with inode %d", localEntry.Inode())
			return false
		}
		if base.IsDirectory() != item.IsDirectory() {
			log.Printf("discovery: not a move, types don't match: %v %v %v", base.Type, item.Type, localEntry.Type())
			return false
		}
		// Directories and virtual files don't need size/mtime equality.
		if !localEntry.IsDirectory() && !base.IsVirtualFile() &&
			(base.ModTime != localEntry.ModTime() || base.FileSize != localEntry.Size()) {
			log.Printf("discovery: not a move, mtime or size differs, modtime: %d %d, size: %d %d",
				base.ModTime, localEntry.ModTime(), base.FileSize, localEntry.Size())
			return false
		}

		// The old file must have been deleted.
		oldAbs := filepath.Join(s.localDir, filepath.FromSlash(originalPath))
		if _, statErr := os.Lstat(oldAbs); statErr == nil &&
			// Exception: if the rename changes case only (like "foo" -> "Foo")
			// the old filename might still point to the same file.
			!(fsCasePreserving() && strings.EqualFold(originalPath, path.Local) && originalPath != path.Local) {
			log.Printf("discovery: not a move, base file still exists at %s", originalPath)
			return false
		}

		// Verify the checksum where possible.
		if base.ChecksumHeader != "" && item.Type == journal.ItemTypeFile && base.Type == journal.ItemTypeFile {
			abs := filepath.Join(s.localDir, filepath.FromSlash(path.Original))
			if computeLocalChecksum(base.ChecksumHeader, abs, item) {
				log.Printf("discovery: checking checksum of potential rename %s %s %s",
					path.Original, item.ChecksumHeader, base.ChecksumHeader)
				if item.ChecksumHeader != base.ChecksumHeader {
					log.Printf("discovery: not a move, checksums differ")
					return false
				}
			}
		}

		if s.isRenamed(originalPath) {
			log.Printf("discovery: not a move, base path already renamed")
			return false
		}
		return true
	}

	// If it's not a move it's just a local-NEW.
	if !moveCheck() {
		postProcessLocalNew(path)
		finalize(path, recurseQueryServer)
		return
	}

	// TODO: checkMovePermissions(base.RemotePerm, originalPath, item.IsDirectory())
	// gates the rename upstream, and on refusal falls back to local-NEW while
	// either undoing or forbidding the source delete. That needs
	// checkPermissions(), which is not ported.

	wasDeletedOnClient, oldEtag := s.findAndCancelDeletedJob(originalPath)

	processRename := func(path PathTuple) PathTuple {
		adjustedOriginalPath := s.adjustRenamedPath(originalPath, DirectionDown)
		s.renamedItemsLocal[originalPath] = path.Target
		item.RenameTarget = path.Target
		path.Server = adjustedOriginalPath
		item.SetLocalName(path.Server)
		path.Original = originalPath
		item.OriginalFile = path.Original
		item.ModTime = base.ModTime
		item.Inode = base.Inode
		item.SetInstruction(InstructionRename)
		item.Direction = DirectionUp
		item.FileID = base.FileID
		item.RemotePerm = base.RemotePerm
		item.Etag = base.Etag
		item.Type = base.Type

		// Discard any download/dehydrate tags on the base file. They could be
		// preserved and honored in a follow-up sync, but it complicates
		// handling a lot and will happen rarely.
		if item.Type == journal.ItemTypeVirtualFileDownload {
			item.Type = journal.ItemTypeVirtualFile
		} else if item.Type == journal.ItemTypeVirtualFileDehydration {
			item.Type = journal.ItemTypeFile
		}

		log.Printf("discovery: rename detected (up) %s -> %s", item.LocalName(), item.RenameTarget)
		return path
	}

	if wasDeletedOnClient {
		mode := NormalQuery
		if oldEtag == base.Etag {
			mode = ParentNotChanged
		}
		finalize(processRename(path), mode)
		return
	}

	// Upstream queries the server for the original path's etag here to confirm
	// the source is gone: 404 or a changed etag means this is not a rename but
	// a local-NEW. Without an HTTP client, take the local-NEW branch, which is
	// the answer upstream uses when the query says "still there".
	// TODO: port the RequestEtagJob branch once the remote query lands.
	log.Printf("discovery: cannot confirm local rename of %s without a server query, treating as new", originalPath)
	postProcessLocalNew(path)
	finalize(path, recurseQueryServer)
}

// ===========================================================================
// processFileConflict — discovery.cpp:875-944.
// ===========================================================================

func (s *Syncer) processFileConflict(item *SyncFileItem, path PathTuple,
	localEntry LocalInfo, serverEntry RemoteFileData, dbEntry journal.SyncJournalFileRecord,
) {
	item.PreviousSize = localEntry.Size()
	item.PreviousModtime = localEntry.ModTime()

	if serverEntry.IsDirectory() && localEntry.IsDirectory() {
		// Folders of the same path are always considered equal.
		item.SetInstruction(InstructionUpdateMetadata)
		return
	}

	// A conflict with a virtual should lead to virtual file download.
	if dbEntry.IsVirtualFile() || localEntry.IsVirtualFile() {
		item.Type = journal.ItemTypeVirtualFileDownload
	}

	// If there's no content hash, use heuristics.
	if serverEntry.ChecksumHeader() == "" {
		// If the size or mtime is different, it's definitely a conflict.
		sizeDiffers := serverEntry.Size() != localEntry.Size()
		modTimeDiffers := serverEntry.ModTime() != localEntry.ModTime()
		isConflict := sizeDiffers || modTimeDiffers
		if isConflict {
			log.Printf("discovery: %s: detected conflict: size difference: %t mtime difference: %t",
				serverEntry.Name(), sizeDiffers, modTimeDiffers)
		}
		// It could be a conflict even if size and mtime match. Older client
		// versions always treated these as a non-conflict, and that behavior
		// is preserved when the server provides no content checksum: so there
		// can be !isConflict here even though the contents differ. This is an
		// intentional tradeoff — downloading and comparing would be correct
		// but wastes too much. In particular this NEW/NEW situation with equal
		// sizes and mtimes pops up when the local database is lost.
		if isConflict {
			item.SetInstruction(InstructionConflict)
			item.Direction = DirectionNone
		} else {
			item.SetInstruction(InstructionUpdateMetadata)
			item.Direction = DirectionDown
		}
		return
	}

	// Do we have an UploadInfo for this? Maybe the upload completed but the
	// connection broke just before we received the etag (issue #5106).
	up, err := s.journal.UploadInfo(path.Original)
	if err != nil {
		s.dbError(err)
		return
	}
	if up.Valid && up.ContentChecksum == serverEntry.ChecksumHeader() {
		// Solve the conflict into an upload, or update metadata.
		if up.ModTime == localEntry.ModTime() && up.Size == localEntry.Size() {
			item.SetInstruction(InstructionUpdateMetadata)
		} else {
			item.SetInstruction(InstructionSync)
		}
		item.Direction = DirectionUp

		if item.Instruction() == InstructionUpdateMetadata {
			// Update the etag and other server metadata in the journal already.
			item.Etag = serverEntry.Etag()
			item.FileID = serverEntry.FileID()
			item.RemotePerm = serverEntry.RemotePerm()
			item.ChecksumHeader = serverEntry.ChecksumHeader()
		}
		return
	}

	// Rely on content hash comparisons to optimize away non-conflicts inside
	// the job.
	item.SetInstruction(InstructionConflict)
	item.Direction = DirectionNone
}

// ===========================================================================
// Terminal and helpers.
// ===========================================================================

// processFileFinalize is the non-recursive part of discovery.cpp:946-990. The
// sub-directory job scheduling it does upstream needs the per-directory job,
// which is not ported.
func (s *Syncer) processFileFinalize(item *SyncFileItem, path PathTuple, recurse bool,
	recurseQueryLocal, recurseQueryServer QueryMode,
) {
	if path.Original != path.Target &&
		item.Instruction()&(InstructionUpdateMetadata|InstructionNone) != 0 {
		// Otherwise subitems are not updated.
		item.SetInstruction(InstructionRename)
		item.RenameTarget = path.Target
		// TODO: upstream takes the direction from the parent _dirItem.
	}

	log.Printf("discovery: discovered %s %v %v %v",
		item.LocalName(), item.Instruction(), item.Direction, item.Type)

	if item.IsDirectory() && item.Instruction() == InstructionSync {
		item.SetInstruction(InstructionUpdateMetadata)
	}

	removed := item.Instruction() == InstructionRemove
	// TODO: checkPermissions(item) can turn this into a restoration.
	// TODO: if recurse, queue a sub-directory job.
	_ = recurse
	_ = recurseQueryLocal
	_ = recurseQueryServer

	if removed || (item.Type == journal.ItemTypeVirtualFile && item.Instruction() == InstructionNew) {
		// For the purpose of rename deletion, a restored deleted placeholder
		// counts as deleted.
		s.deletedItem[path.Original] = item
		return
	}
	s.emitItemDiscovered(item)
}

func (s *Syncer) emitItemDiscovered(item *SyncFileItem) {
	if s.onItemDiscovered != nil {
		s.onItemDiscovered(item)
	}
}

// dbError is ProcessDirectoryJob::dbError().
func (s *Syncer) dbError(err error) {
	log.Printf("discovery: error while reading the database: %v", err)
}

func (s *Syncer) vfsNeedsMetadataUpdate(item *SyncFileItem) bool {
	if s.needsMetadataUpdate == nil {
		return false
	}
	return s.needsMetadataUpdate(item)
}

func (s *Syncer) vfsIsDehydratedPlaceholder(absPath string) bool {
	if s.isDehydratedPlaceholder == nil {
		return false
	}
	return s.isDehydratedPlaceholder(absPath)
}

// fsCasePreserving is Utility::fsCasePreserving() (utility.cpp:176-181):
// true on Windows and macOS.
func fsCasePreserving() bool {
	return runtime.GOOS == "windows" || runtime.GOOS == "darwin"
}

// computeLocalChecksum is the static helper at discovery.cpp:388-400: compute
// the file's checksum using the algorithm named in header and assign the
// result to item.ChecksumHeader.
func computeLocalChecksum(header, path string, item *SyncFileItem) bool {
	ch := journal.ParseChecksumHeader(header)
	if !ch.IsValid() {
		return false
	}
	sum := checksumFile(path, ch.Type())
	if sum == "" {
		return false
	}
	item.ChecksumHeader = journal.NewChecksumHeader(ch.Type(), sum).MakeChecksumHeader()
	return true
}

// checksumFile is ComputeChecksum::computeNowOnFile(). The crypto hashes are
// hex encoded; adler32 is base-16 without padding, matching
// QByteArray::number(adler, 16).
func checksumFile(path string, algo journal.ChecksumAlgorithm) string {
	f, err := os.Open(path)
	if err != nil {
		log.Printf("checksums: could not open file %s for reading and computing checksum: %v", path, err)
		return ""
	}
	defer f.Close()

	if algo == journal.ChecksumADLER32 {
		h := adler32.New()
		if _, err := io.Copy(h, f); err != nil {
			return ""
		}
		return strconv.FormatUint(uint64(h.Sum32()), 16)
	}

	var h hash.Hash
	switch algo {
	case journal.ChecksumSHA3_256:
		h = sha3.New256()
	case journal.ChecksumSHA256:
		h = sha256.New()
	case journal.ChecksumSHA1:
		h = sha1.New()
	case journal.ChecksumMD5:
		h = md5.New()
	default:
		log.Printf("checksums: failed to compute checksum: unsupported algorithm %v", algo)
		return ""
	}
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}
