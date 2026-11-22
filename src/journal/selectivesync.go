package journal

const createSelectiveSyncTable = `CREATE TABLE IF NOT EXISTS selectivesync (
path VARCHAR(4096),
type INTEGER
);`

const (
	setSelectiveSyncQuery = `INSERT INTO selectivesync VALUES (?1, ?2)`

	getSelectiveSyncQuery = `SELECT path FROM selectivesync WHERE type=?1`
)

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
