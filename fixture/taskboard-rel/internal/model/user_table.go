package model

import "context"

// UserTable reads the users table.
type UserTable struct{}

// NewUserTable returns the users table.
func NewUserTable() *UserTable { return &UserTable{} }

// Find returns the user with the ID, or ErrNoRows.
func (*UserTable) Find(ctx context.Context, db DB, id string) (*User, error) {
	var u User
	if err := db.view(ctx, func(x *Tx) error {
		r, ok := x.t.users[id]
		if !ok {
			return ErrNoRows
		}
		u = r
		return nil
	}); err != nil {
		return nil, err
	}
	return &u, nil
}
