package model

import (
	"context"
	"database/sql"
	"fmt"
	"sync/atomic"

	_ "modernc.org/sqlite"
)

// The database is SQLite (modernc.org/sqlite) through database/sql.
//
// Only the tables (*_table.go) and the rows (record.go) run SQL; other code
// reads and writes through them. They take a DB, which is satisfied by both
// *sql.DB and *sql.Tx. A table operation that writes runs in a transaction
// (inTx): given a *sql.DB it begins and commits its own; given a *sql.Tx it
// runs inside it, so a caller can group several calls in one transaction.

// ErrNoRows is returned by Find methods when no row matches.
var ErrNoRows = sql.ErrNoRows

// DB is satisfied by *sql.DB and *sql.Tx.
type DB interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// inTx runs fn in a transaction: db itself if it is already a *sql.Tx,
// otherwise a new transaction that commits if fn returns nil.
func inTx(ctx context.Context, db DB, fn func(tx DB) error) error {
	conn, ok := db.(*sql.DB)
	if !ok {
		return fn(db)
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

const schema = `
CREATE TABLE users (
	id        TEXT PRIMARY KEY,
	org_id    TEXT NOT NULL,
	org_admin INTEGER NOT NULL DEFAULT 0 -- org admins may read every project in their org, but cannot write
);
CREATE TABLE projects (
	id       TEXT PRIMARY KEY,
	org_id   TEXT NOT NULL,
	name     TEXT NOT NULL,
	archived INTEGER NOT NULL DEFAULT 0 -- archived projects are read-only
);
CREATE TABLE memberships (
	project_id TEXT NOT NULL,
	user_id    TEXT NOT NULL,
	role       TEXT NOT NULL,
	PRIMARY KEY (project_id, user_id)
);
CREATE TABLE tasks (
	id         TEXT PRIMARY KEY, -- unique across all projects
	project_id TEXT NOT NULL,
	title      TEXT NOT NULL,
	status     TEXT NOT NULL,
	assignee   TEXT NOT NULL DEFAULT '' -- '' means unassigned
);
CREATE TABLE task_seq (n INTEGER NOT NULL);
`

const seed = `
INSERT INTO users (id, org_id, org_admin) VALUES
	('ann', 'acme', 0), ('ben', 'acme', 0), ('cat', 'acme', 0), ('dan', 'acme', 0),
	('eve', 'acme', 1), ('hal', 'acme', 0), ('ivy', 'acme', 0), ('gus', 'globex', 0);
INSERT INTO projects (id, org_id, name, archived) VALUES
	('web', 'acme', 'Website', 0), ('ops', 'acme', 'Operations', 0),
	('legacy', 'acme', 'Legacy', 1), ('gx', 'globex', 'Globex App', 0);
INSERT INTO memberships (project_id, user_id, role) VALUES
	('web', 'ann', 'manager'), ('web', 'ben', 'contributor'), ('web', 'cat', 'contributor'),
	('web', 'dan', 'viewer'), ('web', 'ivy', 'manager'),
	('ops', 'ann', 'viewer'), ('ops', 'cat', 'contributor'), ('ops', 'ivy', 'manager'),
	('legacy', 'ann', 'manager'), ('legacy', 'ben', 'contributor'),
	('gx', 'gus', 'manager');
INSERT INTO tasks (id, project_id, title, status, assignee) VALUES
	('t1', 'web', 'Fix header', 'open', 'ben'),
	('t2', 'web', 'Update footer', 'doing', 'cat'),
	('t3', 'web', 'Write copy', 'open', ''),
	('t4', 'ops', 'Rotate keys', 'open', 'cat'),
	('t5', 'legacy', 'Old bug', 'open', 'ben'),
	('t6', 'gx', 'Globex secret plan', 'open', 'gus');
INSERT INTO task_seq (n) VALUES (6);
`

var dbSeq atomic.Int64

// Open opens a fresh in-memory SQLite database with the schema and the demo
// seed data. Every call gets its own database.
func Open() (*sql.DB, error) {
	dsn := fmt.Sprintf("file:taskboard%d?mode=memory&cache=shared", dbSeq.Add(1))
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// An in-memory database lives as long as one connection to it is open.
	if _, err := db.Conn(context.Background()); err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}
	if _, err := db.Exec(seed); err != nil {
		return nil, err
	}
	return db, nil
}
