package capability

import (
	"fmt"
	"sort"
	"sync"
)

// This file is the data layer, in Active Record style: one record type per
// table, finders on the transaction, and save/destroy methods on the records.
// It is unexported on purpose: nothing outside this package can read or write
// the raw tables.
//
// A transaction (db.update) works on a copy of the tables and commits only if
// the callback succeeds, so a write is all-or-nothing. The data layer itself
// validates nothing: the rules an operation must keep are checked by that
// operation's aggregate (see aggregate.go).

type userRecord struct {
	ID       string
	Org      string
	OrgAdmin bool // org admins may read every project in their org, but cannot write
}

type projectRecord struct {
	ID       string
	Org      string
	Name     string
	Archived bool // archived projects are read-only
}

type membershipRecord struct {
	ProjectID string
	UserID    string
	Role      string
}

type taskRecord struct {
	ID         string
	ProjectID  string
	Title      string
	Status     string
	AssigneeID string // "" means unassigned
}

type tables struct {
	users    map[string]userRecord
	projects map[string]projectRecord
	members  map[string]map[string]string // projectID -> userID -> role
	tasks    map[string]taskRecord        // task IDs are unique across all projects
	nextID   int
}

func (t *tables) clone() *tables {
	n := &tables{
		users:    make(map[string]userRecord, len(t.users)),
		projects: make(map[string]projectRecord, len(t.projects)),
		members:  make(map[string]map[string]string, len(t.members)),
		tasks:    make(map[string]taskRecord, len(t.tasks)),
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

type db struct {
	mu  sync.Mutex
	cur *tables
}

// tx is the handle that finders and save/destroy run against.
type tx struct{ t *tables }

// view runs fn against the committed tables (read only by convention).
func (d *db) view(fn func(x *tx) error) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return fn(&tx{t: d.cur})
}

// update runs fn against a copy of the tables and commits it only if fn
// succeeds. Partial writes are therefore impossible.
func (d *db) update(fn func(x *tx) error) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	next := d.cur.clone()
	if err := fn(&tx{t: next}); err != nil {
		return err
	}
	d.cur = next
	return nil
}

// ---- finders ------------------------------------------------------------------

func (x *tx) findUser(id string) (userRecord, bool) {
	u, ok := x.t.users[id]
	return u, ok
}

func (x *tx) findProject(id string) (projectRecord, bool) {
	p, ok := x.t.projects[id]
	return p, ok
}

func (x *tx) findMembership(projectID, userID string) (membershipRecord, bool) {
	role, ok := x.t.members[projectID][userID]
	if !ok {
		return membershipRecord{}, false
	}
	return membershipRecord{ProjectID: projectID, UserID: userID, Role: role}, true
}

func (x *tx) membershipsOf(projectID string) []membershipRecord {
	var out []membershipRecord
	for uid, role := range x.t.members[projectID] {
		out = append(out, membershipRecord{ProjectID: projectID, UserID: uid, Role: role})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UserID < out[j].UserID })
	return out
}

// role returns the user's role in the project ("" if not a member).
func (x *tx) role(projectID, userID string) string {
	m, _ := x.findMembership(projectID, userID)
	return m.Role
}

func (x *tx) findTask(id string) (taskRecord, bool) {
	t, ok := x.t.tasks[id]
	return t, ok
}

func (x *tx) tasksIn(projectID string) []taskRecord {
	out := []taskRecord{}
	for _, t := range x.t.tasks {
		if t.ProjectID == projectID {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (x *tx) newTaskID() string {
	x.t.nextID++
	return fmt.Sprintf("t%d", x.t.nextID)
}

// ---- save / destroy ---------------------------------------------------------------

func (r userRecord) save(x *tx)    { x.t.users[r.ID] = r }
func (r userRecord) destroy(x *tx) { delete(x.t.users, r.ID) }

func (r projectRecord) save(x *tx)    { x.t.projects[r.ID] = r }
func (r projectRecord) destroy(x *tx) { delete(x.t.projects, r.ID) }

func (r membershipRecord) save(x *tx) {
	if x.t.members[r.ProjectID] == nil {
		x.t.members[r.ProjectID] = map[string]string{}
	}
	x.t.members[r.ProjectID][r.UserID] = r.Role
}
func (r membershipRecord) destroy(x *tx) { delete(x.t.members[r.ProjectID], r.UserID) }

func (r taskRecord) save(x *tx)    { x.t.tasks[r.ID] = r }
func (r taskRecord) destroy(x *tx) { delete(x.t.tasks, r.ID) }

func (r taskRecord) view() TaskView {
	return TaskView{ID: r.ID, ProjectID: r.ProjectID, Title: r.Title, Status: r.Status, Assignee: r.AssigneeID}
}

func isWriterRole(role string) bool { return role == RoleManager || role == RoleContributor }

func newSeededDB() *db {
	t := &tables{
		users:    map[string]userRecord{},
		projects: map[string]projectRecord{},
		members:  map[string]map[string]string{},
		tasks:    map[string]taskRecord{},
	}
	for _, u := range []userRecord{
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
	for _, p := range []projectRecord{
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
	for _, r := range []taskRecord{
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
	return &db{cur: t}
}
