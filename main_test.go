package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func do(t *testing.T, h http.Handler, method, path, user, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if user != "" {
		req.Header.Set("X-User", user)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w.Code, w.Body.String()
}

func TestRead(t *testing.T) {
	cases := []struct {
		name, user, path string
		want             int
	}{
		{"member lists", "dan", "/projects/web/tasks", 200},
		{"org admin lists", "eve", "/projects/ops/tasks", 200},
		{"non member", "hal", "/projects/web/tasks", 404},
		{"other org", "gus", "/projects/web/tasks", 404},
		{"anonymous", "", "/projects/web/tasks", 401},
		{"unknown user", "zed", "/projects/web/tasks", 401},
		{"get task", "dan", "/projects/web/tasks/t1", 200},
		{"task in other project", "ann", "/projects/web/tasks/t4", 404},
		{"archived readable", "ben", "/projects/legacy/tasks/t5", 200},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, _ := do(t, newServer(), "GET", c.path, c.user, "")
			if code != c.want {
				t.Fatalf("got %d want %d", code, c.want)
			}
		})
	}
}

func TestListOnlyProjectTasks(t *testing.T) {
	code, body := do(t, newServer(), "GET", "/projects/web/tasks", "ann", "")
	if code != 200 {
		t.Fatal(code)
	}
	var tasks []Task
	json.Unmarshal([]byte(body), &tasks)
	if len(tasks) != 3 {
		t.Fatalf("want 3 tasks, got %d", len(tasks))
	}
	for _, task := range tasks {
		if task.ProjectID != "web" {
			t.Fatalf("leaked task %s", task.ID)
		}
	}
}

func TestCreate(t *testing.T) {
	cases := []struct {
		name, user, project, body string
		want                      int
	}{
		{"contributor self", "ben", "web", `{"title":"x","assignee":"ben"}`, 201},
		{"contributor unassigned", "ben", "web", `{"title":"x"}`, 201},
		{"contributor assigns other", "ben", "web", `{"title":"x","assignee":"cat"}`, 403},
		{"manager assigns", "ann", "web", `{"title":"x","assignee":"cat"}`, 201},
		{"viewer", "dan", "web", `{"title":"x"}`, 403},
		{"org admin", "eve", "web", `{"title":"x"}`, 403},
		{"non member", "hal", "web", `{"title":"x"}`, 404},
		{"archived", "ann", "legacy", `{"title":"x"}`, 409},
		{"empty title", "ann", "web", `{"title":""}`, 400},
		{"assignee viewer", "ann", "web", `{"title":"x","assignee":"dan"}`, 400},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, _ := do(t, newServer(), "POST", "/projects/"+c.project+"/tasks", c.user, c.body)
			if code != c.want {
				t.Fatalf("got %d want %d", code, c.want)
			}
		})
	}
}

func TestUpdate(t *testing.T) {
	cases := []struct {
		name, user, path, body string
		want                   int
	}{
		{"contributor own", "ben", "/projects/web/tasks/t1", `{"status":"done"}`, 200},
		{"contributor others", "ben", "/projects/web/tasks/t2", `{"status":"done"}`, 403},
		{"contributor reassign", "ben", "/projects/web/tasks/t1", `{"assignee":"cat"}`, 403},
		{"manager any", "ann", "/projects/web/tasks/t2", `{"status":"done","assignee":"ben"}`, 200},
		{"viewer", "dan", "/projects/web/tasks/t1", `{"status":"done"}`, 403},
		{"org admin", "eve", "/projects/web/tasks/t1", `{"status":"done"}`, 403},
		{"cross project id", "ann", "/projects/web/tasks/t4", `{"status":"done"}`, 404},
		{"cross org", "gus", "/projects/web/tasks/t1", `{"status":"done"}`, 404},
		{"archived", "ann", "/projects/legacy/tasks/t5", `{"status":"done"}`, 409},
		{"bad status", "ann", "/projects/web/tasks/t1", `{"status":"nope"}`, 400},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, _ := do(t, newServer(), "PATCH", c.path, c.user, c.body)
			if code != c.want {
				t.Fatalf("got %d want %d", code, c.want)
			}
		})
	}
}

