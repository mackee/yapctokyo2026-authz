package model

import "context"

// ProjectTable reads the projects table.
type ProjectTable struct{}

// NewProjectTable returns the projects table.
func NewProjectTable() *ProjectTable { return &ProjectTable{} }

// Find returns the project with the ID, or ErrNoRows.
func (*ProjectTable) Find(ctx context.Context, db DB, id string) (*Project, error) {
	var p Project
	if err := db.QueryRowContext(ctx, `SELECT id, org_id, name, archived FROM projects WHERE id = ?`, id).
		Scan(&p.ID, &p.Org, &p.Name, &p.Archived); err != nil {
		return nil, err
	}
	return &p, nil
}
