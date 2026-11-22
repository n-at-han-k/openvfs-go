package directory

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"hash/fnv"
	"io/fs"
	"strings"

	sqlite "modernc.org/sqlite"
)

// Queries copied verbatim from src/libsync/common/syncjournaldb.cpp and
// syncjournalfilerecord.cpp.

const createMetadataTable = `CREATE TABLE IF NOT EXISTS metadata(
phash INTEGER(8),
path VARCHAR(4096),
inode INTEGER,
modtime INTEGER(8),
type INTEGER,
md5 VARCHAR(32),
PRIMARY KEY(phash)
);`

const createDownloadInfoTable = `CREATE TABLE IF NOT EXISTS downloadinfo(
path VARCHAR(4096),
tmpfile VARCHAR(4096),
etag VARCHAR(32),
errorcount INTEGER,
PRIMARY KEY(path)
);`

const createUploadInfoTable = `CREATE TABLE IF NOT EXISTS uploadinfo(
path VARCHAR(4096),
size INTEGER(8),
modtime INTEGER(8),
contentChecksum TEXT,
url TEXT,
PRIMARY KEY(path)
);`

const createBlacklistTable = `CREATE TABLE IF NOT EXISTS blacklist (
path VARCHAR(4096),
lastTryEtag VARCHAR[32],
lastTryModtime INTEGER[8],
retrycount INTEGER,
errorstring VARCHAR[4096],
PRIMARY KEY(path)
);`

const createSelectiveSyncTable = `CREATE TABLE IF NOT EXISTS selectivesync (
path VARCHAR(4096),
type INTEGER
);`

const createChecksumTypeTable = `CREATE TABLE IF NOT EXISTS checksumtype(
id INTEGER PRIMARY KEY,
name TEXT UNIQUE
);`

const createConflictsTable = `CREATE TABLE IF NOT EXISTS conflicts(
path TEXT PRIMARY KEY,
baseFileId TEXT,
baseEtag TEXT,
baseModtime INTEGER
);`

const createVersionTable = `CREATE TABLE IF NOT EXISTS version(
major INTEGER(8),
minor INTEGER(8),
patch INTEGER(8),
custom VARCHAR(256)
);`

// Column additions from updateMetadataTableStructure() / updateErrorBlacklistTableStructure().
var metadataColumns = []struct{ name, ddl string }{
	{"fileid", "ALTER TABLE metadata ADD COLUMN fileid VARCHAR(128);"},
	{"remotePerm", "ALTER TABLE metadata ADD COLUMN remotePerm VARCHAR(128);"},
	{"filesize", "ALTER TABLE metadata ADD COLUMN filesize BIGINT;"},
	{"ignoredChildrenRemote", "ALTER TABLE metadata ADD COLUMN ignoredChildrenRemote INT;"},
	{"contentChecksum", "ALTER TABLE metadata ADD COLUMN contentChecksum TEXT;"},
	{"contentChecksumTypeId", "ALTER TABLE metadata ADD COLUMN contentChecksumTypeId INTEGER;"},
	{"hasDirtyPlaceholder", "ALTER TABLE metadata ADD COLUMN hasDirtyPlaceholder BOOLEAN;"},
}

var blacklistColumns = []struct{ name, ddl string }{
	{"lastTryTime", "ALTER TABLE blacklist ADD COLUMN lastTryTime INTEGER(8);"},
	{"ignoreDuration", "ALTER TABLE blacklist ADD COLUMN ignoreDuration INTEGER(8);"},
	{"renameTarget", "ALTER TABLE blacklist ADD COLUMN renameTarget VARCHAR(4096);"},
	{"errorCategory", "ALTER TABLE blacklist ADD COLUMN errorCategory INTEGER(8);"},
	{"requestId", "ALTER TABLE blacklist ADD COLUMN requestId VARCHAR(36);"},
}

var indexes = []string{
	"CREATE INDEX IF NOT EXISTS metadata_inode ON metadata(inode);",
	"CREATE INDEX IF NOT EXISTS metadata_path ON metadata(path);",
	"CREATE INDEX IF NOT EXISTS metadata_parent ON metadata(parent_hash(path));",
	"CREATE INDEX IF NOT EXISTS blacklist_index ON blacklist(path collate nocase);",
}

// C++ registers parent_hash as a deterministic SQLite function: the hash of
// the substring before the last '/'. See syncjournaldb.cpp:286-295.
func init() {
	_ = sqlite.RegisterDeterministicScalarFunction("parent_hash", 1,
		func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
			path, ok := args[0].(string)
			if !ok {
				return int64(0), nil
			}
			if i := strings.LastIndex(path, "/"); i >= 0 {
				path = path[:i]
			}
			return int64(PHash(path)), nil
		})
}

