package journal

import (
	"database/sql"
)

//
// tables.go
// Copyright (C) 2026 nathan <nathan@elitedesk>
//
// Distributed under terms of the MIT license.
//

type Table struct {
	db   *sql.DB
	name string
}

func (t *Table) Insert(data Record) (int64, error) {
}
