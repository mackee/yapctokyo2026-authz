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

func TestMergeErrors(t *testing.T) {
	cases := []struct {
		name, user, path, body string
		want                   int
	}{
		{"anonymous", "", "/projects/ops/merge", `{"intoProjectId":"web"}`, 401},
		{"unknown user", "zed", "/projects/ops/merge", `{"intoProjectId":"web"}`, 401},
		{"non member source", "hal", "/projects/web/merge", `{"intoProjectId":"ops"}`, 404},
		{"other org source", "gus", "/projects/web/merge", `{"intoProjectId":"ops"}`, 404},
		{"missing source", "ivy", "/projects/nope/merge", `{"intoProjectId":"ops"}`, 404},
		{"target not visible", "ivy", "/projects/ops/merge", `{"intoProjectId":"legacy"}`, 404},
		{"target missing", "ivy", "/projects/ops/merge", `{"intoProjectId":"nope"}`, 404},
		{"target other org", "ivy", "/projects/ops/merge", `{"intoProjectId":"gx"}`, 404},
		{"viewer source", "ann", "/projects/ops/merge", `{"intoProjectId":"web"}`, 403},
		{"contributor source", "cat", "/projects/ops/merge", `{"intoProjectId":"web"}`, 403},
		{"org admin source", "eve", "/projects/ops/merge", `{"intoProjectId":"web"}`, 403},
		{"viewer target", "ann", "/projects/web/merge", `{"intoProjectId":"ops"}`, 403},
		{"archived target", "ann", "/projects/web/merge", `{"intoProjectId":"legacy"}`, 409},
		{"archived source", "ann", "/projects/legacy/merge", `{"intoProjectId":"web"}`, 409},
		{"into itself", "ivy", "/projects/web/merge", `{"intoProjectId":"web"}`, 400},
		{"missing target field", "ivy", "/projects/ops/merge", `{}`, 400},
		{"bad body", "ivy", "/projects/ops/merge", `not json`, 400},
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

func TestMergeMovesAllTasks(t *testing.T) {
	srv := newServer()
	code, body := do(t, srv, "POST", "/projects/ops/merge", "ivy", `{"intoProjectId":"web"}`)
	if code != 200 {
		t.Fatalf("got %d: %s", code, body)
	}
	var moved []Task
	if err := json.Unmarshal([]byte(body), &moved); err != nil {
		t.Fatal(err)
	}
	if len(moved) != 1 || moved[0].ID != "t4" || moved[0].ProjectID != "web" {
		t.Fatalf("unexpected moved tasks: %+v", moved)
	}

	_, body = do(t, srv, "GET", "/projects/web/tasks", "ivy", "")
	var web []Task
	json.Unmarshal([]byte(body), &web)
	if len(web) != 4 {
		t.Fatalf("web: want 4 tasks, got %d", len(web))
	}
	_, body = do(t, srv, "GET", "/projects/ops/tasks", "ivy", "")
	var ops []Task
	json.Unmarshal([]byte(body), &ops)
	if len(ops) != 0 {
		t.Fatalf("ops: want 0 tasks, got %d", len(ops))
	}
}

func TestMergeEmptySource(t *testing.T) {
	srv := newServer()
	if code, body := do(t, srv, "DELETE", "/projects/ops/tasks/t4", "ivy", ""); code != 204 {
		t.Fatalf("setup delete: got %d: %s", code, body)
	}
	code, body := do(t, srv, "POST", "/projects/ops/merge", "ivy", `{"intoProjectId":"web"}`)
	if code != 200 || body != "[]\n" {
		t.Fatalf("got %d %q, want 200 []", code, body)
	}
}

func TestMergeIsAllOrNothing(t *testing.T) {
	srv := newServer()
	// t1 is assigned to ben, who has no role in ops, so the whole merge must fail.
	code, body := do(t, srv, "POST", "/projects/web/merge", "ivy", `{"intoProjectId":"ops"}`)
	if code != 409 {
		t.Fatalf("got %d: %s", code, body)
	}
	if !strings.Contains(body, "t1") || strings.Contains(body, "t2") {
		t.Fatalf("error should name only the blocked task t1: %s", body)
	}

	_, body = do(t, srv, "GET", "/projects/web/tasks", "ivy", "")
	var web []Task
	json.Unmarshal([]byte(body), &web)
	if len(web) != 3 {
		t.Fatalf("web: want 3 tasks after failed merge, got %d", len(web))
	}
	for _, task := range web {
		if task.ProjectID != "web" {
			t.Fatalf("task %s moved despite failed merge", task.ID)
		}
	}
}

func TestMergeAfterBlockedTaskRemoved(t *testing.T) {
	srv := newServer()
	if code, body := do(t, srv, "DELETE", "/projects/web/tasks/t1", "ivy", ""); code != 204 {
		t.Fatalf("setup delete: got %d: %s", code, body)
	}
	// Remaining web tasks: t2 (cat, contributor in ops) and t3 (unassigned).
	code, body := do(t, srv, "POST", "/projects/web/merge", "ivy", `{"intoProjectId":"ops"}`)
	if code != 200 {
		t.Fatalf("got %d: %s", code, body)
	}
	var moved []Task
	json.Unmarshal([]byte(body), &moved)
	if len(moved) != 2 || moved[0].ID != "t2" || moved[1].ID != "t3" {
		t.Fatalf("unexpected moved tasks: %+v", moved)
	}
	for _, task := range moved {
		if task.ProjectID != "ops" {
			t.Fatalf("task %s not in ops: %s", task.ID, task.ProjectID)
		}
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
