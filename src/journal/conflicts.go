package journal

import "database/sql"

const createConflictsTable = `CREATE TABLE IF NOT EXISTS conflicts(
path TEXT PRIMARY KEY,
baseFileId TEXT,
baseEtag TEXT,
baseModtime INTEGER
);`

const addConflictsBasePath = "ALTER TABLE conflicts ADD COLUMN basePath TEXT;"

const (
	setConflictRecordQuery = `INSERT OR REPLACE INTO conflicts 
(path, baseFileId, baseModtime, baseEtag, basePath) 
VALUES (?1, ?2, ?3, ?4, ?5);`

	getConflictRecordQuery = `SELECT baseFileId, baseModtime, baseEtag, basePath FROM conflicts WHERE path=?1;`

	deleteConflictQuery = `DELETE FROM conflicts WHERE path=?1;`
)

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
