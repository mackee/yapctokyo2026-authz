package model

import (
	"context"
	"errors"
)

// Aggregates: one per operation. An aggregate is built inside the transaction
// for a single operation, starting from the row the operation is about (found
// through its table) and loading the related rows through the relation
// methods (record.go). It validates the model rules on those rows, and applies
// the change through the row-change methods. There is no global check at
// commit time.
//
// Model rules (see README):
//   - every task belongs to an existing project
//   - an assignee is a contributor or manager of the task's project
//   - project members are users of the project's organization, with a valid role

// createTaskAggregate creates one task in one project.
type createTaskAggregate struct {
	project  *Project
	assignee *Membership // project.Membership(assignee); nil means unassigned
	in       TaskTableCreateInput
}

func loadCreateTask(ctx context.Context, tx DB, projectID string, in TaskTableCreateInput) (*createTaskAggregate, error) {
	p, err := NewProjectTable().Find(ctx, tx, projectID)
	if errors.Is(err, ErrNoRows) {
		return nil, deny(ErrInvalid, "task must belong to an existing project")
	} else if err != nil {
		return nil, err
	}
	a := &createTaskAggregate{project: p, in: in}
	if in.AssigneeID != "" {
		m, err := p.Membership(ctx, tx, in.AssigneeID)
		if errors.Is(err, ErrNoRows) {
			return nil, deny(ErrInvalid, "assignee must be a contributor or manager of the project")
		} else if err != nil {
			return nil, err
		}
		a.assignee = m
	}
	return a, nil
}

func (a *createTaskAggregate) validate() error {
	if a.assignee != nil && !isWriterRole(a.assignee.Role) {
		return deny(ErrInvalid, "assignee must be a contributor or manager of the project")
	}
	return nil
}

func (a *createTaskAggregate) apply(ctx context.Context, tx DB) (*Task, error) {
	t := a.project.newTask(a.in.Title)
	if a.assignee != nil {
		t.AssigneeID = a.assignee.UserID
	}
	if err := t.create(ctx, tx); err != nil {
		return nil, err
	}
	return t, nil
}

// updateTaskAggregate changes the title, status and/or assignee of one task.
type updateTaskAggregate struct {
	task        *Task
	in          TaskTableUpdateInput
	newAssignee *Membership // task.Project().Membership(new assignee), when assigning someone
}

func loadUpdateTask(ctx context.Context, tx DB, taskID string, in TaskTableUpdateInput) (*updateTaskAggregate, error) {
	t, err := NewTaskTable().Find(ctx, tx, taskID)
	if errors.Is(err, ErrNoRows) {
		return nil, deny(ErrNotFound, "not found")
	} else if err != nil {
		return nil, err
	}
	a := &updateTaskAggregate{task: t, in: in}
	if in.Assignee != nil && *in.Assignee != "" {
		proj, err := t.Project(ctx, tx)
		if errors.Is(err, ErrNoRows) {
			return nil, deny(ErrInvalid, "task must belong to an existing project")
		} else if err != nil {
			return nil, err
		}
		m, err := proj.Membership(ctx, tx, *in.Assignee)
		if errors.Is(err, ErrNoRows) {
			return nil, deny(ErrInvalid, "assignee must be a contributor or manager of the project")
		} else if err != nil {
			return nil, err
		}
		a.newAssignee = m
	}
	return a, nil
}

func (a *updateTaskAggregate) validate() error {
	if a.newAssignee != nil && !isWriterRole(a.newAssignee.Role) {
		return deny(ErrInvalid, "assignee must be a contributor or manager of the project")
	}
	return nil
}

func (a *updateTaskAggregate) apply(ctx context.Context, tx DB) (*Task, error) {
	t := a.task
	if a.in.Assignee != nil {
		if a.newAssignee != nil {
			if err := t.assign(ctx, tx, a.newAssignee); err != nil {
				return nil, err
			}
		} else if err := t.unassign(ctx, tx); err != nil {
			return nil, err
		}
	}
	if a.in.Title != nil {
		if err := t.rename(ctx, tx, *a.in.Title); err != nil {
			return nil, err
		}
	}
	if a.in.Status != nil {
		if err := t.setStatus(ctx, tx, *a.in.Status); err != nil {
			return nil, err
		}
	}
	return t, nil
}

// deleteTaskAggregate removes one task.
type deleteTaskAggregate struct {
	task *Task
}

func loadDeleteTask(ctx context.Context, tx DB, taskID string) (*deleteTaskAggregate, error) {
	t, err := NewTaskTable().Find(ctx, tx, taskID)
	if errors.Is(err, ErrNoRows) {
		return nil, deny(ErrNotFound, "not found")
	} else if err != nil {
		return nil, err
	}
	return &deleteTaskAggregate{task: t}, nil
}

func (a *deleteTaskAggregate) validate() error { return nil }

func (a *deleteTaskAggregate) apply(ctx context.Context, tx DB) error { return a.task.destroy(ctx, tx) }
