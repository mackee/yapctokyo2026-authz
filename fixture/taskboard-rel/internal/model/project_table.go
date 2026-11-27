package model

import "context"

// ProjectTable reads the projects table.
type ProjectTable struct{}

// NewProjectTable returns the projects table.
func NewProjectTable() *ProjectTable { return &ProjectTable{} }

// Find returns the project with the ID, or ErrNoRows.
func (*ProjectTable) Find(ctx context.Context, db DB, id string) (*Project, error) {
	var p Project
	if err := db.view(ctx, func(x *Tx) error {
		r, ok := x.t.projects[id]
		if !ok {
			return ErrNoRows
		}
		p = r
		return nil
	}); err != nil {
		return nil, err
	}
	return &p, nil
}
