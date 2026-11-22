//go:build windows

package winvfs

import "unsafe"

// Declarations from the Windows SDK <cfapi.h>, needed because the cloud filter
// API is reached through cldapi.dll rather than through cgo.
//
// EVERY STRUCT HERE IS LAYOUT CRITICAL. The sizes and offsets are the documented
// 64-bit ones; cfapi_layout_test.go pins them with unsafe.Sizeof/Offsetof, and
// testdata/cfapi_layout_probe.c prints the same numbers from the real header so
// the two can be compared once on a Windows machine. Do not reorder or retype a
// field without re-running that probe.

// LARGE_INTEGER is used throughout cfapi as a 64 bit integer; only its QuadPart
// is ever touched, so it is declared as the integer it overlays.
type LARGE_INTEGER = int64

// CF_CONNECTION_KEY wraps the LONGLONG the API hands back from
// CfConnectSyncRoot.
type CF_CONNECTION_KEY struct {
	Internal int64
}

// CF_TRANSFER_KEY and CF_REQUEST_KEY are both LARGE_INTEGER.
type CF_TRANSFER_KEY = int64
type CF_REQUEST_KEY = int64

// NTSTATUS values the plugin reports back through CfExecute.
const (
	STATUS_SUCCESS                  = 0x00000000
	STATUS_CLOUD_FILE_NOT_IN_SYNC   = 0xC000CF17
	STATUS_CLOUD_FILE_NOT_SUPPORTED = 0xC000CF0A
)

// CF_CALLBACK_TYPE
const (
	CF_CALLBACK_TYPE_FETCH_DATA                   = 0x00000000
	CF_CALLBACK_TYPE_VALIDATE_DATA                = 0x00000001
	CF_CALLBACK_TYPE_CANCEL_FETCH_DATA            = 0x00000002
	CF_CALLBACK_TYPE_FETCH_PLACEHOLDERS           = 0x00000003
	CF_CALLBACK_TYPE_CANCEL_FETCH_PLACEHOLDERS    = 0x00000004
	CF_CALLBACK_TYPE_NOTIFY_FILE_OPEN_COMPLETION  = 0x00000005
	CF_CALLBACK_TYPE_NOTIFY_FILE_CLOSE_COMPLETION = 0x00000006
	CF_CALLBACK_TYPE_NOTIFY_DEHYDRATE             = 0x00000007
	CF_CALLBACK_TYPE_NOTIFY_DEHYDRATE_COMPLETION  = 0x00000008
	CF_CALLBACK_TYPE_NOTIFY_DELETE                = 0x00000009
	CF_CALLBACK_TYPE_NOTIFY_DELETE_COMPLETION     = 0x0000000A
	CF_CALLBACK_TYPE_NOTIFY_RENAME                = 0x0000000B
	CF_CALLBACK_TYPE_NOTIFY_RENAME_COMPLETION     = 0x0000000C
	// CF_CALLBACK_TYPE_NONE terminates the registration table; it is what
	// CF_CALLBACK_REGISTRATION_END expands to together with a nil callback.
	CF_CALLBACK_TYPE_NONE = 0xFFFFFFFF
)

// CF_CONNECT_FLAGS
const (
	CF_CONNECT_FLAG_NONE                          = 0x00000000
	CF_CONNECT_FLAG_REQUIRE_PROCESS_INFO          = 0x00000002
	CF_CONNECT_FLAG_REQUIRE_FULL_FILE_PATH        = 0x00000004
	CF_CONNECT_FLAG_BLOCK_SELF_IMPLICIT_HYDRATION = 0x00000008
)

// CF_OPERATION_TYPE
const (
	CF_OPERATION_TYPE_TRANSFER_DATA         = 0x00000000
	CF_OPERATION_TYPE_RETRIEVE_DATA         = 0x00000001
	CF_OPERATION_TYPE_ACK_DATA              = 0x00000002
	CF_OPERATION_TYPE_RESTART_HYDRATION     = 0x00000003
	CF_OPERATION_TYPE_TRANSFER_PLACEHOLDERS = 0x00000004
	CF_OPERATION_TYPE_ACK_DEHYDRATE         = 0x00000005
	CF_OPERATION_TYPE_ACK_RENAME            = 0x00000006
	CF_OPERATION_TYPE_ACK_DELETE            = 0x00000007
)

