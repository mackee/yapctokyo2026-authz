package model

import (
	"context"
	"sort"
)

// MembershipTable reads the memberships table.
type MembershipTable struct{}

// NewMembershipTable returns the memberships table.
func NewMembershipTable() *MembershipTable { return &MembershipTable{} }

// Find returns the membership of the user in the project, or ErrNoRows.
func (*MembershipTable) Find(ctx context.Context, db DB, projectID, userID string) (*Membership, error) {
	var m Membership
	if err := db.view(ctx, func(x *Tx) error {
		role, ok := x.t.members[projectID][userID]
		if !ok {
			return ErrNoRows
		}
		m = Membership{ProjectID: projectID, UserID: userID, Role: role}
		return nil
	}); err != nil {
		return nil, err
	}
	return &m, nil
}

// ListByProject returns the memberships of the project, ordered by user ID.
func (*MembershipTable) ListByProject(ctx context.Context, db DB, projectID string) ([]*Membership, error) {
	out := []*Membership{}
	if err := db.view(ctx, func(x *Tx) error {
		for uid, role := range x.t.members[projectID] {
			out = append(out, &Membership{ProjectID: projectID, UserID: uid, Role: role})
		}
		return nil
	}); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UserID < out[j].UserID })
	return out, nil
}

// ListByUser returns the memberships of the user in all projects, ordered by
// project ID.
func (*MembershipTable) ListByUser(ctx context.Context, db DB, userID string) ([]*Membership, error) {
	out := []*Membership{}
	if err := db.view(ctx, func(x *Tx) error {
		for pid, m := range x.t.members {
			if role, ok := m[userID]; ok {
				out = append(out, &Membership{ProjectID: pid, UserID: userID, Role: role})
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ProjectID < out[j].ProjectID })
	return out, nil
}
