package capability

// Aggregates: one per operation. An aggregate is built inside a transaction
// for a single operation. It loads the records that operation needs,
// validates the model rules that operation touches, and saves the records.
// There is no global check at commit time.
//
// Model rules (see README):
//   - every task belongs to an existing project
//   - an assignee is a contributor or manager of the task's project
//   - project members are users of the project's organization, with a valid role

// createTaskAggregate creates one task in one project.
type createTaskAggregate struct {
	project  projectRecord
	assignee *membershipRecord // nil means unassigned
	task     taskRecord
}

func loadCreateTask(x *tx, projectID, title, assigneeID string) (*createTaskAggregate, error) {
	p, ok := x.findProject(projectID)
	if !ok {
		return nil, deny(ErrInvalid, "task must belong to an existing project")
	}
	a := &createTaskAggregate{
		project: p,
		task:    taskRecord{ProjectID: p.ID, Title: title, Status: "open", AssigneeID: assigneeID},
	}
	if assigneeID != "" {
		m, ok := x.findMembership(p.ID, assigneeID)
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

func (a *createTaskAggregate) save(x *tx) TaskView {
	a.task.ID = x.newTaskID()
	a.task.save(x)
	return a.task.view()
}

// updateTaskAggregate changes the title, status and/or assignee of one task.
type updateTaskAggregate struct {
	task        taskRecord
	newAssignee *membershipRecord // set when the patch assigns someone
}

func loadUpdateTask(x *tx, taskID string, p TaskPatch) (*updateTaskAggregate, error) {
	t, ok := x.findTask(taskID)
	if !ok {
		return nil, deny(ErrNotFound, "not found")
	}
	a := &updateTaskAggregate{task: t}
	if p.Title != nil {
		a.task.Title = *p.Title
	}
	if p.Status != nil {
		a.task.Status = *p.Status
	}
	if p.Assignee != nil {
		a.task.AssigneeID = *p.Assignee
		if *p.Assignee != "" {
			m, ok := x.findMembership(t.ProjectID, *p.Assignee)
			if !ok {
				return nil, deny(ErrInvalid, "assignee must be a contributor or manager of the project")
			}
			a.newAssignee = &m
		}
	}
	return a, nil
}

func (a *updateTaskAggregate) validate() error {
	if a.newAssignee != nil && !isWriterRole(a.newAssignee.Role) {
		return deny(ErrInvalid, "assignee must be a contributor or manager of the project")
	}
	return nil
}

func (a *updateTaskAggregate) save(x *tx) TaskView {
	a.task.save(x)
	return a.task.view()
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

func (a *deleteTaskAggregate) save(x *tx) { a.task.destroy(x) }
