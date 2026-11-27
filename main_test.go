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
		{"manager of both", "ivy", "/projects/web/tasks/t1/move", `{"toProjectId":"ops"}`, 200},
		{"contributor source", "ben", "/projects/web/tasks/t1/move", `{"toProjectId":"ops"}`, 403},
		{"viewer source", "dan", "/projects/web/tasks/t1/move", `{"toProjectId":"ops"}`, 403},
		{"org admin source", "eve", "/projects/web/tasks/t1/move", `{"toProjectId":"ops"}`, 403},
		{"non member source", "hal", "/projects/web/tasks/t1/move", `{"toProjectId":"ops"}`, 404},
		{"other org", "gus", "/projects/web/tasks/t1/move", `{"toProjectId":"gx"}`, 404},
		{"anonymous", "", "/projects/web/tasks/t1/move", `{"toProjectId":"ops"}`, 401},
		{"unknown user", "zed", "/projects/web/tasks/t1/move", `{"toProjectId":"ops"}`, 401},
		{"task in other project", "ivy", "/projects/web/tasks/t4/move", `{"toProjectId":"ops"}`, 404},
		{"missing task", "ivy", "/projects/web/tasks/nope/move", `{"toProjectId":"ops"}`, 404},
		{"archived source", "ann", "/projects/legacy/tasks/t5/move", `{"toProjectId":"web"}`, 409},
		{"missing target field", "ivy", "/projects/web/tasks/t1/move", `{}`, 400},
		{"invalid body", "ivy", "/projects/web/tasks/t1/move", `nope`, 400},
		{"same project", "ivy", "/projects/web/tasks/t1/move", `{"toProjectId":"web"}`, 400},
		{"target not found", "ivy", "/projects/web/tasks/t1/move", `{"toProjectId":"nope"}`, 404},
		{"target other org", "ann", "/projects/web/tasks/t1/move", `{"toProjectId":"gx"}`, 404},
		{"target no role", "ivy", "/projects/web/tasks/t1/move", `{"toProjectId":"legacy"}`, 404},
		{"target viewer", "ann", "/projects/web/tasks/t1/move", `{"toProjectId":"ops"}`, 403},
		{"target archived", "ann", "/projects/web/tasks/t1/move", `{"toProjectId":"legacy"}`, 409},
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

func TestMoveResponseAndListings(t *testing.T) {
	h := newServer()
	code, body := do(t, h, "POST", "/projects/web/tasks/t1/move", "ivy", `{"toProjectId":"ops"}`)
	if code != 200 {
		t.Fatalf("got %d: %s", code, body)
	}
	var moved Task
	if err := json.Unmarshal([]byte(body), &moved); err != nil {
		t.Fatal(err)
	}
	want := Task{ID: "t1", ProjectID: "ops", Title: "Fix header", Status: "open", Assignee: "ben"}
	if moved != want {
		t.Fatalf("got %+v want %+v", moved, want)
	}

	code, body = do(t, h, "GET", "/projects/ops/tasks/t1", "ivy", "")
	if code != 200 {
		t.Fatalf("get in target: got %d: %s", code, body)
	}
	code, _ = do(t, h, "GET", "/projects/web/tasks/t1", "ivy", "")
	if code != 404 {
		t.Fatalf("get in source after move: got %d want 404", code)
	}

	var ops []Task
	_, body = do(t, h, "GET", "/projects/ops/tasks", "ivy", "")
	json.Unmarshal([]byte(body), &ops)
	found := false
	for _, task := range ops {
		if task.ID == "t1" {
			found = true
		}
	}
	if !found {
		t.Fatal("moved task missing from target listing")
	}

	var web []Task
	_, body = do(t, h, "GET", "/projects/web/tasks", "ivy", "")
	json.Unmarshal([]byte(body), &web)
	if len(web) != 2 {
		t.Fatalf("want 2 tasks left in web, got %d", len(web))
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
