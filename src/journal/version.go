package journal

import "database/sql"

const createVersionTable = `CREATE TABLE IF NOT EXISTS version(
major INTEGER(8),
minor INTEGER(8),
patch INTEGER(8),
custom VARCHAR(256)
);`

const (
	setVersionQuery = `INSERT INTO version VALUES (?1, ?2, ?3, ?4);`

	getVersionQuery = `SELECT major, minor, patch, custom FROM version;`
)

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