// Data queries.
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

	deleteFileRecordQuery  = `DELETE FROM metadata WHERE phash=?1`
	getFileRecordQuery     = selectFileRecord + `WHERE phash=?1`
	getFileRecordByInode   = selectFileRecord + `WHERE inode=?1`
	getFileRecordsByFileId = selectFileRecord + `WHERE fileid=?1`
	listFilesInPathQuery   = selectFileRecord + `WHERE parent_hash(path) = ?1 ORDER BY path||'/' ASC`
	getAllFilesQuery       = selectFileRecord + `ORDER BY path||'/' ASC`
	getDirtyPlaceholders   = selectFileRecord + `WHERE hasDirtyPlaceholder=TRUE`

	getDownloadInfoQuery = `SELECT tmpfile, etag, errorcount FROM downloadinfo WHERE path=?1`
	setDownloadInfoQuery = `INSERT OR REPLACE INTO downloadinfo 
(path, tmpfile, etag, errorcount) 
VALUES ( ?1 , ?2, ?3, ?4 )`
	deleteDownloadInfoQuery = `DELETE FROM downloadinfo WHERE path=?1`
	getAllDownloadInfoQuery = `SELECT tmpfile, etag, errorcount, path FROM downloadinfo`

	getUploadInfoQuery    = `SELECT size, modtime, contentChecksum, url FROM uploadinfo WHERE path=?1`
	getAllUploadInfoQuery = `SELECT size, modtime, contentChecksum, path, url FROM uploadinfo`
	setUploadInfoQuery    = `INSERT OR REPLACE INTO uploadinfo 
(path, size, modtime, contentChecksum, url) 
VALUES ( ?1 , ?2, ?3 , ?4 , ?5 )`

	setErrorBlacklistQuery = `INSERT OR REPLACE INTO blacklist 
(path, lastTryEtag, lastTryModtime, retrycount, errorstring, lastTryTime, ignoreDuration, renameTarget, errorCategory, requestId) 
VALUES ( ?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10)`
	getErrorBlacklistQuery    = `SELECT path, lastTryEtag, lastTryModtime, retrycount, errorstring FROM blacklist WHERE path=?1`
	deleteErrorBlacklistQuery = `DELETE FROM blacklist WHERE path = ?`

	setConflictRecordQuery = `INSERT OR REPLACE INTO conflicts 
(path, baseFileId, baseModtime, baseEtag, basePath) 
VALUES (?1, ?2, ?3, ?4, ?5);`
	getConflictRecordQuery = `SELECT baseFileId, baseModtime, baseEtag, basePath FROM conflicts WHERE path=?1;`
	deleteConflictQuery    = `DELETE FROM conflicts WHERE path=?1;`

	setSelectiveSyncQuery = `INSERT INTO selectivesync VALUES (?1, ?2)`
	getSelectiveSyncQuery = `SELECT path FROM selectivesync WHERE type=?1`
	setVersionQuery       = `INSERT INTO version VALUES (?1, ?2, ?3, ?4);`
	getVersionQuery       = `SELECT major, minor, patch, custom FROM version;`
)

// Journal is a thin wrapper. The C++ client serializes access with a QMutex;
// here the sql.DB pool is limited to one connection for the same effect.
type Journal struct {
	db *sql.DB
}

func OpenJournal(path string) (*Journal, error) {
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)

	j := &Journal{db: db}
	if err := j.createTables(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return j, nil
}

func (j *Journal) createTables() error {
	for _, stmt := range []string{
		createMetadataTable,
		createDownloadInfoTable,
		createUploadInfoTable,
		createBlacklistTable,
		createSelectiveSyncTable,
		createChecksumTypeTable,
		createConflictsTable,
		createVersionTable,
	} {
		if _, err := j.db.Exec(stmt); err != nil {
			return fmt.Errorf("%s: %w", stmt, err)
		}
	}

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
	for _, c := range blacklistColumns {
		if _, err := j.addColumnIfMissing("blacklist", c.name, c.ddl); err != nil {
			return err
		}
	}
	if _, err := j.addColumnIfMissing("conflicts", "basePath", "ALTER TABLE conflicts ADD COLUMN basePath TEXT;"); err != nil {
		return err
	}
	for _, stmt := range indexes {
		if _, err := j.db.Exec(stmt); err != nil {
			return fmt.Errorf("%s: %w", stmt, err)
		}
	}
	return nil
}

func (j *Journal) addColumnIfMissing(table, column, ddl string) (bool, error) {
	rows, err := j.db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			return false, err
		}
		if name == column {
			return false, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	if _, err := j.db.Exec(ddl); err != nil {
		return false, fmt.Errorf("%s: %w", ddl, err)
	}
	return true, nil
}