// CF_PIN_STATE
const (
	CF_PIN_STATE_UNSPECIFIED = 0
	CF_PIN_STATE_PINNED      = 1
	CF_PIN_STATE_UNPINNED    = 2
	CF_PIN_STATE_EXCLUDED    = 3
	CF_PIN_STATE_INHERIT     = 4
)

// CF_SET_PIN_FLAGS
const (
	CF_SET_PIN_FLAG_NONE                  = 0x00000000
	CF_SET_PIN_FLAG_RECURSE               = 0x00000001
	CF_SET_PIN_FLAG_RECURSE_ONLY          = 0x00000002
	CF_SET_PIN_FLAG_RECURSE_STOP_ON_ERROR = 0x00000004
)

// CF_IN_SYNC_STATE
const (
	CF_IN_SYNC_STATE_NOT_IN_SYNC = 0
	CF_IN_SYNC_STATE_IN_SYNC     = 1
)

// CF_SET_IN_SYNC_FLAGS
const CF_SET_IN_SYNC_FLAG_NONE = 0x00000000

// CF_PLACEHOLDER_STATE
const (
	CF_PLACEHOLDER_STATE_NO_STATES              = 0x00000000
	CF_PLACEHOLDER_STATE_PLACEHOLDER            = 0x00000001
	CF_PLACEHOLDER_STATE_SYNC_ROOT              = 0x00000002
	CF_PLACEHOLDER_STATE_ESSENTIAL_PROP_PRESENT = 0x00000004
	CF_PLACEHOLDER_STATE_IN_SYNC                = 0x00000008
	CF_PLACEHOLDER_STATE_PARTIAL                = 0x00000010
	CF_PLACEHOLDER_STATE_PARTIALLY_ON_DISK      = 0x00000020
	CF_PLACEHOLDER_STATE_INVALID                = 0xFFFFFFFF
)

// CF_PLACEHOLDER_INFO_CLASS
const (
	CF_PLACEHOLDER_INFO_BASIC    = 0
	CF_PLACEHOLDER_INFO_STANDARD = 1
)

// CF_PLACEHOLDER_CREATE_FLAGS
const (
	CF_PLACEHOLDER_CREATE_FLAG_NONE                         = 0x00000000
	CF_PLACEHOLDER_CREATE_FLAG_DISABLE_ON_DEMAND_POPULATION = 0x00000001
	CF_PLACEHOLDER_CREATE_FLAG_MARK_IN_SYNC                 = 0x00000002
	CF_PLACEHOLDER_CREATE_FLAG_SUPERSEDE                    = 0x00000004
	CF_PLACEHOLDER_CREATE_FLAG_ALWAYS_FULL                  = 0x00000008
)

// CF_CREATE_FLAGS
const CF_CREATE_FLAG_NONE = 0x00000000

// CF_UPDATE_FLAGS
const (
	CF_UPDATE_FLAG_NONE                         = 0x00000000
	CF_UPDATE_FLAG_VERIFY_IN_SYNC               = 0x00000001
	CF_UPDATE_FLAG_MARK_IN_SYNC                 = 0x00000002
	CF_UPDATE_FLAG_DEHYDRATE                    = 0x00000004
	CF_UPDATE_FLAG_ENABLE_ON_DEMAND_POPULATION  = 0x00000008
	CF_UPDATE_FLAG_DISABLE_ON_DEMAND_POPULATION = 0x00000010
	CF_UPDATE_FLAG_REMOVE_FILE_IDENTITY         = 0x00000020
	CF_UPDATE_FLAG_CLEAR_IN_SYNC                = 0x00000040
	CF_UPDATE_FLAG_REMOVE_PROPERTY              = 0x00000080
	CF_UPDATE_FLAG_PASSTHROUGH_FS_METADATA      = 0x00000100
	CF_UPDATE_FLAG_ALWAYS_FULL                  = 0x00000200
	CF_UPDATE_FLAG_ALLOW_PARTIAL                = 0x00000400
)

