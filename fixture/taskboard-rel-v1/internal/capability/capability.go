// Package capability owns the taskboard data and every access decision.
//
// Handlers never see the raw data. They receive scopes (bound to the
// authenticated actor and the path) and acquire narrowly scoped capabilities
// from them:
//
//	ActorScope   -> Project(pid) ProjectScope
//	ProjectScope -> Read() ReadProject | Write() WriteProject | Task(tid) TaskScope
//	TaskScope    -> Read() ReadTask    | Edit() EditTask      | Manage() ManageTask
//
// Each Acquire applies the access rules (see README) and returns an error that
// maps onto the HTTP convention (401/403/404/409/400). Capabilities are
// interfaces with unexported implementations, so they can only be obtained
// through an Acquire. A capability is the authorized model object: its
// methods perform the writes, each through the aggregate for that operation
// (aggregate.go) on top of Active Record style records (record.go).
package capability

import (
	"errors"
)

// Project roles.
const (
	RoleManager     = "manager"
	RoleContributor = "contributor"
	RoleViewer      = "viewer"
)

var validStatuses = map[string]bool{"open": true, "doing": true, "done": true}

// Error kinds. Handlers map them to HTTP statuses.
var (
	ErrUnauthenticated = errors.New("unauthenticated")     // 401
	ErrNotFound        = errors.New("not found")           // 404 (also used for "not readable")
	ErrForbidden       = errors.New("forbidden")           // 403
	ErrArchived        = errors.New("project is archived") // 409
	ErrInvalid         = errors.New("invalid request")     // 400
)

type denial struct {
	kind error
	msg  string
}

func (d *denial) Error() string { return d.msg }
func (d *denial) Unwrap() error { return d.kind }

func deny(kind error, msg string) error { return &denial{kind: kind, msg: msg} }

// TaskView is the public representation of a task.
type TaskView struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	Assignee  string `json:"assignee"`
}

// Assignee is an assignee that has been admitted for one specific project:
// either nobody, or a user who is a contributor or manager of that project.
// It can only be obtained from WriteProject.AdmitAssignee.
type Assignee struct {
	project string
	user    string
}

// User returns the admitted user ID ("" means unassigned).
func (a Assignee) User() string { return a.user }

// Actor is an authenticated user.
type Actor struct {
	id       string
	org      string
	orgAdmin bool
}

func (a Actor) ID() string { return a.id }

// Authorizer is the only entry point to the data.
type Authorizer struct {
	db *db
}

// New returns an Authorizer over freshly seeded demo data.
func New() *Authorizer { return &Authorizer{db: newSeededDB()} }

