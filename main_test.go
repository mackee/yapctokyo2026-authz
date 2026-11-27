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

func TestCopy(t *testing.T) {
	cases := []struct {
		name, user, path, body string
		want                   int
	}{
		{"manager copies", "ivy", "/projects/web/tasks/t2", `{"toProjectId":"ops"}`, 201},
		{"contributor copies own", "cat", "/projects/web/tasks/t2", `{"toProjectId":"ops"}`, 201},
		{"unassigned copies", "ivy", "/projects/web/tasks/t3", `{"toProjectId":"ops"}`, 201},
		{"assignee not in target", "ivy", "/projects/web/tasks/t1", `{"toProjectId":"ops"}`, 400},
		{"contributor copies task of non-target member", "cat", "/projects/web/tasks/t1", `{"toProjectId":"ops"}`, 400},
		{"viewer in target", "ann", "/projects/web/tasks/t2", `{"toProjectId":"ops"}`, 403},
		{"org admin in target", "eve", "/projects/web/tasks/t2", `{"toProjectId":"ops"}`, 403},
		{"non member source", "hal", "/projects/web/tasks/t2", `{"toProjectId":"ops"}`, 404},
		{"other org source", "gus", "/projects/web/tasks/t2", `{"toProjectId":"ops"}`, 404},
		{"other org target", "ann", "/projects/web/tasks/t3", `{"toProjectId":"gx"}`, 404},
		{"unknown target", "ivy", "/projects/web/tasks/t3", `{"toProjectId":"nope"}`, 404},
		{"archived target", "ann", "/projects/web/tasks/t3", `{"toProjectId":"legacy"}`, 409},
		{"same project", "ivy", "/projects/web/tasks/t3", `{"toProjectId":"web"}`, 400},
		{"missing toProjectId", "ivy", "/projects/web/tasks/t3", `{}`, 400},
		{"invalid body", "ivy", "/projects/web/tasks/t3", `nope`, 400},
		{"task in other project", "ann", "/projects/web/tasks/t4", `{"toProjectId":"ops"}`, 404},
		{"anonymous", "", "/projects/web/tasks/t3", `{"toProjectId":"ops"}`, 401},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, _ := do(t, newServer(), "POST", c.path+"/copy", c.user, c.body)
			if code != c.want {
				t.Fatalf("got %d want %d", code, c.want)
			}
		})
	}
}

func TestCopyContributorCannotCopyUnassignableTask(t *testing.T) {
	srv := newServer()
	// ivy (manager in web and ops) is assigned a task in web.
	code, body := do(t, srv, "POST", "/projects/web/tasks", "ann", `{"title":"for ivy","assignee":"ivy"}`)
	if code != 201 {
		t.Fatalf("setup: got %d %s", code, body)
	}
	var created Task
	json.Unmarshal([]byte(body), &created)

	// cat is a contributor in ops; copying a task assigned to ivy would
	// assign it to someone else, which contributors may not do.
	code, _ = do(t, srv, "POST", "/projects/web/tasks/"+created.ID+"/copy", "cat", `{"toProjectId":"ops"}`)
	if code != 403 {
		t.Fatalf("contributor copy to other assignee: got %d want 403", code)
	}
	code, _ = do(t, srv, "POST", "/projects/web/tasks/"+created.ID+"/copy", "ivy", `{"toProjectId":"ops"}`)
	if code != 201 {
		t.Fatalf("manager copy: got %d want 201", code)
	}
}

func TestCopyCreatesNewOpenTaskAndKeepsSource(t *testing.T) {
	srv := newServer()
	code, body := do(t, srv, "POST", "/projects/web/tasks/t2/copy", "ivy", `{"toProjectId":"ops"}`)
	if code != 201 {
		t.Fatalf("got %d %s", code, body)
	}
	var copied Task
	if err := json.Unmarshal([]byte(body), &copied); err != nil {
		t.Fatal(err)
	}
	if copied.ID == "" || copied.ID == "t2" {
		t.Fatalf("want a new ID, got %q", copied.ID)
	}
	if copied.ProjectID != "ops" {
		t.Fatalf("projectId = %q want ops", copied.ProjectID)
	}
	if copied.Title != "Update footer" {
		t.Fatalf("title = %q", copied.Title)
	}
	if copied.Assignee != "cat" {
		t.Fatalf("assignee = %q want cat", copied.Assignee)
	}
	if copied.Status != "open" {
		t.Fatalf("status = %q want open (source is doing)", copied.Status)
	}

	// The copy is visible in the target project.
	if code, _ := do(t, srv, "GET", "/projects/ops/tasks/"+copied.ID, "ivy", ""); code != 200 {
		t.Fatalf("copy not readable in target: %d", code)
	}
	// The source is unchanged.
	code, body = do(t, srv, "GET", "/projects/web/tasks/t2", "ivy", "")
	if code != 200 {
		t.Fatalf("source get: %d", code)
	}
	var src Task
	json.Unmarshal([]byte(body), &src)
	if src.ProjectID != "web" || src.Status != "doing" || src.Assignee != "cat" || src.Title != "Update footer" {
		t.Fatalf("source changed: %+v", src)
	}
	// Target now has its seeded task plus the copy.
	code, body = do(t, srv, "GET", "/projects/ops/tasks", "ivy", "")
	var tasks []Task
	json.Unmarshal([]byte(body), &tasks)
	if code != 200 || len(tasks) != 2 {
		t.Fatalf("ops tasks: code %d, got %d", code, len(tasks))
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
