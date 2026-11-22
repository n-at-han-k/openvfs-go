package journal

import "database/sql"

const createBlacklistTable = `CREATE TABLE IF NOT EXISTS blacklist (
path VARCHAR(4096),
lastTryEtag VARCHAR[32],
lastTryModtime INTEGER[8],
retrycount INTEGER,
errorstring VARCHAR[4096],
PRIMARY KEY(path)
);`

var blacklistColumns = []struct{ name, ddl string }{
	{"lastTryTime", "ALTER TABLE blacklist ADD COLUMN lastTryTime INTEGER(8);"},
	{"ignoreDuration", "ALTER TABLE blacklist ADD COLUMN ignoreDuration INTEGER(8);"},
	{"renameTarget", "ALTER TABLE blacklist ADD COLUMN renameTarget VARCHAR(4096);"},
	{"errorCategory", "ALTER TABLE blacklist ADD COLUMN errorCategory INTEGER(8);"},
	{"requestId", "ALTER TABLE blacklist ADD COLUMN requestId VARCHAR(36);"},
}

var blacklistIndexes = []string{
	"CREATE INDEX IF NOT EXISTS blacklist_index ON blacklist(path collate nocase);",
}

func (j *Journal) updateErrorBlacklistTableStructure() error {
	for _, c := range blacklistColumns {
		if _, err := j.addColumnIfMissing("blacklist", c.name, c.ddl); err != nil {
			return err
		}
	}
	return nil
}

const (
	setErrorBlacklistQuery = `INSERT OR REPLACE INTO blacklist 
(path, lastTryEtag, lastTryModtime, retrycount, errorstring, lastTryTime, ignoreDuration, renameTarget, errorCategory, requestId) 
VALUES ( ?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10)`

	getErrorBlacklistQuery = `SELECT path, lastTryEtag, lastTryModtime, retrycount, errorstring FROM blacklist WHERE path=?1`

	deleteErrorBlacklistQuery = `DELETE FROM blacklist WHERE path = ?`
)

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
	_, err := j.db.Exec(
		setErrorBlacklistQuery,
		e.Path,
		e.LastTryEtag,
		e.LastTryModtime,
		e.RetryCount,
		e.ErrorString,
		e.LastTryTime,
		e.IgnoreDuration,
		e.RenameTarget,
		e.ErrorCategory,
		e.RequestID,
	)
	return err
}

func (j *Journal) DeleteErrorBlacklistEntry(path string) error {
	_, err := j.db.Exec(deleteErrorBlacklistQuery, path)
	return err
}
