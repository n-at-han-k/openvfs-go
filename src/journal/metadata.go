package journal

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	sqlite "modernc.org/sqlite"
)

const createMetadataTable = `CREATE TABLE IF NOT EXISTS metadata(
phash INTEGER(8),
path VARCHAR(4096),
inode INTEGER,
modtime INTEGER(8),
type INTEGER,
md5 VARCHAR(32),
PRIMARY KEY(phash)
);`

var metadataColumns = []struct{ name, ddl string }{
	{"fileid", "ALTER TABLE metadata ADD COLUMN fileid VARCHAR(128);"},
	{"remotePerm", "ALTER TABLE metadata ADD COLUMN remotePerm VARCHAR(128);"},
	{"filesize", "ALTER TABLE metadata ADD COLUMN filesize BIGINT;"},
	{"ignoredChildrenRemote", "ALTER TABLE metadata ADD COLUMN ignoredChildrenRemote INT;"},
	{"contentChecksum", "ALTER TABLE metadata ADD COLUMN contentChecksum TEXT;"},
	{"contentChecksumTypeId", "ALTER TABLE metadata ADD COLUMN contentChecksumTypeId INTEGER;"},
	{"hasDirtyPlaceholder", "ALTER TABLE metadata ADD COLUMN hasDirtyPlaceholder BOOLEAN;"},
}

var metadataIndexes = []string{
	"CREATE INDEX IF NOT EXISTS metadata_inode ON metadata(inode);",
	"CREATE INDEX IF NOT EXISTS metadata_path ON metadata(path);",
	"CREATE INDEX IF NOT EXISTS metadata_parent ON metadata(parent_hash(path));",
}

func (j *Journal) updateMetadataTableStructure() error {
	for _, c := range metadataColumns {
		added, err := j.addColumnIfMissing("metadata", c.name, c.ddl)
		if err != nil {
			return err
		}
		if added && c.name == "fileid" {
			if _, err := j.db.Exec("CREATE INDEX metadata_file_id ON metadata(fileid);"); err != nil {
				return err
			}
		}
		if added && c.name == "hasDirtyPlaceholder" {
			if _, err := j.db.Exec("CREATE INDEX hasDirtyPlaceholderIndex ON metadata(path) WHERE hasDirtyPlaceholder=TRUE;"); err != nil {
				return err
			}
		}
	}
	return nil
}

const (
	selectFileRecord = `SELECT path, inode, modtime, type, md5, fileid, remotePerm, filesize,
 ignoredChildrenRemote, contentchecksumtype.name || ':' || contentChecksum,
 hasDirtyPlaceholder
 FROM metadata
 LEFT JOIN checksumtype as contentchecksumtype ON metadata.contentChecksumTypeId == contentchecksumtype.id `

	setFileRecordQuery = `INSERT OR REPLACE INTO metadata 
(phash, path, inode, modtime, type, md5, fileid, remotePerm, filesize, ignoredChildrenRemote, 
contentChecksum, contentChecksumTypeId, hasDirtyPlaceholder) 
VALUES (?1 , ?2, ?3 , ?4 , ?5 , ?6 , ?7,  ?8 , ?9 , ?10, ?11, ?12, ?13);`

	deleteFileRecordQuery = `DELETE FROM metadata WHERE phash=?1`

	getFileRecordQuery = selectFileRecord + `WHERE phash=?1`

	getFileRecordByInode = selectFileRecord + `WHERE inode=?1`

	getFileRecordsByFileId = selectFileRecord + `WHERE fileid=?1`

	listFilesInPathQuery = selectFileRecord + `WHERE parent_hash(path) = ?1 ORDER BY path||'/' ASC`

	getAllFilesQuery = selectFileRecord + `ORDER BY path||'/' ASC`

	getDirtyPlaceholders = selectFileRecord + `WHERE hasDirtyPlaceholder=TRUE`
)

// C++ registers parent_hash as a deterministic SQLite function, see
// syncjournaldb.cpp:286-295:
//
//	const char *end = std::strrchr(text, '/');
//	if (!end) end = text;
//	c_jhash64(text, end - text, 0);
//
// A path with no slash hashes the *empty* string, not itself: a root-level
// file's parent is the sync root. Hashing the whole path instead makes every
// root-level row invisible to listFilesInPath("").
func init() {
	_ = sqlite.RegisterDeterministicScalarFunction("parent_hash", 1,
		func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
			path, ok := args[0].(string)
			if !ok {
				return int64(0), nil
			}
			end := 0
			if i := strings.LastIndex(path, "/"); i >= 0 {
				end = i
			}
			return int64(PHash(path[:end])), nil
		})
}

