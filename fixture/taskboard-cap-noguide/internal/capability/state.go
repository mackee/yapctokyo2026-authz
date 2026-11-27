package capability

import (
	"fmt"
	"sort"
	"sync"
)

// This file is the data layer. It is unexported on purpose: nothing outside
// this package can read or write the raw state. Every mutation goes through
// store.tx, which works on a copy and commits only if the model invariants
// still hold, so a write is all-or-nothing and can never leave the data in a
// state that breaks the access rules.

type user struct {
	id       string
	org      string
	orgAdmin bool // org admins may read every project in their org, but cannot write
}

type project struct {
	id       string
	org      string
	name     string
	archived bool // archived projects are read-only
}

type task struct {
	id       string
	project  string
	title    string
	status   string
	assignee Assignee // bound to task.project; see Assignee
}

type state struct {
	users    map[string]user
	projects map[string]project
	members  map[string]map[string]string // projectID -> userID -> role
	tasks    map[string]task              // task IDs are unique across all projects
	nextID   int
}

func (s *state) role(projectID, userID string) string { return s.members[projectID][userID] }

func isWriterRole(role string) bool { return role == RoleManager || role == RoleContributor }

func (s *state) clone() *state {
	n := &state{
		users:    make(map[string]user, len(s.users)),
		projects: make(map[string]project, len(s.projects)),
		members:  make(map[string]map[string]string, len(s.members)),
		tasks:    make(map[string]task, len(s.tasks)),
		nextID:   s.nextID,
	}
	for k, v := range s.users {
		n.users[k] = v
	}
	for k, v := range s.projects {
		n.projects[k] = v
	}
	for p, m := range s.members {
		mm := make(map[string]string, len(m))
		for u, r := range m {
			mm[u] = r
		}
		n.members[p] = mm
	}
	for k, v := range s.tasks {
		n.tasks[k] = v
	}
	return n
}

// check enforces the model invariants. It runs before every commit.
//
//   - every task belongs to an existing project
//   - a task's Assignee is bound to the task's own project
//   - an assignee is always a contributor or manager of the task's project
//   - project members are users of the project's organization, with a valid role
func (s *state) check() error {
	for pid, m := range s.members {
		p, ok := s.projects[pid]
		if !ok {
			return deny(ErrInvalid, fmt.Sprintf("members of unknown project %s", pid))
		}
		for uid, role := range m {
			u, ok := s.users[uid]
			if !ok || u.org != p.org {
				return deny(ErrInvalid, fmt.Sprintf("project %s: member %s is not a user of the project's organization", pid, uid))
			}
			if role != RoleManager && role != RoleContributor && role != RoleViewer {
				return deny(ErrInvalid, fmt.Sprintf("project %s: invalid role for %s", pid, uid))
			}
		}
	}
	for _, t := range s.tasks {
		p, ok := s.projects[t.project]
		if !ok {
			return deny(ErrInvalid, fmt.Sprintf("task %s belongs to an unknown project", t.id))
		}
		if t.assignee.project != t.project {
			return deny(ErrInvalid, fmt.Sprintf("task %s: assignee was admitted for another project", t.id))
		}
		if u := t.assignee.user; u != "" {
			if !isWriterRole(s.role(p.id, u)) {
				return deny(ErrInvalid, fmt.Sprintf("task %s: assignee must be a contributor or manager of the project", t.id))
			}
		}
	}
	return nil
}

func (s *state) tasksIn(projectID string) []TaskView {
	out := []TaskView{}
	for _, t := range s.tasks {
		if t.project == projectID {
			out = append(out, t.view())
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (s *state) newTaskID() string {
	s.nextID++
	return fmt.Sprintf("t%d", s.nextID)
}

func (t task) view() TaskView {
	return TaskView{ID: t.id, ProjectID: t.project, Title: t.title, Status: t.status, Assignee: t.assignee.user}
}

type store struct {
	mu  sync.Mutex
	cur *state
}

// read runs fn against the committed state.
func (st *store) read(fn func(s *state) error) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	return fn(st.cur)
}

// tx runs fn against a copy of the state and commits it only if fn succeeds
// and the invariants hold. Partial writes are therefore impossible.
func (st *store) tx(fn func(s *state) error) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	next := st.cur.clone()
	if err := fn(next); err != nil {
		return err
	}
	if err := next.check(); err != nil {
		return err
	}
	st.cur = next
	return nil
}

func newSeededStore() *store {
	s := &state{
		users:    map[string]user{},
		projects: map[string]project{},
		members:  map[string]map[string]string{},
		tasks:    map[string]task{},
	}
	for _, u := range []user{
		{id: "ann", org: "acme"},
		{id: "ben", org: "acme"},
		{id: "cat", org: "acme"},
		{id: "dan", org: "acme"},
		{id: "eve", org: "acme", orgAdmin: true},
		{id: "hal", org: "acme"},
		{id: "ivy", org: "acme"},
		{id: "gus", org: "globex"},
	} {
		s.users[u.id] = u
	}
	for _, p := range []project{
		{id: "web", org: "acme", name: "Website"},
		{id: "ops", org: "acme", name: "Operations"},
		{id: "legacy", org: "acme", name: "Legacy", archived: true},
		{id: "gx", org: "globex", name: "Globex App"},
	} {
		s.projects[p.id] = p
	}
	s.members = map[string]map[string]string{
		"web":    {"ann": RoleManager, "ben": RoleContributor, "cat": RoleContributor, "dan": RoleViewer, "ivy": RoleManager},
		"ops":    {"ann": RoleViewer, "cat": RoleContributor, "ivy": RoleManager},
		"legacy": {"ann": RoleManager, "ben": RoleContributor},
		"gx":     {"gus": RoleManager},
	}
	for _, t := range []task{
		{id: "t1", project: "web", title: "Fix header", status: "open", assignee: Assignee{project: "web", user: "ben"}},
		{id: "t2", project: "web", title: "Update footer", status: "doing", assignee: Assignee{project: "web", user: "cat"}},
		{id: "t3", project: "web", title: "Write copy", status: "open", assignee: Assignee{project: "web"}},
		{id: "t4", project: "ops", title: "Rotate keys", status: "open", assignee: Assignee{project: "ops", user: "cat"}},
		{id: "t5", project: "legacy", title: "Old bug", status: "open", assignee: Assignee{project: "legacy", user: "ben"}},
		{id: "t6", project: "gx", title: "Globex secret plan", status: "open", assignee: Assignee{project: "gx", user: "gus"}},
	} {
		s.tasks[t.id] = t
	}
	s.nextID = 6
	if err := s.check(); err != nil {
		panic(err)
	}
	return &store{cur: s}
}