// CF_CONVERT_FLAGS
const (
	CF_CONVERT_FLAG_NONE                        = 0x00000000
	CF_CONVERT_FLAG_MARK_IN_SYNC                = 0x00000001
	CF_CONVERT_FLAG_DEHYDRATE                   = 0x00000002
	CF_CONVERT_FLAG_ENABLE_ON_DEMAND_POPULATION = 0x00000004
	CF_CONVERT_FLAG_ALWAYS_FULL                 = 0x00000008
	CF_CONVERT_FLAG_FORCE_CONVERT_TO_CLOUD_FILE = 0x00000010
)

// CF_CALLBACK_RENAME_FLAGS
const (
	CF_CALLBACK_RENAME_FLAG_NONE            = 0x00000000
	CF_CALLBACK_RENAME_FLAG_IS_DIRECTORY    = 0x00000001
	CF_CALLBACK_RENAME_FLAG_SOURCE_IN_SCOPE = 0x00000002
	CF_CALLBACK_RENAME_FLAG_TARGET_IN_SCOPE = 0x00000004
)

// CF_CALLBACK_DEHYDRATION_REASON
const (
	CF_CALLBACK_DEHYDRATION_REASON_NONE              = 0
	CF_CALLBACK_DEHYDRATION_REASON_USER_MANUAL       = 1
	CF_CALLBACK_DEHYDRATION_REASON_SYSTEM_LOW_SPACE  = 2
	CF_CALLBACK_DEHYDRATION_REASON_SYSTEM_INACTIVITY = 3
	CF_CALLBACK_DEHYDRATION_REASON_SYSTEM_OS_UPGRADE = 4
)

// FILE_BASIC_INFO from <winbase.h>, 40 bytes.
type FILE_BASIC_INFO struct {
	CreationTime   LARGE_INTEGER // 0
	LastAccessTime LARGE_INTEGER // 8
	LastWriteTime  LARGE_INTEGER // 16
	ChangeTime     LARGE_INTEGER // 24
	FileAttributes uint32        // 32
	_              uint32        // 36 tail padding
}

// FILE_ATTRIBUTE_TAG_INFO from <winbase.h>, 8 bytes.
type FILE_ATTRIBUTE_TAG_INFO struct {
	FileAttributes uint32
	ReparseTag     uint32
}

// FileAttributeTagInfo is the FILE_INFO_BY_HANDLE_CLASS value for
// GetFileInformationByHandleEx.
const FileAttributeTagInfo = 9

// CF_FS_METADATA, 48 bytes.
type CF_FS_METADATA struct {
	BasicInfo FILE_BASIC_INFO // 0
	FileSize  LARGE_INTEGER   // 40
}

// CF_PLACEHOLDER_CREATE_INFO, 72 bytes.
type CF_PLACEHOLDER_CREATE_INFO struct {
	RelativeFileName   *uint16        // 0
	FsMetadata         CF_FS_METADATA // 8
	FileIdentity       unsafe.Pointer // 56
	FileIdentityLength uint32         // 64
	Flags              uint32         // 68 (CF_PLACEHOLDER_CREATE_FLAGS)
	Result             int32          // 72 (HRESULT)
	_                  uint32         // 76
	CreateUsn          int64          // 80
}

// CF_PLACEHOLDER_BASIC_INFO, 32 bytes.
type CF_PLACEHOLDER_BASIC_INFO struct {
	PinState           uint32 // 0  (CF_PIN_STATE)
	InSyncState        uint32 // 4  (CF_IN_SYNC_STATE)
	FileId             int64  // 8
	SyncRootFileId     int64  // 16
	FileIdentityLength uint32 // 24
	_                  uint32 // 28
	// FileIdentity is a trailing dynamic array; it is read out of the raw
	// buffer rather than through this field.
}

