package state

import "database/sql"

func runStateTransaction[T any](database *sql.DB, apply func(*sql.Tx) error, read func(*sql.Tx) (T, error)) (T, error) {
	var zero T
	transaction, err := database.Begin()
	if err != nil {
		return zero, err
	}
	if err := apply(transaction); err != nil {
		_ = transaction.Rollback()
		return zero, err
	}
	return commitStateRead(transaction, read)
}

func commitStateRead[T any](transaction *sql.Tx, read func(*sql.Tx) (T, error)) (T, error) {
	var zero T
	value, err := read(transaction)
	if err != nil {
		_ = transaction.Rollback()
		return zero, err
	}
	if err := transaction.Commit(); err != nil {
		return zero, err
	}
	return value, nil
}
