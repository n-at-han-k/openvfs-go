package journal

import "database/sql"

const createDownloadInfoTable = `CREATE TABLE IF NOT EXISTS downloadinfo(
path VARCHAR(4096),
tmpfile VARCHAR(4096),
etag VARCHAR(32),
errorcount INTEGER,
PRIMARY KEY(path)
);`

const (
	getDownloadInfoQuery = `SELECT tmpfile, etag, errorcount FROM downloadinfo WHERE path=?1`

	setDownloadInfoQuery = `INSERT OR REPLACE INTO downloadinfo 
(path, tmpfile, etag, errorcount) 
VALUES ( ?1 , ?2, ?3, ?4 )`

	deleteDownloadInfoQuery = `DELETE FROM downloadinfo WHERE path=?1`

	getAllDownloadInfoQuery = `SELECT tmpfile, etag, errorcount, path FROM downloadinfo`
)

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
