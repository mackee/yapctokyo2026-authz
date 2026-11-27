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

func TestMove(t *testing.T) {
	cases := []struct {
		name, user, path, body string
		want                   int
	}{
		{"manager moves to writable dest", "ivy", "/projects/web/tasks/t2/move", `{"toProjectId":"ops"}`, 200},
		{"contributor moves own task", "cat", "/projects/web/tasks/t2/move", `{"toProjectId":"ops"}`, 200},
		{"contributor moves others", "cat", "/projects/web/tasks/t1/move", `{"toProjectId":"ops"}`, 403},
		{"viewer source", "dan", "/projects/web/tasks/t1/move", `{"toProjectId":"ops"}`, 403},
		{"org admin no role", "eve", "/projects/web/tasks/t1/move", `{"toProjectId":"ops"}`, 403},
		{"non member source", "hal", "/projects/web/tasks/t1/move", `{"toProjectId":"ops"}`, 404},
		{"other org source", "gus", "/projects/web/tasks/t1/move", `{"toProjectId":"ops"}`, 404},
		{"cross project id", "ann", "/projects/ops/tasks/t1/move", `{"toProjectId":"web"}`, 404},
		{"archived source", "ann", "/projects/legacy/tasks/t5/move", `{"toProjectId":"ops"}`, 409},
		{"dest viewer", "ann", "/projects/web/tasks/t3/move", `{"toProjectId":"ops"}`, 403},
		{"dest non member", "cat", "/projects/web/tasks/t2/move", `{"toProjectId":"legacy"}`, 404},
		{"dest other org", "ann", "/projects/web/tasks/t3/move", `{"toProjectId":"gx"}`, 404},
		{"dest missing", "ann", "/projects/web/tasks/t3/move", `{"toProjectId":"nope"}`, 404},
		{"dest archived", "ann", "/projects/web/tasks/t3/move", `{"toProjectId":"legacy"}`, 409},
		{"same project", "ann", "/projects/web/tasks/t3/move", `{"toProjectId":"web"}`, 400},
		{"missing toProjectId", "ann", "/projects/web/tasks/t3/move", `{}`, 400},
		{"bad json", "ann", "/projects/web/tasks/t3/move", `{`, 400},
		{"anonymous", "", "/projects/web/tasks/t3/move", `{"toProjectId":"ops"}`, 401},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, _ := do(t, newServer(), "POST", c.path, c.user, c.body)
			if code != c.want {
				t.Fatalf("got %d want %d", code, c.want)
			}
		})
	}
}

func TestMoveKeepsFieldsAndUpdatesProject(t *testing.T) {
	srv := newServer()
	code, body := do(t, srv, "POST", "/projects/web/tasks/t2/move", "cat", `{"toProjectId":"ops"}`)
	if code != 200 {
		t.Fatalf("move: got %d: %s", code, body)
	}
	var moved Task
	if err := json.Unmarshal([]byte(body), &moved); err != nil {
		t.Fatal(err)
	}
	want := Task{ID: "t2", ProjectID: "ops", Title: "Update footer", Status: "doing", Assignee: "cat"}
	if moved != want {
		t.Fatalf("moved task = %+v, want %+v", moved, want)
	}

	// No longer visible in the source project.
	if code, _ := do(t, srv, "GET", "/projects/web/tasks/t2", "ann", ""); code != 404 {
		t.Fatalf("old project lookup: got %d want 404", code)
	}
	// Visible in the destination project with the same ID and fields.
	code, body = do(t, srv, "GET", "/projects/ops/tasks/t2", "ann", "")
	if code != 200 {
		t.Fatalf("new project lookup: got %d: %s", code, body)
	}
	var got Task
	json.Unmarshal([]byte(body), &got)
	if got != want {
		t.Fatalf("stored task = %+v, want %+v", got, want)
	}
	// Source project list no longer includes it; destination list does.
	_, body = do(t, srv, "GET", "/projects/web/tasks", "ann", "")
	if strings.Contains(body, `"id":"t2"`) {
		t.Fatalf("t2 still listed in web: %s", body)
	}
	_, body = do(t, srv, "GET", "/projects/ops/tasks", "ann", "")
	if !strings.Contains(body, `"id":"t2"`) {
		t.Fatalf("t2 missing from ops: %s", body)
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
