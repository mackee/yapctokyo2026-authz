# taskboard

A tiny project task tracker HTTP API (Go, `net/http`, SQLite via `database/sql` and
`modernc.org/sqlite`). Every server starts with a fresh in-memory database
loaded with the seed data below (`db.go` has the schema and the seed).

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
