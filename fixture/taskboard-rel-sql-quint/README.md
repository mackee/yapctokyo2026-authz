# taskboard

A tiny project task tracker HTTP API (Go, SQLite via `database/sql` and
`modernc.org/sqlite`). Every server starts with a fresh in-memory database
loaded with the seed data below (`internal/model/db.go` has the schema and the seed).

```sh
go run . -addr :8080
curl -H 'X-User: ann' localhost:8080/projects/web/tasks
go test ./...
```

`X-User` is a demo stand-in for authentication.

## Endpoints

| Method | Path | Notes |
| --- | --- | --- |
| GET | `/projects/{pid}/tasks` | list tasks of a project |
| POST | `/projects/{pid}/tasks` | `{"title","assignee"}` → 200 |
| GET | `/projects/{pid}/tasks/{tid}` | |
| PATCH | `/projects/{pid}/tasks/{tid}` | `{"title"?,"status"?,"assignee"?}` → 200 |
| DELETE | `/projects/{pid}/tasks/{tid}` | → 204 |

Statuses: `open`, `doing`, `done`. Errors are `{"error":"..."}`. Request
bodies are JSON (`Content-Type: application/json`).

## Access rules

- A malformed request body (bad JSON, missing or invalid fields that can be
  checked without looking at the data) → 400, before any of the rules below.
- Unknown or missing user → 401.
- Project missing, in another organization, or not readable by the user → 404.
- A project is readable by its members (any role) and by org admins of the same organization.
- Writes require the `contributor` or `manager` role in the project. Users who can read but not write get 403.
- Contributors may only modify tasks assigned to themselves, and may not reassign tasks. Managers may modify any task.
- Only managers may delete tasks.
- Archived projects reject all writes with 409 (reads still work).
- A task ID that does not belong to the project in the path → 404.

## Seed data

| User | Org | Roles |
| --- | --- | --- |
| ann | acme | web: manager, ops: viewer, legacy: manager |
| ben | acme | web: contributor, legacy: contributor |
| cat | acme | web: contributor, ops: contributor |
| dan | acme | web: viewer |
| eve | acme | org admin (no project roles) |
| hal | acme | none |
| ivy | acme | web: manager, ops: manager |
| gus | globex | gx: manager |

Projects: `web`, `ops`, `legacy` (archived) in acme; `gx` in globex.

## Architecture (tanukirpc + authorization objects + table operations)

The access rules above are implemented in `internal/model`. HTTP wiring is in
`main.go` and uses [tanukirpc](https://github.com/mackee/tanukirpc):

```text
Transformer "/projects/{pid}"   authenticates X-User, builds ProjectAuthz -> projectRegistry{db, authz}
  Transformer "/tasks/{tid}"    builds TaskAuthz                          -> taskRegistry{db, authz}
```

- Request bodies are typed structs decoded and validated by tanukirpc before
  the handler runs (`validate` struct tags), so body errors (400) come before
  access decisions.
- Each Transformer builds an **authorization object** for its scope:
  `model.NewProjectAuthz` returns a `ProjectAuthz`, `model.NewTaskAuthz` a
  `TaskAuthz`. It records what the accessor may do in that scope and is
  evaluated once, when it is built. A project the accessor cannot read, or a
  task that does not belong to the project, stops in the Transformer (404).
  Authorization objects have unexported fields: they can only be built by
  those functions, and the zero value grants nothing.
- Handlers call a **table operation** with the database, the authorization
  object and an input:
  `model.NewTaskTable().Create(ctx, reg.db, reg.authz, model.TaskTableCreateInput{...})`.
  The operation asks the authorization object whether the accessor may
  perform it (`CanWrite`, `CanAssign`, `CanEdit`, `CanReassign`, `CanManage`);
  a denial is an error that maps onto the HTTP status convention
  (`ErrUnauthenticated` 401, `ErrNotFound` 404, `ErrForbidden` 403,
  `ErrArchived` 409, `ErrInvalid` 400).
- `model.DB` is satisfied by `*sql.DB` and `*sql.Tx`. A table operation that
  writes runs in a transaction: given a `*sql.DB` it begins and commits its
  own; given a `*sql.Tx` it runs inside it. A failed write rolls back, so a
  write is all-or-nothing.
- Data access is hidden in the **tables** and the **rows**; only they run
  SQL. Each table (`NewUserTable()`, `NewProjectTable()`,
  `NewMembershipTable()`, `NewTaskTable()`) reads rows with `Find` and `List*`
  methods, which return `model.ErrNoRows` when no row matches.
- Rows (`User`, `Project`, `Membership`, `Task` in `record.go`) are in Active
  Record style. They load their related rows through relation methods that
  call the tables (for example `project.Membership(ctx, db, userID)`,
  `membership.AssignedTasks(ctx, db)`, `user.Memberships(ctx, db)`,
  `task.Project(ctx, db)`), and a change to a row is an unexported method on
  the row object that saves it (for example `task.assign`, `task.unassign`,
  `task.rename`), so rows are changed only by table operations.
- Each table operation runs its own aggregate (`aggregate.go`) inside the
  transaction: it starts from the row the operation is about (found through its
  table), loads the
  related rows through the relation methods, validates the model rules on
  them, and applies the change through the row-change methods. The model rules
  are: tasks belong to existing projects; an assignee is a contributor or
  manager of the task's project; project members belong to the project's
  organization. A write that would break a rule is rejected with `ErrInvalid`
  (400) and nothing changes.

## Formal model

`spec/taskboard.qnt` is a [Quint](https://quint-lang.org) model of the data, the
access rules and the model invariants. Each operation has an outcome function
(the HTTP status it must answer) and an action (the state change when it
succeeds). Check the model with:

```sh
quint typecheck spec/taskboard.qnt
quint test spec/taskboard.qnt
quint run spec/taskboard.qnt --invariant=invariants --max-steps=20 --max-samples=2000
```
