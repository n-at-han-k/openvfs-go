package journal

import "database/sql"

//

// tables.gob

// Copyright (C) 2026 nathan <nathan@elitedesk>

//

// Distributed under terms of the MIT license.

//

type Table struct {
	db        *sql.DB
	name      string
	createSQL string
}

func (self *Table) Create() error {
	if self.createSQL == "" {
		return nil
	}
	_, err := self.db.Exec(self.createSQL)
	return err
}

func (self *Table) Insert(query string, args ...any) (int64, error) {
	res, err := self.db.Exec(query, args...)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (self *Table) Exec(query string, args ...any) (sql.Result, error) {
	return self.db.Exec(query, args...)
}

func (self *Table) Query(query string, args ...any) (*sql.Rows, error) {
	return self.db.Query(query, args...)
}

func (self *Table) QueryRow(query string, args ...any) *sql.Row {
	return self.db.QueryRow(query, args...)
}
