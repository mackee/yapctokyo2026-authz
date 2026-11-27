package capability

// Aggregates: one per operation. An aggregate is built inside the transaction
// for a single operation, starting from the record the operation is about and
// loading the related records through the relation methods (record.go). It
// validates the model rules on those records, and applies the change through
// the row-change methods. There is no global check at commit time.
//
// Model rules (see README):
//   - every task belongs to an existing project
//   - an assignee is a contributor or manager of the task's project
//   - project members are users of the project's organization, with a valid role

// createTaskAggregate creates one task in one project.
type createTaskAggregate struct {
	project  projectRecord
	assignee *membershipRecord // project.Membership(assignee); nil means unassigned
	title    string
}

func loadCreateTask(x *tx, projectID, title, assigneeID string) (*createTaskAggregate, error) {
	p, ok := x.findProject(projectID)
	if !ok {
		return nil, deny(ErrInvalid, "task must belong to an existing project")
	}
	a := &createTaskAggregate{project: p, title: title}
	if assigneeID != "" {
		m, ok := p.Membership(x, assigneeID)
		if !ok {
			return nil, deny(ErrInvalid, "assignee must be a contributor or manager of the project")
		}
		a.assignee = &m
	}
	return a, nil
}

func (a *createTaskAggregate) validate() error {
	if a.assignee != nil && !isWriterRole(a.assignee.Role) {
		return deny(ErrInvalid, "assignee must be a contributor or manager of the project")
	}
	return nil
}

func (a *createTaskAggregate) apply(x *tx) TaskView {
	t := a.project.NewTask(a.title)
	if a.assignee != nil {
		t.AssigneeID = a.assignee.UserID
	}
	t.Create(x)
	return t.view()
}

// updateTaskAggregate changes the title, status and/or assignee of one task.
type updateTaskAggregate struct {
	task        taskRecord
	patch       TaskPatch
	newAssignee *membershipRecord // task.Project().Membership(new assignee), when assigning someone
}

func loadUpdateTask(x *tx, taskID string, p TaskPatch) (*updateTaskAggregate, error) {
	t, ok := x.findTask(taskID)
	if !ok {
		return nil, deny(ErrNotFound, "not found")
	}
	a := &updateTaskAggregate{task: t, patch: p}
	if p.Assignee != nil && *p.Assignee != "" {
		proj, ok := t.Project(x)
		if !ok {
			return nil, deny(ErrInvalid, "task must belong to an existing project")
		}
		m, ok := proj.Membership(x, *p.Assignee)
		if !ok {
			return nil, deny(ErrInvalid, "assignee must be a contributor or manager of the project")
		}
		a.newAssignee = &m
	}
	return a, nil
}

func (a *updateTaskAggregate) validate() error {
	if a.newAssignee != nil && !isWriterRole(a.newAssignee.Role) {
		return deny(ErrInvalid, "assignee must be a contributor or manager of the project")
	}
	return nil
}

func (a *updateTaskAggregate) apply(x *tx) TaskView {
	t := a.task
	if a.patch.Assignee != nil {
		if a.newAssignee != nil {
			t.Assign(x, *a.newAssignee)
		} else {
			t.Unassign(x)
		}
	}
	if a.patch.Title != nil {
		t.Rename(x, *a.patch.Title)
	}
	if a.patch.Status != nil {
		t.SetStatus(x, *a.patch.Status)
	}
	return t.view()
}

// deleteTaskAggregate removes one task.
type deleteTaskAggregate struct {
	task taskRecord
}

func loadDeleteTask(x *tx, taskID string) (*deleteTaskAggregate, error) {
	t, ok := x.findTask(taskID)
	if !ok {
		return nil, deny(ErrNotFound, "not found")
	}
	return &deleteTaskAggregate{task: t}, nil
}

func (a *deleteTaskAggregate) validate() error { return nil }

func (a *deleteTaskAggregate) apply(x *tx) { a.task.Destroy(x) }
