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
| POST | `/projects/{pid}/tasks/{tid}/move` | `{"toProjectId"}` → 200 with the moved task |

Statuses: `open`, `doing`, `done`. Errors are `{"error":"..."}`.

## Access rules

- Unknown or missing user → 401.
- Project missing, in another organization, or not readable by the user → 404.
- A project is readable by its members (any role) and by org admins of the same organization.
- Writes require the `contributor` or `manager` role in the project. Users who can read but not write get 403.
- Contributors may only modify tasks assigned to themselves, and may not reassign tasks. Managers may modify any task.
- Only managers may delete tasks.
- Moving a task requires the `manager` role in the source project and the `contributor` or `manager` role in the target project. The task keeps its ID, title, status and assignee, so the assignee may not be a member of the target project.
- Moving a task to the project it is already in → 400.
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