// PHash stands in for the C++ getPHash() (c_jhash64). The queries are the
// same; the hash function still needs porting from common/checksums or utility.
func PHash(path string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(path))
	return h.Sum64()
}

// Records reads the previous state from the metadata table using the C++
// SELECT (SyncJournalFileRecord::query()).
func (j *Journal) Records() (map[string]Meta, error) {
	rows, err := j.db.Query(getAllFilesQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]Meta)
	for rows.Next() {
		var path, etag, fileid, perm, ignoredChildren, checksum sql.NullString
		var inode, modtime, typ, filesize, dirty sql.NullInt64
		if err := rows.Scan(&path, &inode, &modtime, &typ, &etag, &fileid, &perm, &filesize, &ignoredChildren, &checksum, &dirty); err != nil {
			return nil, err
		}
		if !path.Valid {
			continue
		}
		out[path.String] = Meta{Size: filesize.Int64, ModTime: modtime.Int64, Mode: fs.FileMode(typ.Int64)}
	}
	return out, rows.Err()
}

// SetRecord writes a scanned path using the C++ INSERT OR REPLACE query.
func (j *Journal) SetRecord(path string, m Meta) error {
	_, err := j.db.Exec(setFileRecordQuery,
		PHash(path), path, int64(0), m.ModTime, int64(m.Mode),
		"", "", "", m.Size, 0, "", 0, false,
	)
	return err
}

// DeleteRecord removes a path using the C++ phash DELETE query.
func (j *Journal) DeleteRecord(path string) error {
	_, err := j.db.Exec(deleteFileRecordQuery, PHash(path))
	return err
}

// ---- query-backed methods ------------------------------------------------
// Each method below uses one of the SQL constants copied from the C++ client.

// DownloadInfo is SyncJournalDb::DownloadInfo.
type DownloadInfo struct {
	TmpFile    string
	Etag       string
	ErrorCount int
	Valid      bool
}