// CF_PLACEHOLDER_STANDARD_INFO, 80 bytes.
type CF_PLACEHOLDER_STANDARD_INFO struct {
	OnDiskDataSize     int64  // 0
	ValidatedDataSize  int64  // 8
	ModifiedDataSize   int64  // 16
	PropertyStoreSize  int64  // 24
	FileIdentityLength uint32 // 32
	_                  uint32 // 36
	PinState           uint32 // 40
	InSyncState        uint32 // 44
	FileId             int64  // 48
	SyncRootFileId     int64  // 56
	// FileIdentity is a trailing dynamic array.
}

// CF_PROCESS_INFO, 56 bytes.
type CF_PROCESS_INFO struct {
	StructSize    uint32  // 0
	ProcessId     uint32  // 4
	ImagePath     *uint16 // 8
	PackageName   *uint16 // 16
	ApplicationId *uint16 // 24
	CommandLine   *uint16 // 32
	SessionId     int32   // 40
	_             uint32  // 44
}

// CF_CALLBACK_INFO, 128 bytes.
type CF_CALLBACK_INFO struct {
	StructSize             uint32            // 0
	_                      uint32            // 4
	ConnectionKey          CF_CONNECTION_KEY // 8
	CallbackContext        unsafe.Pointer    // 16
	VolumeGuidName         *uint16           // 24
	VolumeDosName          *uint16           // 32
	VolumeSerialNumber     uint32            // 40
	_                      uint32            // 44
	SyncRootFileId         LARGE_INTEGER     // 48
	SyncRootIdentity       unsafe.Pointer    // 56
	SyncRootIdentityLength uint32            // 64
	_                      uint32            // 68
	FileId                 LARGE_INTEGER     // 72
	FileSize               LARGE_INTEGER     // 80
	FileIdentity           unsafe.Pointer    // 88
	FileIdentityLength     uint32            // 96
	_                      uint32            // 100
	NormalizedPath         *uint16           // 104
	TransferKey            CF_TRANSFER_KEY   // 112
	PriorityHint           uint8             // 120
	_                      [7]byte           // 121
	CorrelationVector      unsafe.Pointer    // 128
	ProcessInfo            *CF_PROCESS_INFO  // 136
	RequestKey             CF_REQUEST_KEY    // 144
}

// cfCallbackParametersSize is the size of the CF_CALLBACK_PARAMETERS union,
// which is dominated by its FetchPlaceholders and Rename members.
const cfCallbackParametersSize = 40

// CF_CALLBACK_PARAMETERS is a union; the plugin only reads the FetchData and
// Rename arms, which are projected by the typed views below.
type CF_CALLBACK_PARAMETERS struct {
	ParamSize uint32
	_         uint32
	Data      [cfCallbackParametersSize]byte
}

// CF_CALLBACK_PARAMETERS_FETCH_DATA is the FetchData arm.
type CF_CALLBACK_PARAMETERS_FETCH_DATA struct {
	Flags                 uint32        // 0
	_                     uint32        // 4
	RequiredFileOffset    LARGE_INTEGER // 8
	RequiredLength        LARGE_INTEGER // 16
	OptionalFileOffset    LARGE_INTEGER // 24
	OptionalLength        LARGE_INTEGER // 32
	LastDehydrationReason uint32        // 40
	_                     uint32        // 44
}

// CF_CALLBACK_PARAMETERS_RENAME is the Rename arm.
type CF_CALLBACK_PARAMETERS_RENAME struct {
	Flags      uint32  // 0
	_          uint32  // 4
	TargetPath *uint16 // 8
}

// FetchData projects the union onto its FetchData arm.
func (p *CF_CALLBACK_PARAMETERS) FetchData() *CF_CALLBACK_PARAMETERS_FETCH_DATA {
	return (*CF_CALLBACK_PARAMETERS_FETCH_DATA)(unsafe.Pointer(&p.Data[0]))
}

