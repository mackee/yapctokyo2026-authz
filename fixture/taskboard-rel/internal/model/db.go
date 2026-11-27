package model

import (
	"context"
	"errors"
	"sync"
)

// This file is the in-memory stand-in for a SQL database.
//
//	Conn ~ *sql.DB   (the connection handle)
//	Tx   ~ *sql.Tx   (a transaction)
//	DB   ~ an interface satisfied by both
//
// Only the tables (*_table.go) and the rows (record.go) touch the data; other
// code reads and writes through them. Given a *Conn, a table or row method runs
// in its own transaction; given a *Tx, it runs inside it, so a caller can group
// several calls in one transaction with Conn.Tx. A write transaction works on a
// copy of the data and commits only if it succeeds, so a write is
// all-or-nothing.

// ErrNoRows is returned by Find methods when no row matches (like sql.ErrNoRows).
var ErrNoRows = errors.New("no rows in result set")

// DB is satisfied by *Conn and *Tx.
type DB interface {
	view(ctx context.Context, fn func(x *Tx) error) error
	update(ctx context.Context, fn func(x *Tx) error) error
}

// Conn is the database handle.
type Conn struct {
	mu  sync.Mutex
	cur *tables
}

// Tx is a transaction.
type Tx struct{ t *tables }

// Tx runs fn in one write transaction and commits it if fn returns nil.
func (c *Conn) Tx(ctx context.Context, fn func(x *Tx) error) error { return c.update(ctx, fn) }

func (c *Conn) view(_ context.Context, fn func(x *Tx) error) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return fn(&Tx{t: c.cur})
}

func (c *Conn) update(_ context.Context, fn func(x *Tx) error) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	next := c.cur.clone()
	if err := fn(&Tx{t: next}); err != nil {
		return err
	}
	c.cur = next
	return nil
}

func (x *Tx) view(_ context.Context, fn func(x *Tx) error) error   { return fn(x) }
func (x *Tx) update(_ context.Context, fn func(x *Tx) error) error { return fn(x) }

type tables struct {
	users    map[string]User
	projects map[string]Project
	members  map[string]map[string]string // projectID -> userID -> role
	tasks    map[string]Task              // task IDs are unique across all projects
	nextID   int
}

func (t *tables) clone() *tables {
	n := &tables{
		users:    make(map[string]User, len(t.users)),
		projects: make(map[string]Project, len(t.projects)),
		members:  make(map[string]map[string]string, len(t.members)),
		tasks:    make(map[string]Task, len(t.tasks)),
		nextID:   t.nextID,
	}
	for k, v := range t.users {
		n.users[k] = v
	}
	for k, v := range t.projects {
		n.projects[k] = v
	}
	for p, m := range t.members {
		mm := make(map[string]string, len(m))
		for u, r := range m {
			mm[u] = r
		}
		n.members[p] = mm
	}
	for k, v := range t.tasks {
		n.tasks[k] = v
	}
	return n
}

// Open returns a Conn over freshly seeded demo data.
func Open() *Conn {
	t := &tables{
		users:    map[string]User{},
		projects: map[string]Project{},
		members:  map[string]map[string]string{},
		tasks:    map[string]Task{},
	}
	for _, u := range []User{
		{ID: "ann", Org: "acme"},
		{ID: "ben", Org: "acme"},
		{ID: "cat", Org: "acme"},
		{ID: "dan", Org: "acme"},
		{ID: "eve", Org: "acme", OrgAdmin: true},
		{ID: "hal", Org: "acme"},
		{ID: "ivy", Org: "acme"},
		{ID: "gus", Org: "globex"},
	} {
		t.users[u.ID] = u
	}
	for _, p := range []Project{
		{ID: "web", Org: "acme", Name: "Website"},
		{ID: "ops", Org: "acme", Name: "Operations"},
		{ID: "legacy", Org: "acme", Name: "Legacy", Archived: true},
		{ID: "gx", Org: "globex", Name: "Globex App"},
	} {
		t.projects[p.ID] = p
	}
	t.members = map[string]map[string]string{
		"web":    {"ann": RoleManager, "ben": RoleContributor, "cat": RoleContributor, "dan": RoleViewer, "ivy": RoleManager},
		"ops":    {"ann": RoleViewer, "cat": RoleContributor, "ivy": RoleManager},
		"legacy": {"ann": RoleManager, "ben": RoleContributor},
		"gx":     {"gus": RoleManager},
	}
	for _, r := range []Task{
		{ID: "t1", ProjectID: "web", Title: "Fix header", Status: "open", AssigneeID: "ben"},
		{ID: "t2", ProjectID: "web", Title: "Update footer", Status: "doing", AssigneeID: "cat"},
		{ID: "t3", ProjectID: "web", Title: "Write copy", Status: "open"},
		{ID: "t4", ProjectID: "ops", Title: "Rotate keys", Status: "open", AssigneeID: "cat"},
		{ID: "t5", ProjectID: "legacy", Title: "Old bug", Status: "open", AssigneeID: "ben"},
		{ID: "t6", ProjectID: "gx", Title: "Globex secret plan", Status: "open", AssigneeID: "gus"},
	} {
		t.tasks[r.ID] = r
	}
	t.nextID = 6
	return &Conn{cur: t}
}