func TestDelete(t *testing.T) {
	cases := []struct {
		name, user, path string
		want             int
	}{
		{"manager", "ann", "/projects/web/tasks/t1", 204},
		{"contributor own", "ben", "/projects/web/tasks/t1", 403},
		{"archived", "ann", "/projects/legacy/tasks/t5", 409},
		{"cross project", "ann", "/projects/web/tasks/t4", 404},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, _ := do(t, newServer(), "DELETE", c.path, c.user, "")
			if code != c.want {
				t.Fatalf("got %d want %d", code, c.want)
			}
		})
	}
}

func TestBulkStatus(t *testing.T) {
	cases := []struct {
		name, user, project, body string
		want                      int
	}{
		{"manager many", "ann", "web", `{"taskIds":["t1","t2"],"status":"done"}`, 200},
		{"contributor own", "ben", "web", `{"taskIds":["t1"],"status":"done"}`, 200},
		{"contributor others", "ben", "web", `{"taskIds":["t1","t2"],"status":"done"}`, 403},
		{"contributor unassigned", "ben", "web", `{"taskIds":["t3"],"status":"done"}`, 403},
		{"viewer", "dan", "web", `{"taskIds":["t1"],"status":"done"}`, 403},
		{"org admin", "eve", "web", `{"taskIds":["t1"],"status":"done"}`, 403},
		{"non member", "hal", "web", `{"taskIds":["t1"],"status":"done"}`, 404},
		{"other org", "gus", "web", `{"taskIds":["t1"],"status":"done"}`, 404},
		{"anonymous", "", "web", `{"taskIds":["t1"],"status":"done"}`, 401},
		{"archived", "ann", "legacy", `{"taskIds":["t5"],"status":"done"}`, 409},
		{"cross project id", "ann", "web", `{"taskIds":["t1","t4"],"status":"done"}`, 404},
		{"unknown task id", "ann", "web", `{"taskIds":["t1","zzz"],"status":"done"}`, 404},
		{"bad status", "ann", "web", `{"taskIds":["t1"],"status":"nope"}`, 400},
		{"missing status", "ann", "web", `{"taskIds":["t1"]}`, 400},
		{"empty taskIds", "ann", "web", `{"taskIds":[],"status":"done"}`, 400},
		{"invalid json", "ann", "web", `{"taskIds":`, 400},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, _ := do(t, newServer(), "PATCH", "/projects/"+c.project+"/tasks", c.user, c.body)
			if code != c.want {
				t.Fatalf("got %d want %d", code, c.want)
			}
		})
	}
}

func TestBulkStatusResponse(t *testing.T) {
	code, body := do(t, newServer(), "PATCH", "/projects/web/tasks", "ann",
		`{"taskIds":["t3","t1","t3"],"status":"done"}`)
	if code != 200 {
		t.Fatalf("got %d: %s", code, body)
	}
	var tasks []Task
	if err := json.Unmarshal([]byte(body), &tasks); err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 || tasks[0].ID != "t3" || tasks[1].ID != "t1" {
		t.Fatalf("unexpected tasks: %+v", tasks)
	}
	for _, task := range tasks {
		if task.Status != "done" {
			t.Fatalf("task %s status %q", task.ID, task.Status)
		}
	}
}

func TestBulkStatusIsAllOrNothing(t *testing.T) {
	h := newServer()
	// t4 belongs to ops, so the whole request must be rejected.
	if code, _ := do(t, h, "PATCH", "/projects/web/tasks", "ann",
		`{"taskIds":["t1","t4"],"status":"done"}`); code != 404 {
		t.Fatalf("got %d want 404", code)
	}
	// Contributor ben may change t1 but not t2: nothing should change.
	if code, _ := do(t, h, "PATCH", "/projects/web/tasks", "ben",
		`{"taskIds":["t1","t2"],"status":"done"}`); code != 403 {
		t.Fatalf("got %d want 403", code)
	}
	code, body := do(t, h, "GET", "/projects/web/tasks", "ann", "")
	if code != 200 {
		t.Fatal(code)
	}
	var tasks []Task
	json.Unmarshal([]byte(body), &tasks)
	want := map[string]string{"t1": "open", "t2": "doing", "t3": "open"}
	for _, task := range tasks {
		if task.Status != want[task.ID] {
			t.Fatalf("task %s changed to %q", task.ID, task.Status)
		}
	}
}
