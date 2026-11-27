// Package model owns the taskboard data, the authorization objects and the
// table operations.
//
//	db.go                 Conn / Tx / DB: the in-memory stand-in for *sql.DB / *sql.Tx
//	*_table.go            tables: Find / List* read rows (ErrNoRows when none match);
//	                      TaskTable also has the API operations, each taking (ctx, db, authz, input)
//	record.go             rows (User, Project, Membership, Task): relation methods and row changes
//	authz.go              authorization objects (ProjectAuthz, TaskAuthz) and how they are built
//	aggregate.go          the aggregate each table operation runs in its transaction
//
// Only the tables and the rows touch the data.
//
// The HTTP layer authenticates the caller and builds an authorization object
// per scope, then calls table operations with it. Rows are changed only by
// table operations, which ask the authorization object first.
package model

import "errors"

// Project roles.
const (
	RoleManager     = "manager"
	RoleContributor = "contributor"
	RoleViewer      = "viewer"
)

// Error kinds. The HTTP layer maps them to statuses.
var (
	ErrUnauthenticated = errors.New("unauthenticated")     // 401
	ErrNotFound        = errors.New("not found")           // 404 (also used for "not readable")
	ErrForbidden       = errors.New("forbidden")           // 403
	ErrArchived        = errors.New("project is archived") // 409
	ErrInvalid         = errors.New("invalid request")     // 400
)

type denial struct {
	kind error
	msg  string
}

func (d *denial) Error() string { return d.msg }
func (d *denial) Unwrap() error { return d.kind }

func deny(kind error, msg string) error { return &denial{kind: kind, msg: msg} }

func isWriterRole(role string) bool { return role == RoleManager || role == RoleContributor }
