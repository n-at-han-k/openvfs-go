package fuse

// Transpiled from src/openvfs/openvfconstants.h.
//
// Defines the available attributes and their field names in the messagepack
// blob.

// Version mirrors OPENVFS_VERSION_MAJOR, which ecm_setup_version() derives
// from the CMake project version (0.1.0).
const Version = 0

// XAttributeNames
const (
	// XAttrData is the XAttribute name for OpenVfs objects.
	XAttrData = "user.openvfs.data"
	// XAttrRealSize is the Xattribute provided by OpenVFSFuse to determine
	// the size on the filesystem.
	XAttrRealSize     = "user.openvfs.realsize"
	XAttrRegistration = "user.openvfs.registration"
)

type RegistrationAttributes uint8

const (
	RegistrationOwner RegistrationAttributes = iota
	RegistrationVersion
)

func (n RegistrationAttributes) String() string {
	switch n {
	case RegistrationOwner:
		return "owner"
	case RegistrationVersion:
		return "version"
	}
	return ""
}

type Attributes uint8

const (
	AttrEtag Attributes = iota
	AttrFileID
	AttrSize
	AttrState
	AttrPinState
)

func (n Attributes) String() string {
	switch n {
	case AttrEtag:
		return "etag"
	case AttrFileID:
		return "fileid"
	case AttrSize:
		return "size"
	case AttrState:
		return "state"
	case AttrPinState:
		return "pinstate"
	}
	return ""
}

type State uint8

const (
	StateHydrated State = iota
	StateDehydrated
	StateHydrating
)

func (n State) String() string {
	switch n {
	case StateHydrated:
		return "hydrated"
	case StateDehydrated:
		return "dehydrated"
	case StateHydrating:
		return "hydrating"
	}
	return ""
}

type PinState uint8

const (
	PinUnspecified PinState = iota
	PinOnlineOnly
	PinAlwaysLocal
	PinInherited
	PinExcluded
)

// String mirrors Constants::name(PinStates) verbatim, including upstream's
// OnlineOnly/AlwaysLocal returning "hydrated"/"dehydrated".
func (n PinState) String() string {
	switch n {
	case PinOnlineOnly:
		return "hydrated"
	case PinAlwaysLocal:
		return "dehydrated"
	case PinInherited:
		return "inherited"
	case PinExcluded:
		return "excluded"
	case PinUnspecified:
		return "unspecified"
	}
	return ""
}