func cMix64(a, b, c uint64) (uint64, uint64, uint64) {
	a -= b
	a -= c
	a ^= c >> 43
	b -= c
	b -= a
	b ^= a << 9
	c -= a
	c -= b
	c ^= b >> 8
	a -= b
	a -= c
	a ^= c >> 38
	b -= c
	b -= a
	b ^= a << 23
	c -= a
	c -= b
	c ^= b >> 5
	a -= b
	a -= c
	a ^= c >> 35
	b -= c
	b -= a
	b ^= a << 49
	c -= a
	c -= b
	c ^= b >> 11
	a -= b
	a -= c
	a ^= c >> 12
	b -= c
	b -= a
	b ^= a << 18
	c -= a
	c -= b
	c ^= b >> 22
	return a, b, c
}

func cJHash64(k []byte, intval uint64) uint64 {
	length := uint64(len(k))

	a, b := intval, intval
	c := uint64(0x9e3779b97f4a7c13)

	for len(k) >= 24 {
		a += uint64(k[0]) | uint64(k[1])<<8 | uint64(k[2])<<16 | uint64(k[3])<<24 |
			uint64(k[4])<<32 | uint64(k[5])<<40 | uint64(k[6])<<48 | uint64(k[7])<<56
		b += uint64(k[8]) | uint64(k[9])<<8 | uint64(k[10])<<16 | uint64(k[11])<<24 |
			uint64(k[12])<<32 | uint64(k[13])<<40 | uint64(k[14])<<48 | uint64(k[15])<<56
		c += uint64(k[16]) | uint64(k[17])<<8 | uint64(k[18])<<16 | uint64(k[19])<<24 |
			uint64(k[20])<<32 | uint64(k[21])<<40 | uint64(k[22])<<48 | uint64(k[23])<<56
		a, b, c = cMix64(a, b, c)
		k = k[24:]
	}

	c += length
	n := len(k)
	for i := 16; i < n; i++ {
		c += uint64(k[i]) << (8 * uint(i-16+1))
	}
	for i := 8; i < n && i < 16; i++ {
		b += uint64(k[i]) << (8 * uint(i-8))
	}
	for i := 0; i < n && i < 8; i++ {
		a += uint64(k[i]) << (8 * uint(i))
	}
	_, _, c = cMix64(a, b, c)

	return c
}

func PHash(path string) uint64 {
	return cJHash64([]byte(path), 0)
}

type ItemType uint8

const (
	ItemTypeFile        ItemType = 0
	ItemTypeSymLink     ItemType = 1
	ItemTypeDirectory   ItemType = 2
	ItemTypeUnsupported ItemType = 3

	ItemTypeVirtualFile ItemType = 4

	ItemTypeVirtualFileDownload ItemType = 5

	ItemTypeVirtualFileDehydration ItemType = 6
)

func (t ItemType) String() string {
	switch t {
	case ItemTypeFile:
		return "File"
	case ItemTypeSymLink:
		return "SymLink"
	case ItemTypeDirectory:
		return "Directory"
	case ItemTypeUnsupported:
		return "Unsupported"
	case ItemTypeVirtualFile:
		return "VirtualFile"
	case ItemTypeVirtualFileDownload:
		return "VirtualFileDownload"
	case ItemTypeVirtualFileDehydration:
		return "VirtualFileDehydration"
	}
	return "Unknown"
}

type SyncJournalFileRecord struct {
	Path                  string
	Inode                 uint64
	ModTime               int64
	HasModTime            bool
	Type                  ItemType
	Etag                  string
	FileID                string
	FileSize              int64
	RemotePerm            RemotePermissions
	ServerHasIgnoredFiles bool
	HasDirtyPlaceholder   bool
	ChecksumHeader        string

	Err string
}

func NewSyncJournalFileRecord(path string, typ ItemType) SyncJournalFileRecord {
	return SyncJournalFileRecord{Path: path, Type: typ}
}

func SyncJournalFileRecordError(err string) SyncJournalFileRecord {
	return SyncJournalFileRecord{Type: ItemTypeUnsupported, Err: err}
}

func RecordQuery() string { return selectFileRecord }

func (r SyncJournalFileRecord) Validate() error {
	switch {
	case r.RemotePerm.IsNull():
		return fmt.Errorf("record %q: remotePerm is null", r.Path)
	case r.Inode == 0:
		return fmt.Errorf("record %q: inode is 0", r.Path)
	case !r.HasModTime:
		return fmt.Errorf("record %q: no modtime", r.Path)
	case r.FileID == "":
		return fmt.Errorf("record %q: fileId is empty", r.Path)
	case r.Type == ItemTypeUnsupported:
		return fmt.Errorf("record %q: type is unsupported", r.Path)
	}
	return nil
}

func (r SyncJournalFileRecord) IsValid() bool  { return !r.HasError() && r.Path != "" }
func (r SyncJournalFileRecord) HasError() bool { return r.Err != "" }

