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

func TestSetMemberRole(t *testing.T) {
	cases := []struct {
		name, user, path, body string
		want                   int
	}{
		{"manager changes", "ann", "/projects/web/members/ben", `{"role":"viewer"}`, 200},
		{"manager promotes viewer", "ann", "/projects/web/members/dan", `{"role":"manager"}`, 200},
		{"contributor caller", "ben", "/projects/web/members/cat", `{"role":"viewer"}`, 403},
		{"viewer caller", "dan", "/projects/web/members/ben", `{"role":"viewer"}`, 403},
		{"org admin caller", "eve", "/projects/web/members/ben", `{"role":"viewer"}`, 403},
		{"non member caller", "hal", "/projects/web/members/ben", `{"role":"viewer"}`, 404},
		{"other org caller", "gus", "/projects/web/members/ben", `{"role":"viewer"}`, 404},
		{"anonymous", "", "/projects/web/members/ben", `{"role":"viewer"}`, 401},
		{"unknown project", "ann", "/projects/nope/members/ben", `{"role":"viewer"}`, 404},
		{"project in other org", "ann", "/projects/gx/members/gus", `{"role":"viewer"}`, 404},
		{"archived", "ann", "/projects/legacy/members/ben", `{"role":"viewer"}`, 409},
		{"target not a member", "ann", "/projects/web/members/hal", `{"role":"viewer"}`, 404},
		{"target unknown", "ann", "/projects/web/members/zed", `{"role":"viewer"}`, 404},
		{"invalid role", "ann", "/projects/web/members/ben", `{"role":"admin"}`, 400},
		{"empty role", "ann", "/projects/web/members/ben", `{"role":""}`, 400},
		{"bad body", "ann", "/projects/web/members/ben", `{"role":`, 400},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, _ := do(t, newServer(), "PUT", c.path, c.user, c.body)
			if code != c.want {
				t.Fatalf("got %d want %d", code, c.want)
			}
		})
	}
}

func TestSetMemberRoleResponse(t *testing.T) {
	code, body := do(t, newServer(), "PUT", "/projects/web/members/ben", "ann", `{"role":"viewer"}`)
	if code != 200 {
		t.Fatalf("got %d want 200", code)
	}
	var got map[string]string
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatal(err)
	}
	if got["userId"] != "ben" || got["role"] != "viewer" {
		t.Fatalf("got %v", got)
	}
}

func TestSetMemberRoleTakesEffect(t *testing.T) {
	h := newServer()

	// ben starts as a contributor and can create tasks in web.
	if code, _ := do(t, h, "POST", "/projects/web/tasks", "ben", `{"title":"x"}`); code != 201 {
		t.Fatalf("before: create got %d want 201", code)
	}

	if code, _ := do(t, h, "PUT", "/projects/web/members/ben", "ann", `{"role":"viewer"}`); code != 200 {
		t.Fatalf("demote: got %d want 200", code)
	}

	// After demotion ben can read but no longer write.
	if code, _ := do(t, h, "GET", "/projects/web/tasks", "ben", ""); code != 200 {
		t.Fatalf("read after demote: got %d want 200", code)
	}
	if code, _ := do(t, h, "POST", "/projects/web/tasks", "ben", `{"title":"x"}`); code != 403 {
		t.Fatalf("create after demote: got %d want 403", code)
	}

	// Promoting dan to contributor lets dan write.
	if code, _ := do(t, h, "PUT", "/projects/web/members/dan", "ann", `{"role":"contributor"}`); code != 200 {
		t.Fatalf("promote: got %d want 200", code)
	}
	if code, _ := do(t, h, "POST", "/projects/web/tasks", "dan", `{"title":"x"}`); code != 201 {
		t.Fatalf("create after promote: got %d want 201", code)
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
