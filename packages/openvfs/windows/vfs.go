// The Vfs plugin interface, transpiled from src/libsync/vfs/vfs.{h,cpp}.
//
// Interface describing how to deal with virtual/placeholder files.
//
// There are different ways of representing files locally that will only be
// filled with data (hydrated) on demand. One such way would be suffixed files,
// others could be FUSE based or use Windows CfAPI.
//
// This interface intends to decouple the sync algorithm and Folder from the
// details of how a particular VFS solution works.
package windows

import "strings"

// Mode is the kind of VFS in use (or no-VFS).
//
// Currently plugins and modes are one-to-one but that's not required. The raw
// integer values are used in Qml.
type Mode uint8

const (
	ModeOff          Mode = 0
	ModeWindowsCfApi Mode = 1
	ModeOpenVFS      Mode = 2
)

// String is Utility::enumToString(Vfs::Mode).
func (m Mode) String() string {
	switch m {
	case ModeOff:
		return "off"
	case ModeWindowsCfApi:
		return "wincfapi"
	case ModeOpenVFS:
		return "openvfs"
	}
	return ""
}

// ModeFromString is Vfs::modeFromString(). ok is false where upstream returns
// an empty Optional.
func ModeFromString(str string) (Mode, bool) {
	// Note: Strings are used for the config file, so they must not change.
	switch strings.ToLower(str) {
	case "off":
		return ModeOff, true
	case "wincfapi":
		return ModeWindowsCfApi, true
	case "openvfs":
		return ModeOpenVFS, true
	}
	return ModeOff, false
}

type ConvertToPlaceholderResult uint8

const (
	ConvertToPlaceholderOk ConvertToPlaceholderResult = iota
	ConvertToPlaceholderLocked
)

type AvailabilityError uint8

const (
	// AvailabilityErrorDbError means availability can't be retrieved due to db error.
	AvailabilityErrorDbError AvailabilityError = iota
	// AvailabilityErrorNoSuchItem means availability is not available since the
	// item doesn't exist.
	AvailabilityErrorNoSuchItem
)

// PinState determines whether items should be available locally permanently or
// not. Transpiled from src/libsync/common/pinstate.h.
//
// Note: This enum intentionally mimics CF_PIN_STATE of Windows cfapi.
type PinState uint8

const (
	// PinInherited means the pin state is derived from the state of the parent
	// folder.
	PinInherited PinState = 0
	// PinAlwaysLocal means the file shall be available and up to date locally.
	PinAlwaysLocal PinState = 1
	// PinOnlineOnly means the file shall be a dehydrated placeholder, filled on
	// demand.
	PinOnlineOnly PinState = 2
	// PinUnspecified means the user hasn't made a decision.
	PinUnspecified PinState = 3
	// PinExcluded is the special case for files that are excluded.
	PinExcluded PinState = 4
)

// VfsItemAvailability is a user-facing version of PinState.
//
// NOTE: The numerical values and ordering of this enum are relevant.
type VfsItemAvailability uint8

const (
	// AvailabilityAlwaysLocal means the item and all its subitems are hydrated
	// and pinned AlwaysLocal.
	AvailabilityAlwaysLocal VfsItemAvailability = 0
	// AvailabilityAllHydrated means the item and all its subitems are hydrated.
	AvailabilityAllHydrated VfsItemAvailability = 1
	// AvailabilityMixed means there are dehydrated and hydrated items.
	AvailabilityMixed VfsItemAvailability = 2
	// AvailabilityAllDehydrated means there are only dehydrated items but the
	// pin state isn't all OnlineOnly.
	AvailabilityAllDehydrated VfsItemAvailability = 3
	// AvailabilityOnlineOnly means the item and all its subitems are dehydrated
	// and OnlineOnly.
	AvailabilityOnlineOnly VfsItemAvailability = 4
)

// AvailabilityResult is Vfs::AvailabilityResult, a Result<VfsItemAvailability,
// AvailabilityError>.
type AvailabilityResult struct {
	Availability VfsItemAvailability
	Err          AvailabilityError
	HasErr       bool
}

func AvailabilityOf(a VfsItemAvailability) AvailabilityResult {
	return AvailabilityResult{Availability: a}
}

func AvailabilityErrorOf(e AvailabilityError) AvailabilityResult {
	return AvailabilityResult{Err: e, HasErr: true}
}

// ItemType is SyncFileItem::Type, as far as the vfs plugins read it.
type ItemType uint8

