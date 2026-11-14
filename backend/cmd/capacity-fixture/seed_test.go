package main

import (
	"database/sql"
	_ "embed"
)

//go:embed seed.sql
var seedSQL string

func seed(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(seedSQL); err != nil {
		return err
	}
	return tx.Commit()
}
