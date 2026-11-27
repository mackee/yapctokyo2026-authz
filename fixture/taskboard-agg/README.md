# taskboard

A tiny project task tracker HTTP API (Go, `net/http`, in-memory seed data).

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
| POST | `/projects/{pid}/tasks` | `{"title","assignee"}` → 201 |
| GET | `/projects/{pid}/tasks/{tid}` | |
| PATCH | `/projects/{pid}/tasks/{tid}` | `{"title"?,"status"?,"assignee"?}` → 200 |
| DELETE | `/projects/{pid}/tasks/{tid}` | → 204 |

Statuses: `open`, `doing`, `done`. Errors are `{"error":"..."}`.

## Access rules

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

## Architecture (tanukirpc + capabilities)

The access rules above are implemented once, in `internal/capability`. HTTP
wiring is in `main.go` and uses [tanukirpc](https://github.com/mackee/tanukirpc):

```text
Transformer "/projects/{pid}"   authenticates X-User -> projectRegistry{scope ProjectScope}
  Transformer "/tasks/{tid}"    -> taskRegistry{scope TaskScope}
```

- Handlers only see **scopes** bound to the authenticated actor and the path.
  To read or write anything they **acquire a capability** from a scope:
  `ProjectScope.Read()/Write()`, `TaskScope.Read()/Edit()/Manage()`. Each
  acquisition applies the access rules and returns an error that maps onto the
  HTTP status convention (`ErrUnauthenticated` 401, `ErrNotFound` 404,
  `ErrForbidden` 403, `ErrArchived` 409, `ErrInvalid` 400).
- `ActorScope.Project(pid)` gives a scope for another project of the same
  actor (for features that touch more than one project); acquiring from it
  applies the same rules.
- Capabilities are interfaces with unexported implementations: they cannot be
  constructed outside `internal/capability`. A capability is the authorized
  model object: its methods perform the writes. For example a task's assignee
  is an `Assignee` admitted for that task's project by
  `WriteProject.AdmitAssignee`.
- The data is private to `internal/capability` and kept in Active Record style
  records (`record.go`: one record type per table, finders on the transaction,
  `save`/`destroy` on the records). `db.update` runs a write on a copy and
  commits it only if it succeeds, so a write is all-or-nothing.
- Each write operation has its own aggregate (`aggregate.go`), built inside the
  transaction for that operation: it loads the records the operation needs,
  validates the model rules the operation touches, and saves the records. The
  model rules are: tasks belong to existing projects; an assignee is a
  contributor or manager of the task's project; project members belong to the
  project's organization. A write that would break a rule is rejected with
  `ErrInvalid` (400) and nothing changes.
