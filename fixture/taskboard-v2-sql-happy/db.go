package main

import (
	"context"
	"database/sql"
	"fmt"
	"sync/atomic"

	_ "modernc.org/sqlite"
)

type Task struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	Assignee  string `json:"assignee"`
}

// Project roles.
const (
	RoleManager     = "manager"
	RoleContributor = "contributor"
	RoleViewer      = "viewer"
)

var validStatuses = map[string]bool{"open": true, "doing": true, "done": true}

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

// openSeededDB opens a fresh in-memory SQLite database with the schema and the
// demo seed data. Every call gets its own database.
func openSeededDB() (*sql.DB, error) {
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

// newTaskID returns the next task ID ("t7", "t8", ...).
func newTaskID(ctx context.Context, tx *sql.Tx) (string, error) {
	var n int
	if err := tx.QueryRowContext(ctx, `UPDATE task_seq SET n = n + 1 RETURNING n`).Scan(&n); err != nil {
		return "", err
	}
	return fmt.Sprintf("t%d", n), nil
}
