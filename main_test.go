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

func TestMoveProject(t *testing.T) {
	cases := []struct {
		name, user, path, body string
		want                   int
	}{
		{"manager moves and clears assignee", "ivy", "/projects/web/tasks/t1", `{"projectId":"ops","assignee":""}`, 200},
		{"manager moves, assignee not in target", "ivy", "/projects/web/tasks/t1", `{"projectId":"ops"}`, 400},
		{"manager moves, assignee valid in target", "ivy", "/projects/web/tasks/t2", `{"projectId":"ops"}`, 200},
		{"new assignee must be valid in target", "ivy", "/projects/web/tasks/t3", `{"projectId":"ops","assignee":"dan"}`, 400},
		{"target viewer only", "ann", "/projects/web/tasks/t3", `{"projectId":"ops"}`, 403},
		{"target not a member", "ben", "/projects/web/tasks/t1", `{"projectId":"ops"}`, 404},
		{"target other org", "ben", "/projects/web/tasks/t1", `{"projectId":"gx"}`, 404},
		{"target missing", "ivy", "/projects/web/tasks/t1", `{"projectId":"nope"}`, 404},
		{"target archived", "ann", "/projects/web/tasks/t1", `{"projectId":"legacy"}`, 409},
		{"empty projectId", "ivy", "/projects/web/tasks/t1", `{"projectId":""}`, 400},
		{"contributor cannot move others", "ben", "/projects/web/tasks/t2", `{"projectId":"ops"}`, 403},
		{"contributor moves own task to project they contribute to", "cat", "/projects/ops/tasks/t4", `{"projectId":"web"}`, 200},
		{"org admin without source role", "eve", "/projects/web/tasks/t1", `{"projectId":"ops"}`, 403},
		{"source task in other project", "ivy", "/projects/ops/tasks/t1", `{"projectId":"web"}`, 404},
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

func TestMoveProjectWithOtherFields(t *testing.T) {
	srv := newServer()
	code, body := do(t, srv, "PATCH", "/projects/web/tasks/t2", "ivy",
		`{"projectId":"ops","title":"Moved footer","status":"done"}`)
	if code != 200 {
		t.Fatalf("move: got %d want 200: %s", code, body)
	}
	var got Task
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatal(err)
	}
	want := Task{ID: "t2", ProjectID: "ops", Title: "Moved footer", Status: "done", Assignee: "cat"}
	if got != want {
		t.Fatalf("response = %+v, want %+v", got, want)
	}

	if code, _ := do(t, srv, "GET", "/projects/web/tasks/t2", "ann", ""); code != 404 {
		t.Fatalf("old project still serves task: got %d", code)
	}
	code, body = do(t, srv, "GET", "/projects/ops/tasks/t2", "ivy", "")
	if code != 200 {
		t.Fatalf("new project lookup: got %d", code)
	}
	json.Unmarshal([]byte(body), &got)
	if got != want {
		t.Fatalf("stored task = %+v, want %+v", got, want)
	}
}

func TestMoveFailureLeavesTaskUnchanged(t *testing.T) {
	srv := newServer()
	// Invalid status alongside a valid move must not partially apply.
	if code, _ := do(t, srv, "PATCH", "/projects/web/tasks/t2", "ivy", `{"projectId":"ops","status":"nope"}`); code != 400 {
		t.Fatalf("got %d want 400", code)
	}
	code, body := do(t, srv, "GET", "/projects/web/tasks/t2", "ann", "")
	if code != 200 {
		t.Fatalf("task moved despite failed request: got %d %s", code, body)
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