func (r SyncJournalFileRecord) ModDateTime() time.Time { return time.Unix(r.ModTime, 0).UTC() }

func (r SyncJournalFileRecord) IsDirectory() bool { return r.Type == ItemTypeDirectory }

func (r SyncJournalFileRecord) IsFile() bool {
	return r.Type == ItemTypeFile || r.Type == ItemTypeVirtualFileDehydration
}

func (r SyncJournalFileRecord) IsVirtualFile() bool {
	return r.Type == ItemTypeVirtualFile || r.Type == ItemTypeVirtualFileDownload
}

func (r SyncJournalFileRecord) Name() string {
	return r.Path[strings.LastIndex(r.Path, "/")+1:]
}

func (r SyncJournalFileRecord) String() string {
	if r.HasError() {
		return fmt.Sprintf("SyncJournalFileRecord(Error: %s)", r.Err)
	}
	return fmt.Sprintf("SyncJournalFileRecord(path: %s, inode: %d, modtime: %d, type: %v, "+
		"etag: %s, fileId: %s, remotePerm: %s, size: %d, checksum: %s, "+
		"serverHasIgnoredFiles: %t, hasDirtyPlaceholder: %t)",
		r.Path, r.Inode, r.ModTime, r.Type, r.Etag, r.FileID, r.RemotePerm,
		r.FileSize, r.ChecksumHeader, r.ServerHasIgnoredFiles, r.HasDirtyPlaceholder)
}

func (r SyncJournalFileRecord) Equal(o SyncJournalFileRecord) bool {
	return r.Path == o.Path && r.Inode == o.Inode && r.ModTime == o.ModTime &&
		r.Type == o.Type && r.Etag == o.Etag && r.FileID == o.FileID &&
		r.FileSize == o.FileSize && r.RemotePerm == o.RemotePerm &&
		r.ServerHasIgnoredFiles == o.ServerHasIgnoredFiles &&
		r.ChecksumHeader == o.ChecksumHeader
}

func (j *Journal) FileRecordCount() (int, error) {
	var count int
	err := j.db.QueryRow("SELECT COUNT(*) FROM metadata").Scan(&count)
	return count, err
}

func (j *Journal) AvoidReadFromDbOnNextSync(path string) error {
	const q = `UPDATE metadata SET md5='_invalid_' WHERE ` +
		`(?1 == path OR (?1 > (path||'/') AND ?1 < (path||'0'))) AND type == 2;`
	if _, err := j.db.Exec(q, path); err != nil {
		return err
	}
	j.etagStorageFilter = append(j.etagStorageFilter, path+"/")
	return nil
}

func (j *Journal) ClearEtagStorageFilter() { j.etagStorageFilter = nil }

func (j *Journal) SetFileRecord(record SyncJournalFileRecord) error {
	etag := record.Etag
	if len(j.etagStorageFilter) > 0 {
		prefix := record.Path + "/"
		for _, it := range j.etagStorageFilter {
			if strings.HasPrefix(it, prefix) {
				etag = "_invalid_"
				break
			}
		}
	}

	checksumHeader := ParseChecksumHeader(record.ChecksumHeader)
	contentChecksumTypeID := j.mapChecksumType(checksumHeader.Type())

	if _, err := j.db.Exec(setFileRecordQuery,
		int64(PHash(record.Path)),
		record.Path,
		int64(record.Inode),
		record.ModTime,
		int64(record.Type),
		etag,
		record.FileID,
		record.RemotePerm.ToDbValue(),
		record.FileSize,
		record.ServerHasIgnoredFiles,
		checksumHeader.Checksum(),
		contentChecksumTypeID,
		record.HasDirtyPlaceholder,
	); err != nil {
		return err
	}

	j.metadataTableIsEmpty = false
	return nil
}

func scanFileRecord(row interface{ Scan(...any) error }) (SyncJournalFileRecord, error) {
	var path, etag, fileid, perm, checksum sql.NullString
	var inode, modtime, typ, filesize, ignoredChildren, dirty sql.NullInt64
	if err := row.Scan(&path, &inode, &modtime, &typ, &etag, &fileid, &perm,
		&filesize, &ignoredChildren, &checksum, &dirty); err != nil {
		return SyncJournalFileRecord{}, err
	}
	return SyncJournalFileRecord{
		Path:                  path.String,
		Inode:                 uint64(inode.Int64),
		ModTime:               modtime.Int64,
		HasModTime:            modtime.Valid,
		Type:                  ItemType(typ.Int64),
		Etag:                  etag.String,
		FileID:                fileid.String,
		RemotePerm:            RemotePermissionsFromDbValue(perm.String),
		FileSize:              filesize.Int64,
		ServerHasIgnoredFiles: ignoredChildren.Int64 > 0,
		ChecksumHeader:        checksum.String,
		HasDirtyPlaceholder:   dirty.Int64 != 0,
	}, nil
}

