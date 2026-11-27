package main

import (
	"net/http"
	"testing"
)

// modelRules are the rules the data must satisfy after every request. Each query
// returns one row per violation.
var modelRules = []struct {
	rule, query string
}{
	{
		"tasks belong to existing projects",
		`SELECT 'task ' || t.id || ' is in missing project ' || t.project_id
		   FROM tasks t LEFT JOIN projects p ON p.id = t.project_id
		  WHERE p.id IS NULL`,
	},
	{
		"an assignee is a contributor or manager of the task's project",
		`SELECT 'task ' || t.id || ' in ' || t.project_id || ' is assigned to ' || t.assignee ||
		        ', whose role there is ' || COALESCE(m.role, 'none')
		   FROM tasks t LEFT JOIN memberships m
		     ON m.project_id = t.project_id AND m.user_id = t.assignee
		  WHERE t.assignee <> '' AND COALESCE(m.role, '') NOT IN ('contributor', 'manager')`,
	},
	{
		"project members belong to the project's organization",
		`SELECT 'member ' || m.user_id || ' of ' || m.project_id || ' is in org ' ||
		        COALESCE(u.org_id, '(no such user)') || ', project is in ' || COALESCE(p.org_id, '(no such project)')
		   FROM memberships m
		   LEFT JOIN projects p ON p.id = m.project_id
		   LEFT JOIN users u ON u.id = m.user_id
		  WHERE p.id IS NULL OR u.id IS NULL OR u.org_id <> p.org_id`,
	},
}

// checkModelRules fails the test if the data behind h breaks a model rule. do()
// calls it after every request that is not a GET, so a test that sends a write
// through do() also checks the rules.
func checkModelRules(t *testing.T, h http.Handler, after string) {
	t.Helper()
	app, ok := h.(*appHandler)
	if !ok {
		t.Fatalf("checkModelRules: %T is not the handler built by newServer", h)
	}
	for _, r := range modelRules {
		rows, err := app.db.Query(r.query)
		if err != nil {
			t.Fatalf("checkModelRules: %v", err)
		}
		var broken []string
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				t.Fatalf("checkModelRules: %v", err)
			}
			broken = append(broken, s)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			t.Fatalf("checkModelRules: %v", err)
		}
		for _, b := range broken {
			t.Errorf("after %s: model rule %q is broken: %s", after, r.rule, b)
		}
	}
}

func TestSeedKeepsModelRules(t *testing.T) {
	checkModelRules(t, newServer(), "seeding")
}