const (
	ItemTypeFile ItemType = iota
	ItemTypeFileMetadataOnly
	ItemTypeDirectory
	ItemTypeVirtualFile
	ItemTypeVirtualFileDehydration
	ItemTypeVirtualFileDownload
)

// SyncFileItem carries the fields the vfs plugins read off an item.
type SyncFileItem struct {
	LocalName string
	FileID    []byte
	Modtime   int64
	Size      int64
	Type      ItemType
	Inode     uint64
}

// SyncFileStatusTag is SyncFileStatus::SyncFileStatusTag.
type SyncFileStatusTag uint8

const (
	StatusNone SyncFileStatusTag = iota
	StatusSync
	StatusWarning
	StatusUpToDate
	StatusError
	StatusExcluded
)

// LocalInfo is the subset of OCC::LocalInfo statTypeVirtualFile() touches.
type LocalInfo struct {
	Path  string
	Type  ItemType
	Valid bool
}

// Account is the part of OCC::Account the cfapi plugin reads.
type Account interface {
	UUID() string
	URL() string
}

// Journal is the part of OCC::SyncJournalDb the plugins reach through
// VfsSetupParams.
type Journal interface {
	SetFileRecord(item SyncFileItem) error
}

// SyncEngine is the part of OCC::SyncEngine the cfapi rename callback reads.
type SyncEngine interface {
	IsExcluded(path string) bool
}

// SetupParams is VfsSetupParams: the collection of parameters for initializing
// a Vfs instance.
type SetupParams struct {
	// Account url, credentials etc for network calls
	Account Account

	// Journal gives access to the sync folder's database.
	//
	// Note: The journal must live at least until the Vfs.Stop() call.
	Journal Journal

	// Strings potentially passed on to the platform
	ProviderDisplayName string
	ProviderName        string
	ProviderVersion     string
	SocketPath          string

	BaseURL                string
	SpaceID                string
	FolderDisplayNameValue string

	SyncEngine SyncEngine

	// Root is the sync folder's local root.
	Root string
}

func (p *SetupParams) FolderDisplayName() string { return p.FolderDisplayNameValue }

// FilesystemPath is VfsSetupParams::filesystemPath().
func (p *SetupParams) FilesystemPath() string { return p.Root }

// Vfs is the OCC::Vfs interface. The Qt signals become the Callbacks a plugin
// is started with.
type Vfs interface {
	Mode() Mode

	// Stop interaction with VFS provider. Like when the client application quits.
	Stop()

	// UnregisterFolder deregisters the folder with the sync provider, like when
	// a folder is removed.
	UnregisterFolder()

	// SocketAPIPinStateActionsShown reports whether the socket api should show
	// pin state options.
	SocketAPIPinStateActionsShown() bool

	// CreatePlaceholder creates a new dehydrated placeholder. Called from
	// PropagateDownload.
	CreatePlaceholder(item SyncFileItem) error

	// NeedsMetadataUpdate is the discovery hook: even unchanged files may need
	// UPDATE_METADATA.
	NeedsMetadataUpdate(item SyncFileItem) bool

	// IsDehydratedPlaceholder determines whether the file at the given absolute
	// path is a dehydrated placeholder.
	IsDehydratedPlaceholder(filePath string) bool

	// StatTypeVirtualFile is similar to IsDehydratedPlaceholder but used from
	// sync discovery. It shall set the type if appropriate.
	StatTypeVirtualFile(fileInfo LocalInfo) LocalInfo

	// SetPinState sets the pin state for the item at a path, and for all items
	// below it. relFilePath is relative to the sync folder, "" for the root.
	SetPinState(relFilePath string, state PinState) bool

	// PinState returns the pin state of an item at a path, or ok false on
	// retrieval error.
	PinState(relFilePath string) (PinState, bool)

	// Availability returns the availability status of an item at a path.
	Availability(folderPath string) AvailabilityResult

	// FileStatusChanged updates in-sync state based on the
	// SyncFileStatusTracker signal.
	FileStatusChanged(systemFileName string, status SyncFileStatusTag)
}

// Callbacks replaces the Qt signals Vfs emits.
type Callbacks struct {
	// Started signals that start completed.
	Started func()
	// Error signals that we encountered an error.
	Error func(err string)
	// NeedSync signals that the vfs plugin detected that the meta data are out
	// of sync and requests a sync with the server.
	NeedSync func()
}
