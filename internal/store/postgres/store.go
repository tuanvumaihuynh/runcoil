package postgres

import "github.com/runcoil/runcoil/internal/store/postgres/gen"

// Store keeps the tables of every subject on db.
type Store struct {
	db      DB
	queries gen.Queries
}

// NewStore creates a Store on db.
func NewStore(db DB, queries gen.Queries) *Store {
	return &Store{db: db, queries: queries}
}
