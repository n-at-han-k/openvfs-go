package journal

import "database/sql"

const createUploadInfoTable = `CREATE TABLE IF NOT EXISTS uploadinfo(
path VARCHAR(4096),
size INTEGER(8),
modtime INTEGER(8),
contentChecksum TEXT,
url TEXT,
PRIMARY KEY(path)
);`

const (
	getUploadInfoQuery = `SELECT size, modtime, contentChecksum, url FROM uploadinfo WHERE path=?1`

	getAllUploadInfoQuery = `SELECT size, modtime, contentChecksum, path, url FROM uploadinfo`

	setUploadInfoQuery = `INSERT OR REPLACE INTO uploadinfo 
(path, size, modtime, contentChecksum, url) 
VALUES ( ?1 , ?2, ?3 , ?4 , ?5 )`
)

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
