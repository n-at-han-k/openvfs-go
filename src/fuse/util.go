package fuse

import (
	"os/user"
	"strconv"
)

// Helpers with no single upstream counterpart: the small pieces of C that the
// callbacks use inline.

// getusername is getusername(): the passwd entry's name, or not ok where
// upstream returns nullptr.
func getusername(uid uint32) (string, bool) {
	p, err := user.LookupId(strconv.FormatUint(uint64(uid), 10))
	if err != nil {
		return "", false
	}
	return p.Username, true
}

// getgroupname is getgroupname().
func getgroupname(gid uint32) (string, bool) {
	g, err := user.LookupGroupId(strconv.FormatUint(uint64(gid), 10))
	if err != nil {
		return "", false
	}
	return g.Name, true
}
