package model

import "context"

// UserTable reads the users table.
type UserTable struct{}

// NewUserTable returns the users table.
func NewUserTable() *UserTable { return &UserTable{} }

// Find returns the user with the ID, or ErrNoRows.
func (*UserTable) Find(ctx context.Context, db DB, id string) (*User, error) {
	var u User
	if err := db.QueryRowContext(ctx, `SELECT id, org_id, org_admin FROM users WHERE id = ?`, id).
		Scan(&u.ID, &u.Org, &u.OrgAdmin); err != nil {
		return nil, err
	}
	return &u, nil
}
