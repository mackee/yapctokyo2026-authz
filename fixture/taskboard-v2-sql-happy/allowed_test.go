package main

import "testing"

// TestAllowed lists calls that must succeed: who may do what. Each row runs on
// a fresh server. When you add an operation, add a row for every role that
// should be allowed to perform it.
func TestAllowed(t *testing.T) {
	cases := []struct {
		name, user, method, path, body string
		want                           int
	}{
		// read
		{"viewer lists tasks", "dan", "GET", "/projects/web/tasks", "", 200},
		{"viewer reads a task", "dan", "GET", "/projects/web/tasks/t1", "", 200},
		{"contributor lists tasks", "ben", "GET", "/projects/web/tasks", "", 200},
		{"manager lists tasks", "ann", "GET", "/projects/web/tasks", "", 200},
		{"org admin lists tasks", "eve", "GET", "/projects/ops/tasks", "", 200},
		{"member reads archived project", "ben", "GET", "/projects/legacy/tasks/t5", "", 200},

		// create
		{"contributor creates own task", "ben", "POST", "/projects/web/tasks", `{"title":"x","assignee":"ben"}`, 200},
		{"contributor creates unassigned task", "cat", "POST", "/projects/ops/tasks", `{"title":"x"}`, 200},
		{"manager creates task for contributor", "ann", "POST", "/projects/web/tasks", `{"title":"x","assignee":"cat"}`, 200},
		{"manager creates task for manager", "ivy", "POST", "/projects/ops/tasks", `{"title":"x","assignee":"ivy"}`, 200},

		// update
		{"contributor updates own task", "ben", "PATCH", "/projects/web/tasks/t1", `{"status":"done"}`, 200},
		{"contributor renames own task", "cat", "PATCH", "/projects/ops/tasks/t4", `{"title":"y"}`, 200},
		{"manager updates any task", "ann", "PATCH", "/projects/web/tasks/t2", `{"status":"done"}`, 200},
		{"manager reassigns task", "ann", "PATCH", "/projects/web/tasks/t2", `{"assignee":"ben"}`, 200},
		{"manager unassigns task", "ivy", "PATCH", "/projects/ops/tasks/t4", `{"assignee":""}`, 200},

		// delete
		{"manager deletes task", "ann", "DELETE", "/projects/web/tasks/t1", "", 204},
		{"manager deletes in other project", "ivy", "DELETE", "/projects/ops/tasks/t4", "", 204},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, body := do(t, newServer(), c.method, c.path, c.user, c.body)
			if code != c.want {
				t.Fatalf("got %d want %d body=%s", code, c.want, body)
			}
		})
	}
}