// Authenticate resolves the X-User value. Unknown or missing users are 401.
func (a *Authorizer) Authenticate(userID string) (ActorScope, error) {
	var act Actor
	err := a.db.view(func(x *tx) error {
		u, ok := x.findUser(userID)
		if userID == "" || !ok {
			return deny(ErrUnauthenticated, "unauthenticated")
		}
		act = Actor{id: u.ID, org: u.Org, orgAdmin: u.OrgAdmin}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &actorScope{a: a, actor: act}, nil
}

// ---- scopes -----------------------------------------------------------------

// ActorScope is what an authenticated request may start from.
type ActorScope interface {
	Actor() Actor
	Project(projectID string) ProjectScope
}

// ProjectScope is bound to one actor and one project ID. It grants nothing by
// itself; call Read/Write/Task to acquire a capability.
type ProjectScope interface {
	ID() string
	Actor() ActorScope
	Read() (ReadProject, error)
	Write() (WriteProject, error)
	Task(taskID string) TaskScope
}

// TaskScope is bound to one actor, one project ID and one task ID.
type TaskScope interface {
	Read() (ReadTask, error)
	Edit() (EditTask, error)
	Manage() (ManageTask, error)
}

type actorScope struct {
	a     *Authorizer
	actor Actor
}

func (s *actorScope) Actor() Actor { return s.actor }
func (s *actorScope) Project(projectID string) ProjectScope {
	return &projectScope{as: s, pid: projectID}
}

type projectScope struct {
	as  *actorScope
	pid string
}

func (s *projectScope) ID() string        { return s.pid }
func (s *projectScope) Actor() ActorScope { return s.as }
func (s *projectScope) Task(taskID string) TaskScope {
	return &taskScope{ps: s, tid: taskID}
}

// readable applies the read rule: the project exists, is in the actor's org,
// and the actor is a member (any role) or an org admin. Otherwise 404.
func readable(x *tx, act Actor, pid string) (projectRecord, string, error) {
	p, ok := x.findProject(pid)
	if !ok || p.Org != act.org {
		return projectRecord{}, "", deny(ErrNotFound, "not found")
	}
	role := x.role(pid, act.id)
	if role == "" && !act.orgAdmin {
		return projectRecord{}, "", deny(ErrNotFound, "not found")
	}
	return p, role, nil
}

func (s *projectScope) Read() (ReadProject, error) {
	err := s.as.a.db.view(func(x *tx) error {
		_, _, err := readable(x, s.as.actor, s.pid)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &readProject{ps: s}, nil
}

// Write applies the write rule: readable, contributor or manager (else 403),
// not archived (else 409).
func (s *projectScope) Write() (WriteProject, error) {
	var role string
	err := s.as.a.db.view(func(x *tx) error {
		p, r, err := readable(x, s.as.actor, s.pid)
		if err != nil {
			return err
		}
		if !isWriterRole(r) {
			return deny(ErrForbidden, "forbidden")
		}
		if p.Archived {
			return deny(ErrArchived, "project is archived")
		}
		role = r
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &writeProject{readProject: readProject{ps: s}, role: role}, nil
}

// ---- project capabilities ----------------------------------------------------

// ReadProject may list and read the tasks of one project.
type ReadProject interface {
	ID() string
	Tasks() []TaskView
}

// WriteProject may create tasks in one project and admit assignees for it.
type WriteProject interface {
	ReadProject
	Role() string
	// AdmitAssignee returns an Assignee for this project. "" admits nobody.
	// A user who is not a contributor or manager of this project is 400.
	AdmitAssignee(userID string) (Assignee, error)
	// CreateTask creates a task. Contributors may only assign themselves (403).
	CreateTask(title string, assignee Assignee) (TaskView, error)
}

type readProject struct{ ps *projectScope }

func (p *readProject) ID() string { return p.ps.pid }
func (p *readProject) Tasks() []TaskView {
	out := []TaskView{}
	_ = p.ps.as.a.db.view(func(x *tx) error {
		for _, t := range x.tasksIn(p.ps.pid) {
			out = append(out, t.view())
		}
		return nil
	})
	return out
}

type writeProject struct {
	readProject
	role string
}

func (p *writeProject) Role() string { return p.role }

func (p *writeProject) AdmitAssignee(userID string) (Assignee, error) {
	if userID == "" {
		return Assignee{project: p.ps.pid}, nil
	}
	var a Assignee
	err := p.ps.as.a.db.view(func(x *tx) error {
		if !isWriterRole(x.role(p.ps.pid, userID)) {
			return deny(ErrInvalid, "assignee must be a contributor or manager of the project")
		}
		a = Assignee{project: p.ps.pid, user: userID}
		return nil
	})
	return a, err
}

func (p *writeProject) CreateTask(title string, assignee Assignee) (TaskView, error) {
	if title == "" {
		return TaskView{}, deny(ErrInvalid, "invalid request body")
	}
	if assignee.project != p.ps.pid {
		return TaskView{}, deny(ErrInvalid, "assignee was not admitted for this project")
	}
	if p.role == RoleContributor && assignee.user != "" && assignee.user != p.ps.as.actor.id {
		return TaskView{}, deny(ErrForbidden, "contributors may only assign tasks to themselves")
	}
	var v TaskView
	err := p.ps.as.a.db.update(func(x *tx) error {
		agg, err := loadCreateTask(x, p.ps.pid, title, assignee.user)
		if err != nil {
			return err
		}
		if err := agg.validate(); err != nil {
			return err
		}
		v = agg.apply(x)
		return nil
	})
	return v, err
}

// ---- task capabilities ---------------------------------------------------------

// ReadTask may read one task.
type ReadTask interface {
	View() TaskView
}

// TaskPatch is a partial update. Nil fields are left unchanged.
type TaskPatch struct {
	Title    *string
	Status   *string
	Assignee *string // reassigning requires the manager role
}

// EditTask may modify one task: managers any task, contributors only tasks
// assigned to themselves. The project is not archived.
type EditTask interface {
	ReadTask
	// Apply validates the whole patch first and then applies it atomically.
	Apply(p TaskPatch) (TaskView, error)
}

// ManageTask is the manager-only capability for one task.
type ManageTask interface {
	EditTask
	Delete() error
}

type taskScope struct {
	ps  *projectScope
	tid string
}

// locate applies the read rule to the project and checks that the task
// belongs to the project in the path (else 404).
func locate(x *tx, act Actor, pid, tid string) (projectRecord, string, taskRecord, error) {
	p, role, err := readable(x, act, pid)
	if err != nil {
		return projectRecord{}, "", taskRecord{}, err
	}
	t, ok := x.findTask(tid)
	if !ok || t.ProjectID != pid {
		return projectRecord{}, "", taskRecord{}, deny(ErrNotFound, "not found")
	}
	return p, role, t, nil
}

func (s *taskScope) Read() (ReadTask, error) {
	err := s.ps.as.a.db.view(func(x *tx) error {
		_, _, _, err := locate(x, s.ps.as.actor, s.ps.pid, s.tid)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &readTask{ts: s}, nil
}

func (s *taskScope) Edit() (EditTask, error) {
	var role string
	err := s.ps.as.a.db.view(func(x *tx) error {
		p, r, t, err := locate(x, s.ps.as.actor, s.ps.pid, s.tid)
		if err != nil {
			return err
		}
		if !isWriterRole(r) {
			return deny(ErrForbidden, "forbidden")
		}
		if r == RoleContributor && t.AssigneeID != s.ps.as.actor.id {
			return deny(ErrForbidden, "contributors may only modify their own tasks")
		}
		if p.Archived {
			return deny(ErrArchived, "project is archived")
		}
		role = r
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &editTask{readTask: readTask{ts: s}, role: role}, nil
}

func (s *taskScope) Manage() (ManageTask, error) {
	err := s.ps.as.a.db.view(func(x *tx) error {
		p, r, _, err := locate(x, s.ps.as.actor, s.ps.pid, s.tid)
		if err != nil {
			return err
		}
		if r != RoleManager {
			return deny(ErrForbidden, "forbidden")
		}
		if p.Archived {
			return deny(ErrArchived, "project is archived")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &manageTask{editTask{readTask: readTask{ts: s}, role: RoleManager}}, nil
}

type readTask struct{ ts *taskScope }

func (t *readTask) View() TaskView {
	var v TaskView
	_ = t.ts.ps.as.a.db.view(func(x *tx) error {
		r, _ := x.findTask(t.ts.tid)
		v = r.view()
		return nil
	})
	return v
}

type editTask struct {
	readTask
	role string
}

func (t *editTask) Apply(p TaskPatch) (TaskView, error) {
	if p.Title != nil && *p.Title == "" {
		return TaskView{}, deny(ErrInvalid, "title must not be empty")
	}
	if p.Status != nil && !validStatuses[*p.Status] {
		return TaskView{}, deny(ErrInvalid, "invalid status")
	}
	if p.Assignee != nil && t.role != RoleManager {
		return TaskView{}, deny(ErrForbidden, "only managers may reassign tasks")
	}
	var v TaskView
	err := t.ts.ps.as.a.db.update(func(x *tx) error {
		agg, err := loadUpdateTask(x, t.ts.tid, p)
		if err != nil {
			return err
		}
		if err := agg.validate(); err != nil {
			return err
		}
		v = agg.apply(x)
		return nil
	})
	return v, err
}

type manageTask struct{ editTask }

func (t *manageTask) Delete() error {
	return t.ts.ps.as.a.db.update(func(x *tx) error {
		agg, err := loadDeleteTask(x, t.ts.tid)
		if err != nil {
			return err
		}
		if err := agg.validate(); err != nil {
			return err
		}
		agg.apply(x)
		return nil
	})
}
