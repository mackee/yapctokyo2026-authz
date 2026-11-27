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
// through an Acquire. Model functions take capabilities (and admitted values
// such as Assignee) as arguments, so a caller cannot perform a write it has not
// been authorized for. All writes go through store.tx, which enforces the
// model invariants before committing.
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
// It can only be obtained from WriteProject.AdmitAssignee (or ManageTask), and
// a task can only hold an Assignee admitted for the task's own project; the
// store rejects any commit that violates this.
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
	st *store
}

// New returns an Authorizer over freshly seeded demo data.
func New() *Authorizer { return &Authorizer{st: newSeededStore()} }

// Authenticate resolves the X-User value. Unknown or missing users are 401.
func (a *Authorizer) Authenticate(userID string) (ActorScope, error) {
	var act Actor
	err := a.st.read(func(s *state) error {
		u, ok := s.users[userID]
		if userID == "" || !ok {
			return deny(ErrUnauthenticated, "unauthenticated")
		}
		act = Actor{id: u.id, org: u.org, orgAdmin: u.orgAdmin}
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
func readable(s *state, act Actor, pid string) (project, string, error) {
	p, ok := s.projects[pid]
	if !ok || p.org != act.org {
		return project{}, "", deny(ErrNotFound, "not found")
	}
	role := s.role(pid, act.id)
	if role == "" && !act.orgAdmin {
		return project{}, "", deny(ErrNotFound, "not found")
	}
	return p, role, nil
}

func (s *projectScope) Read() (ReadProject, error) {
	err := s.as.a.st.read(func(st *state) error {
		_, _, err := readable(st, s.as.actor, s.pid)
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
	err := s.as.a.st.read(func(st *state) error {
		p, r, err := readable(st, s.as.actor, s.pid)
		if err != nil {
			return err
		}
		if !isWriterRole(r) {
			return deny(ErrForbidden, "forbidden")
		}
		if p.archived {
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
	var out []TaskView
	_ = p.ps.as.a.st.read(func(s *state) error { out = s.tasksIn(p.ps.pid); return nil })
	return out
}

type writeProject struct {
	readProject
	role string
}

func (p *writeProject) Role() string { return p.role }

func admit(s *state, pid, userID string) (Assignee, error) {
	if userID == "" {
		return Assignee{project: pid}, nil
	}
	if !isWriterRole(s.role(pid, userID)) {
		return Assignee{}, deny(ErrInvalid, "assignee must be a contributor or manager of the project")
	}
	return Assignee{project: pid, user: userID}, nil
}

func (p *writeProject) AdmitAssignee(userID string) (Assignee, error) {
	var a Assignee
	err := p.ps.as.a.st.read(func(s *state) error {
		var err error
		a, err = admit(s, p.ps.pid, userID)
		return err
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
	err := p.ps.as.a.st.tx(func(s *state) error {
		t := task{id: s.newTaskID(), project: p.ps.pid, title: title, status: "open", assignee: assignee}
		s.tasks[t.id] = t
		v = t.view()
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
func locate(s *state, act Actor, pid, tid string) (project, string, task, error) {
	p, role, err := readable(s, act, pid)
	if err != nil {
		return project{}, "", task{}, err
	}
	t, ok := s.tasks[tid]
	if !ok || t.project != pid {
		return project{}, "", task{}, deny(ErrNotFound, "not found")
	}
	return p, role, t, nil
}

func (s *taskScope) Read() (ReadTask, error) {
	err := s.ps.as.a.st.read(func(st *state) error {
		_, _, _, err := locate(st, s.ps.as.actor, s.ps.pid, s.tid)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &readTask{ts: s}, nil
}

func (s *taskScope) Edit() (EditTask, error) {
	var role string
	err := s.ps.as.a.st.read(func(st *state) error {
		p, r, t, err := locate(st, s.ps.as.actor, s.ps.pid, s.tid)
		if err != nil {
			return err
		}
		if !isWriterRole(r) {
			return deny(ErrForbidden, "forbidden")
		}
		if r == RoleContributor && t.assignee.user != s.ps.as.actor.id {
			return deny(ErrForbidden, "contributors may only modify their own tasks")
		}
		if p.archived {
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
	err := s.ps.as.a.st.read(func(st *state) error {
		p, r, _, err := locate(st, s.ps.as.actor, s.ps.pid, s.tid)
		if err != nil {
			return err
		}
		if r != RoleManager {
			return deny(ErrForbidden, "forbidden")
		}
		if p.archived {
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
	_ = t.ts.ps.as.a.st.read(func(s *state) error { v = s.tasks[t.ts.tid].view(); return nil })
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
	err := t.ts.ps.as.a.st.tx(func(s *state) error {
		cur := s.tasks[t.ts.tid]
		if p.Assignee != nil {
			a, err := admit(s, cur.project, *p.Assignee)
			if err != nil {
				return err
			}
			cur.assignee = a
		}
		if p.Title != nil {
			cur.title = *p.Title
		}
		if p.Status != nil {
			cur.status = *p.Status
		}
		s.tasks[cur.id] = cur
		v = cur.view()
		return nil
	})
	return v, err
}

type manageTask struct{ editTask }

func (t *manageTask) Delete() error {
	return t.ts.ps.as.a.st.tx(func(s *state) error {
		delete(s.tasks, t.ts.tid)
		return nil
	})
}
