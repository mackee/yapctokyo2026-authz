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

func TestBulkAssignAuth(t *testing.T) {
	cases := []struct {
		name, user, project, body string
		want                      int
	}{
		{"manager", "ann", "web", `{"taskIds":["t1","t3"],"assignee":"cat"}`, 200},
		{"manager unassign", "ann", "web", `{"taskIds":["t1"],"assignee":""}`, 200},
		{"assignee is viewer in ops", "ivy", "ops", `{"taskIds":["t4"],"assignee":"ann"}`, 400},
		{"contributor", "ben", "web", `{"taskIds":["t1"],"assignee":"cat"}`, 403},
		{"contributor unassign own", "ben", "web", `{"taskIds":["t1"],"assignee":""}`, 403},
		{"viewer", "dan", "web", `{"taskIds":["t1"],"assignee":"cat"}`, 403},
		{"org admin", "eve", "web", `{"taskIds":["t1"],"assignee":"cat"}`, 403},
		{"non member", "hal", "web", `{"taskIds":["t1"],"assignee":"cat"}`, 404},
		{"other org", "gus", "web", `{"taskIds":["t1"],"assignee":"cat"}`, 404},
		{"anonymous", "", "web", `{"taskIds":["t1"],"assignee":"cat"}`, 401},
		{"unknown user", "zed", "web", `{"taskIds":["t1"],"assignee":"cat"}`, 401},
		{"archived", "ann", "legacy", `{"taskIds":["t5"],"assignee":"ben"}`, 409},
		{"task in other project", "ann", "web", `{"taskIds":["t1","t4"],"assignee":"cat"}`, 404},
		{"unknown task", "ann", "web", `{"taskIds":["t1","t99"],"assignee":"cat"}`, 404},
		{"assignee is viewer", "ann", "web", `{"taskIds":["t1"],"assignee":"dan"}`, 400},
		{"assignee not member", "ann", "web", `{"taskIds":["t1"],"assignee":"hal"}`, 400},
		{"empty task list", "ann", "web", `{"taskIds":[],"assignee":"cat"}`, 400},
		{"missing task list", "ann", "web", `{"assignee":"cat"}`, 400},
		{"missing assignee", "ann", "web", `{"taskIds":["t1"]}`, 400},
		{"null assignee", "ann", "web", `{"taskIds":["t1"],"assignee":null}`, 400},
		{"duplicate task", "ann", "web", `{"taskIds":["t1","t1"],"assignee":"cat"}`, 400},
		{"bad json", "ann", "web", `{"taskIds":`, 400},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, _ := do(t, newServer(), "PATCH", "/projects/"+c.project+"/tasks/assignee", c.user, c.body)
			if code != c.want {
				t.Fatalf("got %d want %d", code, c.want)
			}
		})
	}
}

func TestBulkAssignResponse(t *testing.T) {
	h := newServer()
	code, body := do(t, h, "PATCH", "/projects/web/tasks/assignee", "ann", `{"taskIds":["t3","t1"],"assignee":"cat"}`)
	if code != 200 {
		t.Fatalf("got %d: %s", code, body)
	}
	var tasks []Task
	if err := json.Unmarshal([]byte(body), &tasks); err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 || tasks[0].ID != "t3" || tasks[1].ID != "t1" {
		t.Fatalf("want [t3 t1] in request order, got %+v", tasks)
	}
	for _, task := range tasks {
		if task.Assignee != "cat" || task.ProjectID != "web" {
			t.Fatalf("unexpected task after update: %+v", task)
		}
	}
	// The change is persisted, not just echoed back.
	_, body = do(t, h, "GET", "/projects/web/tasks/t1", "ann", "")
	if !strings.Contains(body, `"assignee":"cat"`) {
		t.Fatalf("t1 not persisted: %s", body)
	}
}

func TestBulkAssignUnassign(t *testing.T) {
	h := newServer()
	code, _ := do(t, h, "PATCH", "/projects/web/tasks/assignee", "ann", `{"taskIds":["t1","t2"],"assignee":""}`)
	if code != 200 {
		t.Fatalf("got %d", code)
	}
	for _, id := range []string{"t1", "t2"} {
		_, body := do(t, h, "GET", "/projects/web/tasks/"+id, "ann", "")
		if !strings.Contains(body, `"assignee":""`) {
			t.Fatalf("%s still assigned: %s", id, body)
		}
	}
}

func TestBulkAssignAllOrNothing(t *testing.T) {
	h := newServer()
	// t4 belongs to ops, so the whole request must fail and t1 must stay as it was.
	code, _ := do(t, h, "PATCH", "/projects/web/tasks/assignee", "ann", `{"taskIds":["t1","t4"],"assignee":"cat"}`)
	if code != 404 {
		t.Fatalf("got %d want 404", code)
	}
	code, _ = do(t, h, "PATCH", "/projects/web/tasks/assignee", "ann", `{"taskIds":["t1","t99"],"assignee":"cat"}`)
	if code != 404 {
		t.Fatalf("got %d want 404", code)
	}
	code, _ = do(t, h, "PATCH", "/projects/web/tasks/assignee", "ann", `{"taskIds":["t1","t3"],"assignee":"hal"}`)
	if code != 400 {
		t.Fatalf("got %d want 400", code)
	}
	for id, want := range map[string]string{"t1": "ben", "t3": "", "t4": "cat"} {
		proj := "web"
		if id == "t4" {
			proj = "ops"
		}
		_, body := do(t, h, "GET", "/projects/"+proj+"/tasks/"+id, "ann", "")
		if !strings.Contains(body, `"assignee":"`+want+`"`) {
			t.Fatalf("%s changed, want assignee %q: %s", id, want, body)
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
