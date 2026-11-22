package fuse

import (
	"bytes"
	"fmt"
	"os"
	"strconv"

	"github.com/vmihailenco/msgpack/v5"
)

// toMsgpack is nlohmann::json::to_msgpack(), byte for byte. nlohmann backs a
// JSON object with a std::map, so its keys go out in increasing order, and it
// writes every integer in the narrowest form that holds it.
func toMsgpack(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := msgpack.NewEncoder(&buf)
	enc.SetSortMapKeys(true)
	enc.UseCompactInts(true)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Transpiled from src/openvfs/openvfs.{h,cpp}.

// Registration is the blob stored in user.openvfs.registration.
type Registration struct {
	Path    string
	Owner   string
	Version int
	Error   string
}

func newRegistration(absolutePath, owner string) Registration {
	return Registration{Path: absolutePath, Owner: owner, Version: Version}
}

func (r Registration) IsOK() bool { return r.Error == "" }

func (r Registration) ToData() ([]byte, error) {
	return toMsgpack(map[string]any{
		RegistrationOwner.String():   r.Owner,
		RegistrationVersion.String(): r.Version,
	})
}

func RegistrationFromData(absolutePath string, d []byte) Registration {
	if len(d) == 0 {
		out := newRegistration(absolutePath, "")
		out.Error = "Invalid path"
		return out
	}
	var j struct {
		Owner   string `msgpack:"owner"`
		Version int    `msgpack:"version"`
	}
	if err := msgpack.Unmarshal(d, &j); err != nil {
		// nlohmann throws here; upstream lets it propagate, we report it.
		out := newRegistration(absolutePath, "")
		out.Error = err.Error()
		return out
	}
	info := newRegistration(absolutePath, j.Owner)
	info.Version = j.Version
	return info
}

func RegistrationFromAttributes(absolutePath string) Registration {
	data, ok := xattrGetString(absolutePath, XAttrRegistration)
	if !ok {
		out := newRegistration(absolutePath, "")
		out.Error = "No registration info found"
		return out
	}
	return RegistrationFromData(absolutePath, []byte(data))
}

func RegisterFilesystem(path, owner string) Registration {
	if info := RegistrationFromAttributes(path); info.IsOK() && info.Owner != owner {
		info.Error = fmt.Sprintf("Failed to register OpenVFS. The path '%s' is owned by: %s", path, info.Owner)
		return info
	}
	info := newRegistration(path, owner)
	registration, err := info.ToData()
	if err != nil {
		info.Error = "Failed to register OpenVFS"
		return info
	}
	if xattrSet(path, XAttrRegistration, registration, 0) != nil {
		info.Error = "Failed to register OpenVFS"
		return info
	}
	return info
}

func UnregisterFilesystem(info Registration) bool {
	if xattrRemove(info.Path, XAttrRegistration) != nil {
		fmt.Fprintln(os.Stderr, "Failed to unregister OpenVFS")
		return false
	}
	// TODO: what to do with dehydtrated files
	return true
}

// PlaceHolderAttributes is the blob stored in user.openvfs.data.
type PlaceHolderAttributes struct {
	AbsolutePath string
	Etag         string
	FileID       string
	Size         uint64
	State        State
	PinState     PinState

	ok bool
}

// NewPlaceHolderAttributes creates a PlaceHolderAttributes object from a given
// absolute path. It is assumed the file exists and is not a placeholder.
func NewPlaceHolderAttributes(absolutePath string) PlaceHolderAttributes {
	// Upstream's default member initialisers: an existing file is assumed.
	return PlaceHolderAttributes{
		AbsolutePath: absolutePath,
		State:        StateHydrated,
		PinState:     PinInherited,
	}
}

func (a PlaceHolderAttributes) IsOK() bool { return a.ok }

// CreatePlaceHolder is PlaceHolderAttributes::create().
func CreatePlaceHolder(absolutePath, etag, fileID string, size uint64) PlaceHolderAttributes {
	return PlaceHolderAttributes{
		AbsolutePath: absolutePath,
		Etag:         etag,
		FileID:       fileID,
		Size:         size,
		State:        StateDehydrated,
		PinState:     PinInherited,
		ok:           true,
	}
}

func (a PlaceHolderAttributes) ToData() ([]byte, error) {
	// Upstream asserts isOk() here.
	return toMsgpack(map[string]any{
		AttrEtag.String():     a.Etag,
		AttrFileID.String():   a.FileID,
		AttrSize.String():     a.Size,
		AttrState.String():    uint8(a.State),
		AttrPinState.String(): uint8(a.PinState),
	})
}

func PlaceHolderFromData(absolutePath string, d []byte) PlaceHolderAttributes {
	if len(d) == 0 {
		return NewPlaceHolderAttributes(absolutePath)
	}
	var j struct {
		Etag     string `msgpack:"etag"`
		FileID   string `msgpack:"fileid"`
		Size     uint64 `msgpack:"size"`
		State    uint8  `msgpack:"state"`
		PinState uint8  `msgpack:"pinstate"`
	}
	if err := msgpack.Unmarshal(d, &j); err != nil {
		// nlohmann throws here; upstream lets it propagate.
		return NewPlaceHolderAttributes(absolutePath)
	}
	return PlaceHolderAttributes{
		AbsolutePath: absolutePath,
		Etag:         j.Etag,
		FileID:       j.FileID,
		Size:         j.Size,
		State:        State(j.State),
		PinState:     PinState(j.PinState),
		ok:           true,
	}
}

// PlaceHolderFromAttributes is the std::optional-returning overload: ok is
// false when the attribute is absent.
func PlaceHolderFromAttributes(absolutePath string) (PlaceHolderAttributes, bool) {
	data, ok := xattrGetString(absolutePath, XAttrData)
	if !ok {
		return PlaceHolderAttributes{}, false
	}
	return PlaceHolderFromData(absolutePath, []byte(data)), true
}

// placeHolderFromAttributesAt is PlaceHolderFromAttributes for a path below the
// mount, resolved against the pre-mount root descriptor. absolutePath is kept
// for the external name upstream carries in PlaceHolderAttributes.
func placeHolderFromAttributesAt(dirfd int, rel, absolutePath string) (PlaceHolderAttributes, bool) {
	data, ok := xattrGetAtString(dirfd, rel, XAttrData)
	if !ok {
		return PlaceHolderAttributes{}, false
	}
	return PlaceHolderFromData(absolutePath, []byte(data)), true
}

// RealSize is PlaceHolderAttributes::realSize().
func (a PlaceHolderAttributes) RealSize() uint64 {
	size, ok := xattrGetString(a.AbsolutePath, XAttrRealSize)
	if !ok {
		// the fuse layer is not available, so we can use the actual file size
		st, err := os.Stat(a.AbsolutePath)
		if err != nil {
			return 0
		}
		return uint64(st.Size())
	}
	n, err := strconv.ParseInt(size, 10, 64)
	if err != nil {
		return 0
	}
	return uint64(n)
}

func (a PlaceHolderAttributes) Validate() bool {
	if !a.ok {
		return false
	}
	switch a.State {
	case StateHydrated:
		return a.Size == 0
	case StateDehydrated:
		return a.RealSize() == 0
	case StateHydrating:
		return true
	}
	return true
}

// DefaultData is the lambda in openVFSfuse_getxattr(): the attributes handed
// out for a file that carries no data attribute of its own.
func DefaultData() []byte {
	attributes := CreatePlaceHolder("", "", "", 0)
	attributes.State = StateHydrated
	data, _ := attributes.ToData()
	return data
}

// SetPlaceHolderAttributes has no upstream counterpart; upstream writes the
// data attribute only through the fuse setxattr entry point. It is kept so a
// dehydrator can produce placeholders.
func SetPlaceHolderAttributes(path string, attr PlaceHolderAttributes) error {
	data, err := attr.ToData()
	if err != nil {
		return err
	}
	return xattrSet(path, XAttrData, data, 0)
}
