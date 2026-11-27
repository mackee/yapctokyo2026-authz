package model

import "context"

// MembershipTable reads the memberships table.
type MembershipTable struct{}

// NewMembershipTable returns the memberships table.
func NewMembershipTable() *MembershipTable { return &MembershipTable{} }

// Find returns the membership of the user in the project, or ErrNoRows.
func (*MembershipTable) Find(ctx context.Context, db DB, projectID, userID string) (*Membership, error) {
	var m Membership
	if err := db.QueryRowContext(ctx,
		`SELECT project_id, user_id, role FROM memberships WHERE project_id = ? AND user_id = ?`, projectID, userID).
		Scan(&m.ProjectID, &m.UserID, &m.Role); err != nil {
		return nil, err
	}
	return &m, nil
}

// ListByProject returns the memberships of the project, ordered by user ID.
func (mt *MembershipTable) ListByProject(ctx context.Context, db DB, projectID string) ([]*Membership, error) {
	return mt.list(ctx, db,
		`SELECT project_id, user_id, role FROM memberships WHERE project_id = ? ORDER BY user_id`, projectID)
}

// ListByUser returns the memberships of the user in all projects, ordered by
// project ID.
func (mt *MembershipTable) ListByUser(ctx context.Context, db DB, userID string) ([]*Membership, error) {
	return mt.list(ctx, db,
		`SELECT project_id, user_id, role FROM memberships WHERE user_id = ? ORDER BY project_id`, userID)
}

func (*MembershipTable) list(ctx context.Context, db DB, query string, args ...any) ([]*Membership, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Membership{}
	for rows.Next() {
		var m Membership
		if err := rows.Scan(&m.ProjectID, &m.UserID, &m.Role); err != nil {
			return nil, err
		}
		out = append(out, &m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
