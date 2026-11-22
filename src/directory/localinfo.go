package directory

import (
	"os"
	"path/filepath"

	"example.com/project/src/journal"
)

type LocalInfo struct {
	path     string
	name     string
	typ      journal.ItemType
	modtime  int64
	size     int64
	inode    uint64
	isHidden bool
}

func NewLocalInfo(path string) LocalInfo {
	out := LocalInfo{path: path, name: filepath.Base(path), typ: journal.ItemTypeUnsupported}
	st, err := statLocalInfo(path)
	if err != nil {
		out.name = ""
		return out
	}
	out.inode = st.inode
	out.size = st.size
	out.modtime = st.modtime
	out.typ = st.typ
	out.isHidden = st.isHidden
	return out
}

func NewLocalInfoFromDirEntry(dir string, entry os.DirEntry) LocalInfo {
	return NewLocalInfo(filepath.Join(dir, entry.Name()))
}

func (i LocalInfo) IsHidden() bool { return i.isHidden }

func (i LocalInfo) Name() string           { return i.name }
func (i LocalInfo) Path() string           { return i.path }
func (i LocalInfo) ModTime() int64         { return i.modtime }
func (i LocalInfo) Size() int64            { return i.size }
func (i LocalInfo) Inode() uint64          { return i.inode }
func (i LocalInfo) Type() journal.ItemType { return i.typ }

func (i *LocalInfo) SetType(typ journal.ItemType) { i.typ = typ }

func (i LocalInfo) IsDirectory() bool { return i.typ == journal.ItemTypeDirectory }

func (i LocalInfo) IsVirtualFile() bool {
	return i.typ == journal.ItemTypeVirtualFile || i.typ == journal.ItemTypeVirtualFileDownload
}

func (i LocalInfo) IsSymLink() bool { return i.typ == journal.ItemTypeSymLink }
func (i LocalInfo) IsValid() bool   { return i.name != "" }
