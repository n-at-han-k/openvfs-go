package journal

import (
	"database/sql"
	"fmt"
)

type Journal struct {
	db *sql.DB

	etagStorageFilter []string

	checksumTypeCache map[ChecksumAlgorithm]int

	metadataTableIsEmpty bool
}

func OpenJournal(path string) (*Journal, error) {
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)

	j := &Journal{db: db, checksumTypeCache: make(map[ChecksumAlgorithm]int)}
	if err := j.createTables(); err != nil {
		_ = db.Close()
		return nil, err
	}
	count, err := j.FileRecordCount()
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	j.metadataTableIsEmpty = count == 0
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

	if err := j.updateMetadataTableStructure(); err != nil {
		return err
	}
	if err := j.updateErrorBlacklistTableStructure(); err != nil {
		return err
	}
	if _, err := j.addColumnIfMissing("conflicts", "basePath", addConflictsBasePath); err != nil {
		return err
	}

	for _, stmt := range [][]string{metadataIndexes, blacklistIndexes} {
		for _, index := range stmt {
			if _, err := j.db.Exec(index); err != nil {
				return fmt.Errorf("%s: %w", index, err)
			}
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

func (j *Journal) Close() error { return j.db.Close() }
