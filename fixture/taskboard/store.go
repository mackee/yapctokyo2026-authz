package main

import (
	"fmt"
	"sort"
	"sync"
)

type User struct {
	ID       string
	OrgID    string
	OrgAdmin bool // org admins may read every project in their org, but cannot write
}

type Project struct {
	ID       string
	OrgID    string
	Name     string
	Archived bool // archived projects are read-only
}

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

type Store struct {
	mu       sync.Mutex
	users    map[string]*User
	projects map[string]*Project
	members  map[string]map[string]string // projectID -> userID -> role
	tasks    map[string]*Task             // task IDs are unique across all projects
	nextID   int
}

func (s *Store) User(id string) *User { return s.users[id] }

func (s *Store) Project(id string) *Project { return s.projects[id] }

func (s *Store) Role(projectID, userID string) string { return s.members[projectID][userID] }

func (s *Store) Task(id string) *Task { return s.tasks[id] }

func (s *Store) TasksInProject(projectID string) []*Task {
	var out []*Task
	for _, t := range s.tasks {
		if t.ProjectID == projectID {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (s *Store) NewTaskID() string {
	s.nextID++
	return fmt.Sprintf("t%d", s.nextID)
}

func (s *Store) PutTask(t *Task) { s.tasks[t.ID] = t }

func (s *Store) DeleteTask(id string) { delete(s.tasks, id) }

func newSeededStore() *Store {
	s := &Store{
		users:    map[string]*User{},
		projects: map[string]*Project{},
		members:  map[string]map[string]string{},
		tasks:    map[string]*Task{},
	}
	for _, u := range []*User{
		{ID: "ann", OrgID: "acme"},
		{ID: "ben", OrgID: "acme"},
		{ID: "cat", OrgID: "acme"},
		{ID: "dan", OrgID: "acme"},
		{ID: "eve", OrgID: "acme", OrgAdmin: true},
		{ID: "hal", OrgID: "acme"},
		{ID: "gus", OrgID: "globex"},
	} {
		s.users[u.ID] = u
	}
	for _, p := range []*Project{
		{ID: "web", OrgID: "acme", Name: "Website"},
		{ID: "ops", OrgID: "acme", Name: "Operations"},
		{ID: "legacy", OrgID: "acme", Name: "Legacy", Archived: true},
		{ID: "gx", OrgID: "globex", Name: "Globex App"},
	} {
		s.projects[p.ID] = p
	}
	s.members = map[string]map[string]string{
		"web":    {"ann": RoleManager, "ben": RoleContributor, "cat": RoleContributor, "dan": RoleViewer},
		"ops":    {"ann": RoleViewer, "cat": RoleContributor},
		"legacy": {"ann": RoleManager, "ben": RoleContributor},
		"gx":     {"gus": RoleManager},
	}
	for _, t := range []*Task{
		{ID: "t1", ProjectID: "web", Title: "Fix header", Status: "open", Assignee: "ben"},
		{ID: "t2", ProjectID: "web", Title: "Update footer", Status: "doing", Assignee: "cat"},
		{ID: "t3", ProjectID: "web", Title: "Write copy", Status: "open", Assignee: ""},
		{ID: "t4", ProjectID: "ops", Title: "Rotate keys", Status: "open", Assignee: "cat"},
		{ID: "t5", ProjectID: "legacy", Title: "Old bug", Status: "open", Assignee: "ben"},
		{ID: "t6", ProjectID: "gx", Title: "Globex secret plan", Status: "open", Assignee: "gus"},
	} {
		s.tasks[t.ID] = t
	}
	s.nextID = 6
	return s
}