func (j *Journal) DownloadInfo(path string) (DownloadInfo, error) {
	var out DownloadInfo
	var tmp, etag sql.NullString
	var count sql.NullInt64
	err := j.db.QueryRow(getDownloadInfoQuery, path).Scan(&tmp, &etag, &count)
	if err == sql.ErrNoRows {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	out = DownloadInfo{TmpFile: tmp.String, Etag: etag.String, ErrorCount: int(count.Int64), Valid: true}
	return out, nil
}

func (j *Journal) SetDownloadInfo(path string, info DownloadInfo) error {
	if !info.Valid {
		return j.DeleteDownloadInfo(path)
	}
	_, err := j.db.Exec(setDownloadInfoQuery, path, info.TmpFile, info.Etag, info.ErrorCount)
	return err
}

func (j *Journal) DeleteDownloadInfo(path string) error {
	_, err := j.db.Exec(deleteDownloadInfoQuery, path)
	return err
}

type DownloadInfoWithPath struct {
	DownloadInfo
	Path string
}

func (j *Journal) AllDownloadInfos() ([]DownloadInfoWithPath, error) {
	rows, err := j.db.Query(getAllDownloadInfoQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DownloadInfoWithPath
	for rows.Next() {
		var tmp, etag, path sql.NullString
		var count sql.NullInt64
		if err := rows.Scan(&tmp, &etag, &count, &path); err != nil {
			return nil, err
		}
		out = append(out, DownloadInfoWithPath{
			DownloadInfo: DownloadInfo{TmpFile: tmp.String, Etag: etag.String, ErrorCount: int(count.Int64), Valid: true},
			Path:         path.String,
		})
	}
	return out, rows.Err()
}

// UploadInfo is SyncJournalDb::UploadInfo. The URL is the tus upload URL when
// resuming a TUS transfer.
type UploadInfo struct {
	Size            int64
	ModTime         int64
	ContentChecksum string
	URL             string
	Path            string
	Valid           bool
}

func (j *Journal) UploadInfo(path string) (UploadInfo, error) {
	var out UploadInfo
	var size, modtime sql.NullInt64
	var checksum, url sql.NullString
	err := j.db.QueryRow(getUploadInfoQuery, path).Scan(&size, &modtime, &checksum, &url)
	if err == sql.ErrNoRows {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	out = UploadInfo{
		Size: size.Int64, ModTime: modtime.Int64,
		ContentChecksum: checksum.String, URL: url.String, Path: path, Valid: true,
	}
	return out, nil
}

func (j *Journal) SetUploadInfo(path string, info UploadInfo) error {
	if !info.Valid {
		_, err := j.db.Exec(`DELETE FROM uploadinfo WHERE path=?1`, path)
		return err
	}
	_, err := j.db.Exec(setUploadInfoQuery, path, info.Size, info.ModTime, info.ContentChecksum, info.URL)
	return err
}

func (j *Journal) AllUploadInfos() ([]UploadInfo, error) {
	rows, err := j.db.Query(getAllUploadInfoQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UploadInfo
	for rows.Next() {
		var size, modtime sql.NullInt64
		var checksum, path, url sql.NullString
		if err := rows.Scan(&size, &modtime, &checksum, &path, &url); err != nil {
			return nil, err
		}
		out = append(out, UploadInfo{
			Size: size.Int64, ModTime: modtime.Int64, ContentChecksum: checksum.String,
			Path: path.String, URL: url.String, Valid: true,
		})
	}
	return out, rows.Err()
}

// BlacklistEntry is SyncJournalErrorBlacklistRecord.
type BlacklistEntry struct {
	Path           string
	LastTryEtag    string
	LastTryModtime int64
	RetryCount     int
	ErrorString    string
	LastTryTime    int64
	IgnoreDuration int64
	RenameTarget   string
	ErrorCategory  int
	RequestID      string
}

func (j *Journal) ErrorBlacklistEntry(path string) (BlacklistEntry, bool, error) {
	var out BlacklistEntry
	var etag, errString sql.NullString
	var modtime, lastTry, ignoreDuration sql.NullInt64
	var retries sql.NullInt64
	err := j.db.QueryRow(getErrorBlacklistQuery, path).Scan(&out.Path, &etag, &modtime, &retries, &errString)
	if err == sql.ErrNoRows {
		return out, false, nil
	}
	if err != nil {
		return out, false, err
	}
	out.LastTryEtag, out.LastTryModtime = etag.String, modtime.Int64
	out.RetryCount, out.ErrorString = int(retries.Int64), errString.String
	out.LastTryTime, out.IgnoreDuration = lastTry.Int64, ignoreDuration.Int64
	return out, true, nil
}

func (j *Journal) SetErrorBlacklistEntry(e BlacklistEntry) error {
	_, err := j.db.Exec(setErrorBlacklistQuery,
		e.Path, e.LastTryEtag, e.LastTryModtime, e.RetryCount, e.ErrorString,
		e.LastTryTime, e.IgnoreDuration, e.RenameTarget, e.ErrorCategory, e.RequestID,
	)
	return err
}

func (j *Journal) DeleteErrorBlacklistEntry(path string) error {
	_, err := j.db.Exec(deleteErrorBlacklistQuery, path)
	return err
}

// ConflictRecord is SyncJournalDb::ConflictRecord.
type ConflictRecord struct {
	Path        string
	BaseFileID  string
	BaseEtag    string
	BaseModtime int64
	BasePath    string
}

func (j *Journal) ConflictRecord(path string) (ConflictRecord, bool, error) {
	var out ConflictRecord
	var id, etag, basePath sql.NullString
	var modtime sql.NullInt64
	err := j.db.QueryRow(getConflictRecordQuery, path).Scan(&id, &modtime, &etag, &basePath)
	if err == sql.ErrNoRows {
		return out, false, nil
	}
	if err != nil {
		return out, false, err
	}
	out = ConflictRecord{Path: path, BaseFileID: id.String, BaseEtag: etag.String, BaseModtime: modtime.Int64, BasePath: basePath.String}
	return out, true, nil
}

func (j *Journal) SetConflictRecord(r ConflictRecord) error {
	_, err := j.db.Exec(setConflictRecordQuery, r.Path, r.BaseFileID, r.BaseModtime, r.BaseEtag, r.BasePath)
	return err
}

func (j *Journal) DeleteConflictRecord(path string) error {
	_, err := j.db.Exec(deleteConflictQuery, path)
	return err
}

func (j *Journal) SetSelectiveSync(path string, typ int) error {
	_, err := j.db.Exec(setSelectiveSyncQuery, path, typ)
	return err
}

func (j *Journal) SelectiveSync(typ int) ([]string, error) {
	rows, err := j.db.Query(getSelectiveSyncQuery, typ)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}
		out = append(out, path)
	}
	return out, rows.Err()
}

type Version struct {
	Major, Minor, Patch int
	Custom              string
}

func (j *Journal) Version() (Version, error) {
	var out Version
	var custom sql.NullString
	err := j.db.QueryRow(getVersionQuery).Scan(&out.Major, &out.Minor, &out.Patch, &custom)
	if err == sql.ErrNoRows {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	out.Custom = custom.String
	return out, nil
}

func (j *Journal) SetVersion(v Version) error {
	_, err := j.db.Exec(setVersionQuery, v.Major, v.Minor, v.Patch, v.Custom)
	return err
}

func (j *Journal) Close() error { return j.db.Close() }
