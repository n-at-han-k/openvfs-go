package journal

import (
	"database/sql"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
)

// Package journal is the sync journal: a SQLite database recording what the

// service knows about each file under the sync root.

const (
	createFilesTable = "CREATE TABLE IF NOT EXISTS files (" +
		"path TEXT PRIMARY KEY, " +
		"inode INTEGER, " +
		"modtime INTEGER, " +
		"size INTEGER, " +
		"etag TEXT, " +
		"fileid TEXT)"
	recordFileQuery = "INSERT INTO files (path, inode, modtime, size) VALUES (?, ?, ?, ?) " +
		"ON CONFLICT(path) DO UPDATE SET inode = excluded.inode, " +
		"modtime = excluded.modtime, size = excluded.size"
	// The inode index is what makes a rename cheap to spot: the same inode under
	// a new path is a move, not a delete followed by a create.
	createInodeIndex = "CREATE INDEX IF NOT EXISTS files_inode ON files (inode)"
	pathByInodeQuery = "SELECT path FROM files WHERE inode = ? LIMIT 1"
	forgetFileQuery  = "DELETE FROM files WHERE path = ?"
)

type Journal struct {
	db    *sql.DB
	Files *Table
}

func (self *Journal) Record(path string, inode uint64, modtime int64, size int64) error {
	_, err := self.Files.Exec(recordFileQuery, path, inode, modtime, size)
	return err
}

func (self *Journal) PathByInode(inode uint64) (string, bool) {
	var path string
	if err := self.Files.QueryRow(pathByInodeQuery, inode).Scan(&path); err != nil {
		return "", false
	}
	return path, true
}

func (self *Journal) Forget(path string) error {
	_, err := self.Files.Exec(forgetFileQuery, path)
	return err
}

func (self *Journal) Paths() ([]string, error) {
	rows, err := self.Files.Query("SELECT path FROM files")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var paths []string
	for rows.Next() {
		var path string
		if err = rows.Scan(&path); err != nil {
			return nil, err
		}
		paths = append(paths, path)
	}
	return paths, rows.Err()
}

func (self *Journal) Close() error {
	return self.db.Close()
}

// Open opens the journal at path, creating the file and its tables if they are

// not there yet.

func Open(path string) (*Journal, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	files := &Table{db: db, name: "files", createSQL: createFilesTable}
	if err = files.Create(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err = files.Exec(createInodeIndex); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Journal{db: db, Files: files}, nil
}
