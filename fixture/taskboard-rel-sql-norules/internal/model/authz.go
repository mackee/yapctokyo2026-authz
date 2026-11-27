package model

import (
	"context"
	"errors"
)

// Authorization objects.
//
// The HTTP layer builds one authorization object per scope in its Transformer
// (NewProjectAuthz for /projects/{pid}, NewTaskAuthz for .../tasks/{tid}) and
// passes it down to the model operations. The object records what the
// accessor may do in that scope, evaluated once when it is built; model
// operations ask it (CanWrite, CanEdit, CanManage, ...) before changing
// anything. The fields are unexported, so an authorization object can only be
// built by NewProjectAuthz / NewTaskAuthz; its zero value grants nothing.

// Actor is an authenticated user.
type Actor struct {
	id       string
	org      string
	orgAdmin bool
}

// ID returns the user ID.
func (a Actor) ID() string { return a.id }

// ProjectAuthz is what the actor may do in one project.
type ProjectAuthz struct {
	actor    Actor
	project  string
	role     string // "" when the actor is not a member (org admins)
	archived bool
}

// Actor returns the authenticated actor.
func (a ProjectAuthz) Actor() Actor { return a.actor }

// ProjectID returns the project the object is bound to.
func (a ProjectAuthz) ProjectID() string { return a.project }

// Role returns the actor's role in the project ("" if not a member).
func (a ProjectAuthz) Role() string { return a.role }

// CanRead reports whether the actor may read the project. NewProjectAuthz only
// returns objects that can read, so this is true for every built object.
func (a ProjectAuthz) CanRead() bool { return a.project != "" }

// CanWrite returns nil if the actor may create tasks in the project:
// contributor or manager (else ErrForbidden) and not archived (else ErrArchived).
func (a ProjectAuthz) CanWrite() error {
	if !a.CanRead() || !isWriterRole(a.role) {
		return deny(ErrForbidden, "forbidden")
	}
	if a.archived {
		return deny(ErrArchived, "project is archived")
	}
	return nil
}

// CanAssign returns nil if the actor may make userID the assignee of a new
// task: contributors may only assign themselves (else ErrForbidden).
func (a ProjectAuthz) CanAssign(userID string) error {
	if a.role == RoleContributor && userID != "" && userID != a.actor.id {
		return deny(ErrForbidden, "contributors may only assign tasks to themselves")
	}
	return nil
}

// TaskAuthz is what the actor may do with one task of one project.
type TaskAuthz struct {
	project    ProjectAuthz
	task       string
	isAssignee bool // the task is assigned to the actor
}

// Project returns the project-level authorization the task object came from.
func (a TaskAuthz) Project() ProjectAuthz { return a.project }

// TaskID returns the task the object is bound to.
func (a TaskAuthz) TaskID() string { return a.task }

// CanRead reports whether the actor may read the task. NewTaskAuthz only returns
// objects that can read, so this is true for every built object.
func (a TaskAuthz) CanRead() bool { return a.task != "" && a.project.CanRead() }

// CanEdit returns nil if the actor may modify the task: managers any task,
// contributors only tasks assigned to themselves (else ErrForbidden); the
// project is not archived (else ErrArchived).
func (a TaskAuthz) CanEdit() error {
	if !a.CanRead() || !isWriterRole(a.project.role) {
		return deny(ErrForbidden, "forbidden")
	}
	if a.project.role == RoleContributor && !a.isAssignee {
		return deny(ErrForbidden, "contributors may only modify their own tasks")
	}
	if a.project.archived {
		return deny(ErrArchived, "project is archived")
	}
	return nil
}

// CanReassign returns nil if the actor may change the task's assignee:
// managers only (else ErrForbidden). Call it after CanEdit.
func (a TaskAuthz) CanReassign() error {
	if a.project.role != RoleManager {
		return deny(ErrForbidden, "only managers may reassign tasks")
	}
	return nil
}

// CanManage returns nil if the actor may delete the task: managers only (else
// ErrForbidden) and not archived (else ErrArchived).
func (a TaskAuthz) CanManage() error {
	if !a.CanRead() || a.project.role != RoleManager {
		return deny(ErrForbidden, "forbidden")
	}
	if a.project.archived {
		return deny(ErrArchived, "project is archived")
	}
	return nil
}

// ---- building authorization objects -------------------------------------------------

// Authenticate resolves the X-User value. Unknown or missing users are 401.
func Authenticate(ctx context.Context, db DB, userID string) (Actor, error) {
	if userID == "" {
		return Actor{}, deny(ErrUnauthenticated, "unauthenticated")
	}
	u, err := NewUserTable().Find(ctx, db, userID)
	if errors.Is(err, ErrNoRows) {
		return Actor{}, deny(ErrUnauthenticated, "unauthenticated")
	} else if err != nil {
		return Actor{}, err
	}
	return Actor{id: u.ID, org: u.Org, orgAdmin: u.OrgAdmin}, nil
}

// NewProjectAuthz builds the actor's authorization object for one project.
// The project must exist, be in the actor's org, and be readable by the actor
// (a member with any role, or an org admin); otherwise ErrNotFound.
func NewProjectAuthz(ctx context.Context, db DB, act Actor, projectID string) (ProjectAuthz, error) {
	p, err := NewProjectTable().Find(ctx, db, projectID)
	if errors.Is(err, ErrNoRows) {
		return ProjectAuthz{}, deny(ErrNotFound, "not found")
	} else if err != nil {
		return ProjectAuthz{}, err
	}
	if p.Org != act.org {
		return ProjectAuthz{}, deny(ErrNotFound, "not found")
	}
	role := ""
	m, err := p.Membership(ctx, db, act.id)
	if err == nil {
		role = m.Role
	} else if !errors.Is(err, ErrNoRows) {
		return ProjectAuthz{}, err
	}
	if role == "" && !act.orgAdmin {
		return ProjectAuthz{}, deny(ErrNotFound, "not found")
	}
	return ProjectAuthz{actor: act, project: p.ID, role: role, archived: p.Archived}, nil
}

// NewTaskAuthz builds the actor's authorization object for one task of the
// project. The task must belong to the project; otherwise ErrNotFound.
func NewTaskAuthz(ctx context.Context, db DB, pa ProjectAuthz, taskID string) (TaskAuthz, error) {
	if !pa.CanRead() {
		return TaskAuthz{}, deny(ErrNotFound, "not found")
	}
	t, err := NewTaskTable().Find(ctx, db, taskID)
	if errors.Is(err, ErrNoRows) {
		return TaskAuthz{}, deny(ErrNotFound, "not found")
	} else if err != nil {
		return TaskAuthz{}, err
	}
	if t.ProjectID != pa.project {
		return TaskAuthz{}, deny(ErrNotFound, "not found")
	}
	return TaskAuthz{project: pa, task: t.ID, isAssignee: t.AssigneeID == pa.actor.id}, nil
}
