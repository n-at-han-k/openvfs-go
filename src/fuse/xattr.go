package fuse

// Transpiled from src/openvfs/xattr.{h,cpp}. The platform specific primitives
// live in xattr_linux.go, xattr_darwin.go and xattr_windows.go; upstream
// selects between l*xattr and *xattr with XATTR_NOFOLLOW with an #ifdef and has
// no Windows path at all.
//
// The *At variants have no upstream counterpart. The C++ layer reaches the file
// below the mount by fchdir()ing to a descriptor of the root that was opened
// before mounting and then using a relative path; resolving a relative path
// against that descriptor does the same thing without the process-wide side
// effect, which is what this port uses everywhere.

// xattrGetString is Xattr::CPP::getxattr: grow the buffer by 255 bytes and
// retry for as long as the attribute does not fit.
func xattrGetString(path, name string) (string, bool) {
	size := 0
	for {
		size += 255
		value := make([]byte, size)
		res, err := xattrGet(path, name, value)
		if err == nil {
			return string(value[:res]), true
		}
		if !isErrRange(err) {
			return "", false
		}
	}
}

// xattrGetAtString is xattrGetString against a root-relative path.
func xattrGetAtString(dirfd int, rel, name string) (string, bool) {
	size := 0
	for {
		size += 255
		value := make([]byte, size)
		res, err := xattrGetAt(dirfd, rel, name, value)
		if err == nil {
			return string(value[:res]), true
		}
		if !isErrRange(err) {
			return "", false
		}
	}
}
