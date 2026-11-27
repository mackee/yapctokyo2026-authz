package model

import (
	"context"
	"fmt"
)

// Rows, in Active Record style: one type per table, relation methods that
// load related rows through the tables, and row-change methods that save the
// row.
//
// Relation methods are exported and read only. Row-change methods are
// unexported: rows are changed only by table operations that take an
// authorization object (for example TaskTable.Create).

// User is a row of the users table.
type User struct {
	ID       string
	Org      string
	OrgAdmin bool // org admins may read every project in their org, but cannot write
}

// Project is a row of the projects table.
type Project struct {
	ID       string
	Org      string
	Name     string
	Archived bool // archived projects are read-only
}

// Membership is a row of the memberships table.
type Membership struct {
	ProjectID string
	UserID    string
	Role      string
}

// Task is a row of the tasks table. Its JSON form is the API's task.
type Task struct {
	ID         string `json:"id"`
	ProjectID  string `json:"projectId"`
	Title      string `json:"title"`
	Status     string `json:"status"`
	AssigneeID string `json:"assignee"` // "" means unassigned
}

// ---- relations ------------------------------------------------------------------
//
//	user       1--*  membership  *--1  project  1--*  task
//	membership 1--*  task  (by project and assignee: the tasks assigned to that member)

// Memberships returns the user's memberships in all projects.
func (r *User) Memberships(ctx context.Context, db DB) ([]*Membership, error) {
	return NewMembershipTable().ListByUser(ctx, db, r.ID)
}

// AssignedTasks returns every task assigned to the user, in any project.
func (r *User) AssignedTasks(ctx context.Context, db DB) ([]*Task, error) {
	return NewTaskTable().ListByAssignee(ctx, db, r.ID)
}

// Memberships returns the project's members.
func (r *Project) Memberships(ctx context.Context, db DB) ([]*Membership, error) {
	return NewMembershipTable().ListByProject(ctx, db, r.ID)
}

// Membership returns the membership of one user in the project, or ErrNoRows.
func (r *Project) Membership(ctx context.Context, db DB, userID string) (*Membership, error) {
	return NewMembershipTable().Find(ctx, db, r.ID, userID)
}

// Tasks returns the project's tasks.
func (r *Project) Tasks(ctx context.Context, db DB) ([]*Task, error) {
	return NewTaskTable().ListByProject(ctx, db, r.ID)
}

// Project returns the membership's project.
func (r *Membership) Project(ctx context.Context, db DB) (*Project, error) {
	return NewProjectTable().Find(ctx, db, r.ProjectID)
}

// User returns the membership's user.
func (r *Membership) User(ctx context.Context, db DB) (*User, error) {
	return NewUserTable().Find(ctx, db, r.UserID)
}

// AssignedTasks returns the tasks of the membership's project that are
// assigned to the member.
func (r *Membership) AssignedTasks(ctx context.Context, db DB) ([]*Task, error) {
	return NewTaskTable().ListByProjectAndAssignee(ctx, db, r.ProjectID, r.UserID)
}

// Project returns the task's project.
func (r *Task) Project(ctx context.Context, db DB) (*Project, error) {
	return NewProjectTable().Find(ctx, db, r.ProjectID)
}

// AssigneeMembership returns the assignee's membership in the task's project,
// or ErrNoRows when the task is unassigned or the assignee is not a member.
func (r *Task) AssigneeMembership(ctx context.Context, db DB) (*Membership, error) {
	if r.AssigneeID == "" {
		return nil, ErrNoRows
	}
	return NewMembershipTable().Find(ctx, db, r.ProjectID, r.AssigneeID)
}

// ---- row changes ------------------------------------------------------------------
//
// A change to a row is a method on the row object. Each method changes the
// row and saves it.

// newTask builds an unsaved task in the project; call create to insert it.
func (r *Project) newTask(title string) *Task {
	return &Task{ProjectID: r.ID, Title: title, Status: "open"}
}

// create inserts the task with a fresh ID.
func (r *Task) create(ctx context.Context, db DB) error {
	return db.update(ctx, func(x *Tx) error {
		x.t.nextID++
		r.ID = fmt.Sprintf("t%d", x.t.nextID)
		x.t.tasks[r.ID] = *r
		return nil
	})
}

// rename changes the task's title.
func (r *Task) rename(ctx context.Context, db DB, title string) error {
	r.Title = title
	return r.save(ctx, db)
}

// setStatus changes the task's status.
func (r *Task) setStatus(ctx context.Context, db DB, status string) error {
	r.Status = status
	return r.save(ctx, db)
}

// assign makes the member the task's assignee.
func (r *Task) assign(ctx context.Context, db DB, m *Membership) error {
	r.AssigneeID = m.UserID
	return r.save(ctx, db)
}

// unassign clears the task's assignee.
func (r *Task) unassign(ctx context.Context, db DB) error {
	r.AssigneeID = ""
	return r.save(ctx, db)
}

// destroy deletes the task.
func (r *Task) destroy(ctx context.Context, db DB) error {
	return db.update(ctx, func(x *Tx) error {
		delete(x.t.tasks, r.ID)
		return nil
	})
}

func (r *Task) save(ctx context.Context, db DB) error {
	return db.update(ctx, func(x *Tx) error {
		x.t.tasks[r.ID] = *r
		return nil
	})
}

func (r *User) save(ctx context.Context, db DB) error {
	return db.update(ctx, func(x *Tx) error {
		x.t.users[r.ID] = *r
		return nil
	})
}

func (r *Project) save(ctx context.Context, db DB) error {
	return db.update(ctx, func(x *Tx) error {
		x.t.projects[r.ID] = *r
		return nil
	})
}

func (r *Membership) save(ctx context.Context, db DB) error {
	return db.update(ctx, func(x *Tx) error {
		if x.t.members[r.ProjectID] == nil {
			x.t.members[r.ProjectID] = map[string]string{}
		}
		x.t.members[r.ProjectID][r.UserID] = r.Role
		return nil
	})
}
