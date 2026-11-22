//go:build windows

package directory

import (
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"example.com/project/src/journal"
)

type localStat struct {
	inode    uint64
	size     int64
	modtime  int64
	typ      journal.ItemType
	isHidden bool
}

const fileAttributeTagInfoClass = 9

type fileAttributeTagInfo struct {
	FileAttributes uint32
	ReparseTag     uint32
}

func isReparseTagNameSurrogate(tag uint32) bool { return tag&0x20000000 != 0 }

func statLocalInfo(path string) (localStat, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return localStat{}, err
	}
	h, err := windows.CreateFile(p, 0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return localStat{}, err
	}
	defer windows.CloseHandle(h)

	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &info); err != nil {
		return localStat{}, err
	}

	out := localStat{
		inode:    uint64(info.FileIndexHigh)<<32 | uint64(info.FileIndexLow),
		size:     int64(info.FileSizeHigh)<<32 | int64(info.FileSizeLow),
		modtime:  time.Unix(0, info.LastWriteTime.Nanoseconds()).Unix(),
		isHidden: info.FileAttributes&windows.FILE_ATTRIBUTE_HIDDEN != 0,
	}

	isSymLink := false
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		var tagInfo fileAttributeTagInfo
		if err := windows.GetFileInformationByHandleEx(h, fileAttributeTagInfoClass,
			(*byte)(unsafe.Pointer(&tagInfo)), uint32(unsafe.Sizeof(tagInfo))); err != nil {
			return localStat{}, err
		}
		isSymLink = isReparseTagNameSurrogate(tagInfo.ReparseTag)
	}

	switch {
	case isSymLink:
		out.typ = journal.ItemTypeSymLink
	case info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0:
		out.typ = journal.ItemTypeDirectory
	default:
		out.typ = journal.ItemTypeFile
	}
	return out, nil
}