// Rename projects the union onto its Rename arm.
func (p *CF_CALLBACK_PARAMETERS) Rename() *CF_CALLBACK_PARAMETERS_RENAME {
	return (*CF_CALLBACK_PARAMETERS_RENAME)(unsafe.Pointer(&p.Data[0]))
}

// CF_OPERATION_INFO, 48 bytes.
type CF_OPERATION_INFO struct {
	StructSize        uint32            // 0
	Type              uint32            // 4 (CF_OPERATION_TYPE)
	ConnectionKey     CF_CONNECTION_KEY // 8
	TransferKey       CF_TRANSFER_KEY   // 16
	CorrelationVector unsafe.Pointer    // 24
	SyncStatus        unsafe.Pointer    // 32
	RequestKey        CF_REQUEST_KEY    // 40
}

// cfOperationParametersSize is the size of the CF_OPERATION_PARAMETERS union,
// whose largest arm is TransferPlaceholders.
const cfOperationParametersSize = 40

// CF_OPERATION_PARAMETERS is a union. ParamSize must be set to
// CF_SIZE_OF_OP_PARAM(<arm>) for the arm in use, which is what the
// SizeOfOpParam* helpers below compute.
type CF_OPERATION_PARAMETERS struct {
	ParamSize uint32
	_         uint32
	Data      [cfOperationParametersSize]byte
}

// CF_OPERATION_PARAMETERS_TRANSFER_DATA is the TransferData arm.
type CF_OPERATION_PARAMETERS_TRANSFER_DATA struct {
	Flags            uint32         // 0
	CompletionStatus int32          // 4 (NTSTATUS)
	Buffer           unsafe.Pointer // 8
	Offset           LARGE_INTEGER  // 16
	Length           LARGE_INTEGER  // 24
}

// CF_OPERATION_PARAMETERS_ACK_RENAME is the AckRename arm.
type CF_OPERATION_PARAMETERS_ACK_RENAME struct {
	Flags            uint32 // 0
	CompletionStatus int32  // 4 (NTSTATUS)
}

func (p *CF_OPERATION_PARAMETERS) TransferData() *CF_OPERATION_PARAMETERS_TRANSFER_DATA {
	return (*CF_OPERATION_PARAMETERS_TRANSFER_DATA)(unsafe.Pointer(&p.Data[0]))
}

func (p *CF_OPERATION_PARAMETERS) AckRename() *CF_OPERATION_PARAMETERS_ACK_RENAME {
	return (*CF_OPERATION_PARAMETERS_ACK_RENAME)(unsafe.Pointer(&p.Data[0]))
}

// The CF_SIZE_OF_OP_PARAM(field) macro from cfapiwrapper.h:
//
//	FIELD_OFFSET(CF_OPERATION_PARAMETERS, field) + FIELD_SIZE(CF_OPERATION_PARAMETERS, field)
//
// Every union arm starts at the same offset, so the offset is the one of Data.
var (
	SizeOfOpParamTransferData = uint32(unsafe.Offsetof(CF_OPERATION_PARAMETERS{}.Data)) +
		uint32(unsafe.Sizeof(CF_OPERATION_PARAMETERS_TRANSFER_DATA{}))
	SizeOfOpParamAckRename = uint32(unsafe.Offsetof(CF_OPERATION_PARAMETERS{}.Data)) +
		uint32(unsafe.Sizeof(CF_OPERATION_PARAMETERS_ACK_RENAME{}))
)

// CF_CALLBACK_REGISTRATION is one row of the table handed to
// CfConnectSyncRoot. The table is terminated by
// CF_CALLBACK_REGISTRATION_END, which is {CF_CALLBACK_TYPE_NONE, nil}.
type CF_CALLBACK_REGISTRATION struct {
	Type     uint32
	_        uint32
	Callback uintptr
}
