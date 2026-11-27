package model

import "context"

// TaskTable is the tasks table.
//
// Find and List* read rows. The operations List, Create, Get, Update and
// Delete serve the API: each takes the database (a *Conn or a *Tx), the
// authorization object of its scope, and an input; it asks the authorization
// object whether the actor may perform it, then runs the operation's
// aggregate (aggregate.go) in a transaction.
type TaskTable struct{}

// NewTaskTable returns the tasks table.
func NewTaskTable() *TaskTable { return &TaskTable{} }

// ---- reads ----------------------------------------------------------------------------

// Find returns the task with the ID, or ErrNoRows.
func (*TaskTable) Find(ctx context.Context, db DB, id string) (*Task, error) {
	var t Task
	if err := db.QueryRowContext(ctx,
		`SELECT id, project_id, title, status, assignee FROM tasks WHERE id = ?`, id).
		Scan(&t.ID, &t.ProjectID, &t.Title, &t.Status, &t.AssigneeID); err != nil {
		return nil, err
	}
	return &t, nil
}

// ListByProject returns the tasks of the project, ordered by ID.
func (tt *TaskTable) ListByProject(ctx context.Context, db DB, projectID string) ([]*Task, error) {
	return tt.list(ctx, db,
		`SELECT id, project_id, title, status, assignee FROM tasks WHERE project_id = ? ORDER BY id`, projectID)
}

// ListByAssignee returns the tasks assigned to the user in any project,
// ordered by ID.
func (tt *TaskTable) ListByAssignee(ctx context.Context, db DB, userID string) ([]*Task, error) {
	return tt.list(ctx, db,
		`SELECT id, project_id, title, status, assignee FROM tasks WHERE assignee = ? ORDER BY id`, userID)
}

// ListByProjectAndAssignee returns the tasks of the project assigned to the
// user, ordered by ID.
func (tt *TaskTable) ListByProjectAndAssignee(ctx context.Context, db DB, projectID, userID string) ([]*Task, error) {
	return tt.list(ctx, db,
		`SELECT id, project_id, title, status, assignee FROM tasks WHERE project_id = ? AND assignee = ? ORDER BY id`,
		projectID, userID)
}

func (*TaskTable) list(ctx context.Context, db DB, query string, args ...any) ([]*Task, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Task{}
	for rows.Next() {
		var t Task
		if err := rows.Scan(&t.ID, &t.ProjectID, &t.Title, &t.Status, &t.AssigneeID); err != nil {
			return nil, err
		}
		out = append(out, &t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// ---- operations -----------------------------------------------------------------------

// List returns the tasks of the project.
func (tt *TaskTable) List(ctx context.Context, db DB, authz ProjectAuthz) ([]*Task, error) {
	if !authz.CanRead() {
		return nil, deny(ErrNotFound, "not found")
	}
	return tt.ListByProject(ctx, db, authz.ProjectID())
}

// TaskTableCreateInput is the input of TaskTable.Create.
type TaskTableCreateInput struct {
	Title      string
	AssigneeID string // "" means unassigned
}

// Create creates a task in the project.
func (*TaskTable) Create(ctx context.Context, db DB, authz ProjectAuthz, in TaskTableCreateInput) (*Task, error) {
	if err := authz.CanWrite(); err != nil {
		return nil, err
	}
	if in.Title == "" {
		return nil, deny(ErrInvalid, "title must not be empty")
	}
	var t *Task
	if err := inTx(ctx, db, func(tx DB) error {
		agg, err := loadCreateTask(ctx, tx, authz.ProjectID(), in)
		if err != nil {
			return err
		}
		if err := agg.validate(); err != nil {
			return err
		}
		if err := authz.CanAssign(in.AssigneeID); err != nil {
			return err
		}
		if t, err = agg.apply(ctx, tx); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return t, nil
}

// Get returns the task.
func (tt *TaskTable) Get(ctx context.Context, db DB, authz TaskAuthz) (*Task, error) {
	if !authz.CanRead() {
		return nil, deny(ErrNotFound, "not found")
	}
	return tt.Find(ctx, db, authz.TaskID())
}

// TaskTableUpdateInput is the input of TaskTable.Update. Nil fields are left
// unchanged.
type TaskTableUpdateInput struct {
	Title    *string
	Status   *string
	Assignee *string // "" unassigns; changing it requires CanReassign
}

var validStatuses = map[string]bool{"open": true, "doing": true, "done": true}

// Update applies a partial update to the task, all or nothing.
func (*TaskTable) Update(ctx context.Context, db DB, authz TaskAuthz, in TaskTableUpdateInput) (*Task, error) {
	if err := authz.CanEdit(); err != nil {
		return nil, err
	}
	if in.Title != nil && *in.Title == "" {
		return nil, deny(ErrInvalid, "title must not be empty")
	}
	if in.Status != nil && !validStatuses[*in.Status] {
		return nil, deny(ErrInvalid, "invalid status")
	}
	if in.Assignee != nil {
		if err := authz.CanReassign(); err != nil {
			return nil, err
		}
	}
	var t *Task
	if err := inTx(ctx, db, func(tx DB) error {
		agg, err := loadUpdateTask(ctx, tx, authz.TaskID(), in)
		if err != nil {
			return err
		}
		if err := agg.validate(); err != nil {
			return err
		}
		if t, err = agg.apply(ctx, tx); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return t, nil
}

// Delete deletes the task.
func (*TaskTable) Delete(ctx context.Context, db DB, authz TaskAuthz) error {
	if err := authz.CanManage(); err != nil {
		return err
	}
	return inTx(ctx, db, func(tx DB) error {
		agg, err := loadDeleteTask(ctx, tx, authz.TaskID())
		if err != nil {
			return err
		}
		if err := agg.validate(); err != nil {
			return err
		}
		return agg.apply(ctx, tx)
	})
}
