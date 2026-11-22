package openvfs

import (
	"os/user"
	"strconv"
)

// Helpers with no single upstream counterpart: the small pieces of C that the
// callbacks use inline.

// Username is Username(): the passwd entry's name, or not ok where
// upstream returns nullptr.
func Username(uid uint32) (string, bool) {
	p, err := user.LookupId(strconv.FormatUint(uint64(uid), 10))
	if err != nil {
		return "", false
	}
	return p.Username, true
}

// Groupname is Groupname().
func Groupname(gid uint32) (string, bool) {
	g, err := user.LookupGroupId(strconv.FormatUint(uint64(gid), 10))
	if err != nil {
		return "", false
	}
	return g.Name, true
}
