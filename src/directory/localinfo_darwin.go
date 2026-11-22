//go:build darwin

package directory

import (
	"golang.org/x/sys/unix"

	"example.com/project/src/journal"
)

type localStat struct {
	inode    uint64
	size     int64
	modtime  int64
	typ      journal.ItemType
	isHidden bool
}

func statLocalInfo(path string) (localStat, error) {
	var sb unix.Stat_t
	if err := unix.Lstat(path, &sb); err != nil {
		return localStat{}, err
	}
	out := localStat{
		inode:    sb.Ino,
		size:     sb.Size,
		modtime:  sb.Mtim.Sec,
		isHidden: sb.Flags&unix.UF_HIDDEN != 0,
	}
	switch sb.Mode & unix.S_IFMT {
	case unix.S_IFLNK:
		out.typ = journal.ItemTypeSymLink
	case unix.S_IFDIR:
		out.typ = journal.ItemTypeDirectory
	case unix.S_IFREG:
		out.typ = journal.ItemTypeFile
	default:
		out.typ = journal.ItemTypeUnsupported
	}
	return out, nil
}