func (j *Journal) queryRecords(query string, args ...any) ([]SyncJournalFileRecord, error) {
	rows, err := j.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SyncJournalFileRecord
	for rows.Next() {
		rec, err := scanFileRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (j *Journal) Records() (map[string]SyncJournalFileRecord, error) {
	out := make(map[string]SyncJournalFileRecord)
	if j.metadataTableIsEmpty {
		return out, nil
	}
	all, err := j.queryRecords(getAllFilesQuery)
	if err != nil {
		return nil, err
	}
	for _, rec := range all {
		if rec.Path == "" {
			continue
		}
		out[rec.Path] = rec
	}
	return out, nil
}

func (j *Journal) FileRecord(filename string) (SyncJournalFileRecord, error) {
	if filepath.IsAbs(filename) {
		return SyncJournalFileRecord{}, fmt.Errorf("FileRecord: %q is absolute", filename)
	}
	if j.metadataTableIsEmpty || filename == "" {
		return SyncJournalFileRecord{}, nil
	}
	rec, err := scanFileRecord(j.db.QueryRow(getFileRecordQuery, int64(PHash(filename))))
	if err == sql.ErrNoRows {
		return SyncJournalFileRecord{}, nil
	}
	return rec, err
}

func (j *Journal) FileRecordByInode(inode uint64) (SyncJournalFileRecord, error) {
	if inode == 0 || j.metadataTableIsEmpty {
		return SyncJournalFileRecord{}, nil
	}
	rec, err := scanFileRecord(j.db.QueryRow(getFileRecordByInode, int64(inode)))
	if err == sql.ErrNoRows {
		return SyncJournalFileRecord{}, nil
	}
	return rec, err
}

func (j *Journal) FileRecordsByFileID(fileID string) ([]SyncJournalFileRecord, error) {
	if fileID == "" || j.metadataTableIsEmpty {
		return nil, nil
	}
	return j.queryRecords(getFileRecordsByFileId, fileID)
}

func (j *Journal) ListFilesInPath(path string) ([]SyncJournalFileRecord, error) {
	if j.metadataTableIsEmpty {
		return nil, nil
	}
	return j.queryRecords(listFilesInPathQuery, int64(PHash(path)))
}

func (j *Journal) DirtyPlaceholders() ([]SyncJournalFileRecord, error) {
	if j.metadataTableIsEmpty {
		return nil, nil
	}
	return j.queryRecords(getDirtyPlaceholders)
}

func (j *Journal) DeleteRecord(path string) error {
	_, err := j.db.Exec(deleteFileRecordQuery, int64(PHash(path)))
	return err
}

type Permission int

const (
	CanWrite             Permission = 1
	CanDelete            Permission = 2
	CanRename            Permission = 3
	CanMove              Permission = 4
	CanAddFile           Permission = 5
	CanAddSubDirectories Permission = 6
	CanReshare           Permission = 7
	IsShared             Permission = 8
	IsMounted            Permission = 9
	IsMountedSub         Permission = 10
	DeprecatedRemoved    Permission = 11

	permissionsCount = DeprecatedRemoved
)

const permissionLetters = " WDNVCKRSMmz"

const remotePermNotNullMask = 0x1

type RemotePermissions struct {
	value uint16
}

func (p *RemotePermissions) fromArray(s string) {
	p.value = remotePermNotNullMask
	for _, r := range s {
		if i := strings.IndexRune(permissionLetters, r); i >= 0 {
			p.value |= 1 << uint(i)
		}
	}
}

func (p RemotePermissions) ToDbValue() string {
	if p.IsNull() {
		return ""
	}
	var b strings.Builder
	for i := 1; i <= int(permissionsCount); i++ {
		if p.value&(1<<uint(i)) != 0 {
			b.WriteByte(permissionLetters[i])
		}
	}
	if b.Len() == 0 {
		return " "
	}
	return b.String()
}

func (p RemotePermissions) String() string { return p.ToDbValue() }

func RemotePermissionsFromDbValue(value string) RemotePermissions {
	if value == "" {
		return RemotePermissions{}
	}
	var perm RemotePermissions
	perm.fromArray(value)
	return perm
}

func RemotePermissionsFromServerString(value string) RemotePermissions {
	var perm RemotePermissions
	perm.fromArray(value)
	return perm
}

func (p RemotePermissions) HasPermission(perm Permission) bool {
	return p.value&(1<<uint(perm)) != 0
}

func (p *RemotePermissions) SetPermission(perm Permission) {
	p.value |= (1 << uint(perm)) | remotePermNotNullMask
}

func (p *RemotePermissions) UnsetPermission(perm Permission) {
	p.value &= ^uint16(1 << uint(perm))
}

func (p RemotePermissions) IsNull() bool { return p.value&remotePermNotNullMask == 0 }
