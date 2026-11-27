package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
)

// Hidden grader for task 001 (bulk status). Uses only newServer() and HTTP.

type zgTask struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	Assignee  string `json:"assignee"`
}

func zgDo(h http.Handler, user, method, path, body string) (int, string) {
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	if user != "" {
		req.Header.Set("X-User", user)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func zgBulk(status string, ids ...string) string {
	b, _ := json.Marshal(map[string]any{"taskIds": ids, "status": status})
	return string(b)
}

// zgSnap reads every task via privileged readers (ann: web/ops/legacy, gus: gx).
func zgSnap(t *testing.T, h http.Handler) map[string]zgTask {
	t.Helper()
	out := map[string]zgTask{}
	for _, c := range []struct{ user, pid string }{{"ann", "web"}, {"ann", "ops"}, {"ann", "legacy"}, {"gus", "gx"}} {
		code, body := zgDo(h, c.user, "GET", "/projects/"+c.pid+"/tasks", "")
		if code != 200 {
			t.Fatalf("grader snapshot: GET %s as %s -> %d", c.pid, c.user, code)
		}
		var ts []zgTask
		if err := json.Unmarshal([]byte(body), &ts); err != nil {
			t.Fatalf("grader snapshot decode: %v", err)
		}
		for _, x := range ts {
			out[x.ID] = x
		}
	}
	return out
}

func zgDiff(a, b map[string]zgTask) string {
	var d []string
	for id, x := range a {
		y, ok := b[id]
		if !ok {
			d = append(d, id+" deleted")
		} else if x != y {
			d = append(d, fmt.Sprintf("%s %+v -> %+v", id, x, y))
		}
	}
	for id := range b {
		if _, ok := a[id]; !ok {
			d = append(d, id+" created")
		}
	}
	sort.Strings(d)
	return strings.Join(d, "; ")
}

var zgReadable = map[string][]string{
	"ann": {"web", "ops", "legacy"},
	"ben": {"web", "legacy"},
	"cat": {"web", "ops"},
	"dan": {"web"},
	"eve": {"web", "ops", "legacy"},
	"gus": {"gx"},
}

// zgLeaks returns titles of seeded tasks the caller cannot read that appear in body.
func zgLeaks(user, body string, seed map[string]zgTask) []string {
	ok := map[string]bool{}
	for _, p := range zgReadable[user] {
		ok[p] = true
	}
	var l []string
	for _, tk := range seed {
		if !ok[tk.ProjectID] && strings.Contains(body, tk.Title) {
			l = append(l, tk.Title)
		}
	}
	return l
}

// zgDenied: request must be non-2xx, state unchanged, no leak. Returns code.
func zgDenied(t *testing.T, user, pid, body string) int {
	t.Helper()
	h := newServer()
	before := zgSnap(t, h)
	code, resp := zgDo(h, user, "PATCH", "/projects/"+pid+"/tasks", body)
	after := zgSnap(t, h)
	if code >= 200 && code < 300 {
		t.Errorf("unexpected acceptance: status %d body=%s", code, resp)
	}
	if d := zgDiff(before, after); d != "" {
		t.Errorf("state changed: %s", d)
	}
	if l := zgLeaks(user, resp, before); len(l) > 0 {
		t.Errorf("leak: response body exposes titles %v: %s", l, resp)
	}
	return code
}

type zgCase struct {
	name   string
	user   string
	pid    string
	body   string
	want   int // 0 = ambiguous, no CONTRACT test
	atomic bool
}

func TestGrader(t *testing.T) {
	// ---------- REG ----------
	t.Run("REG_ListAsManager", func(t *testing.T) {
		h := newServer()
		code, body := zgDo(h, "ann", "GET", "/projects/web/tasks", "")
		var ts []zgTask
		json.Unmarshal([]byte(body), &ts)
		if code != 200 || len(ts) != 3 {
			t.Fatalf("code=%d n=%d", code, len(ts))
		}
	})
	for _, c := range []struct {
		n, user, pid string
		want         int
	}{
		{"ListViewer", "dan", "web", 200}, {"ListOrgAdmin", "eve", "web", 200},
		{"ListNonMember404", "hal", "web", 404}, {"ListOtherOrg404", "gus", "web", 404},
		{"ListAnon401", "", "web", 401}, {"ListUnknown401", "zed", "web", 401},
		{"ListArchivedStillReadable", "ann", "legacy", 200},
	} {
		c := c
		t.Run("REG_"+c.n, func(t *testing.T) {
			code, _ := zgDo(newServer(), c.user, "GET", "/projects/"+c.pid+"/tasks", "")
			if code != c.want {
				t.Fatalf("got %d want %d", code, c.want)
			}
		})
	}
	t.Run("REG_GetOwnTask", func(t *testing.T) {
		code, _ := zgDo(newServer(), "ben", "GET", "/projects/web/tasks/t1", "")
		if code != 200 {
			t.Fatalf("got %d", code)
		}
	})
	t.Run("REG_GetTaskWrongProject404", func(t *testing.T) {
		code, _ := zgDo(newServer(), "ann", "GET", "/projects/web/tasks/t4", "")
		if code != 404 {
			t.Fatalf("got %d", code)
		}
	})
	t.Run("REG_SinglePatchContributorOwn", func(t *testing.T) {
		h := newServer()
		code, _ := zgDo(h, "ben", "PATCH", "/projects/web/tasks/t1", `{"status":"done"}`)
		s := zgSnap(t, h)
		if code != 200 || s["t1"].Status != "done" {
			t.Fatalf("code=%d status=%s", code, s["t1"].Status)
		}
	})
	t.Run("REG_SinglePatchManagerAny", func(t *testing.T) {
		h := newServer()
		code, _ := zgDo(h, "ann", "PATCH", "/projects/web/tasks/t2", `{"status":"done"}`)
		s := zgSnap(t, h)
		if code != 200 || s["t2"].Status != "done" {
			t.Fatalf("code=%d status=%s", code, s["t2"].Status)
		}
	})
	for _, c := range []struct {
		n, user, pid, tid, body string
		want                    int
	}{
		{"SinglePatchContributorOthers403", "ben", "web", "t2", `{"status":"done"}`, 403},
		{"SinglePatchViewer403", "dan", "web", "t1", `{"status":"done"}`, 403},
		{"SinglePatchOrgAdmin403", "eve", "web", "t1", `{"status":"done"}`, 403},
		{"SinglePatchArchived409", "ann", "legacy", "t5", `{"status":"done"}`, 409},
		{"SinglePatchWrongProject404", "ann", "web", "t4", `{"status":"done"}`, 404},
		{"SinglePatchInvalidStatus400", "ann", "web", "t1", `{"status":"bogus"}`, 400},
		{"SinglePatchContributorReassign403", "ben", "web", "t1", `{"assignee":"cat"}`, 403},
		{"SinglePatchNonMember404", "hal", "web", "t1", `{"status":"done"}`, 404},
		{"SinglePatchAnon401", "", "web", "t1", `{"status":"done"}`, 401},
		{"DeleteContributor403", "ben", "web", "t1", "", 403},
		{"DeleteManager204", "ann", "web", "t3", "", 204},
	} {
		c := c
		t.Run("REG_"+c.n, func(t *testing.T) {
			m := "PATCH"
			if c.body == "" {
				m = "DELETE"
			}
			code, _ := zgDo(newServer(), c.user, m, "/projects/"+c.pid+"/tasks/"+c.tid, c.body)
			if code != c.want {
				t.Fatalf("got %d want %d", code, c.want)
			}
		})
	}
	t.Run("REG_CreateTask", func(t *testing.T) {
		code, _ := zgDo(newServer(), "ann", "POST", "/projects/web/tasks", `{"title":"x","assignee":"ben"}`)
		if code != 201 {
			t.Fatalf("got %d", code)
		}
	})

	// ---------- FUNC ----------
	check := func(t *testing.T, h http.Handler, user, pid, body string, ids []string, status string) {
		t.Helper()
		before := zgSnap(t, h)
		code, resp := zgDo(h, user, "PATCH", "/projects/"+pid+"/tasks", body)
		if code != 200 {
			t.Fatalf("status %d (want 200) body=%s", code, resp)
		}
		var got []zgTask
		if err := json.Unmarshal([]byte(resp), &got); err != nil {
			t.Fatalf("response is not a JSON array of tasks: %v body=%s", err, resp)
		}
		if len(got) != len(ids) {
			t.Fatalf("response has %d tasks, want %d: %s", len(got), len(ids), resp)
		}
		after := zgSnap(t, h)
		want := map[string]bool{}
		for _, id := range ids {
			want[id] = true
		}
		for _, g := range got {
			if !want[g.ID] {
				t.Errorf("response contains unrequested task %s", g.ID)
			}
			if g.Status != status {
				t.Errorf("response task %s status=%q want %q", g.ID, g.Status, status)
			}
			if g.Title != before[g.ID].Title || g.Assignee != before[g.ID].Assignee || g.ProjectID != before[g.ID].ProjectID {
				t.Errorf("response task %s has altered fields: %+v", g.ID, g)
			}
		}
		for id, b := range before {
			a := after[id]
			if want[id] {
				b.Status = status
			}
			if a != b {
				t.Errorf("state of %s: got %+v want %+v", id, a, b)
			}
		}
	}
	t.Run("FUNC_ManagerBulkOwnProject", func(t *testing.T) {
		check(t, newServer(), "ann", "web", zgBulk("done", "t1", "t2"), []string{"t1", "t2"}, "done")
	})
	t.Run("FUNC_ManagerBulkAllThree", func(t *testing.T) {
		check(t, newServer(), "ann", "web", zgBulk("doing", "t1", "t2", "t3"), []string{"t1", "t2", "t3"}, "doing")
	})
	t.Run("FUNC_ManagerBulkSingleID", func(t *testing.T) {
		check(t, newServer(), "ann", "web", zgBulk("done", "t3"), []string{"t3"}, "done")
	})
	for _, st := range []string{"open", "doing", "done"} {
		st := st
		t.Run("FUNC_StatusValue_"+st, func(t *testing.T) {
			check(t, newServer(), "ann", "web", zgBulk(st, "t1", "t2"), []string{"t1", "t2"}, st)
		})
	}
	t.Run("FUNC_ContributorBulkOwnOnly", func(t *testing.T) {
		check(t, newServer(), "ben", "web", zgBulk("done", "t1"), []string{"t1"}, "done")
	})
	t.Run("FUNC_ContributorCatOps", func(t *testing.T) {
		check(t, newServer(), "cat", "ops", zgBulk("done", "t4"), []string{"t4"}, "done")
	})
	t.Run("FUNC_ContributorMultipleOwn", func(t *testing.T) {
		h := newServer()
		ids := []string{"t1"}
		for i := 0; i < 2; i++ {
			code, body := zgDo(h, "ben", "POST", "/projects/web/tasks", fmt.Sprintf(`{"title":"mine%d","assignee":"ben"}`, i))
			var tk zgTask
			json.Unmarshal([]byte(body), &tk)
			if code != 201 || tk.ID == "" {
				t.Fatalf("setup create failed %d %s", code, body)
			}
			ids = append(ids, tk.ID)
		}
		check(t, h, "ben", "web", zgBulk("doing", ids...), ids, "doing")
	})
	t.Run("FUNC_OtherOrgManager", func(t *testing.T) {
		check(t, newServer(), "gus", "gx", zgBulk("done", "t6"), []string{"t6"}, "done")
	})
	t.Run("FUNC_ReadBackViaGet", func(t *testing.T) {
		h := newServer()
		zgDo(h, "ann", "PATCH", "/projects/web/tasks", zgBulk("done", "t1", "t3"))
		code, body := zgDo(h, "dan", "GET", "/projects/web/tasks/t3", "")
		var tk zgTask
		json.Unmarshal([]byte(body), &tk)
		if code != 200 || tk.Status != "done" {
			t.Fatalf("code=%d task=%+v", code, tk)
		}
	})

	// ---------- AUTHZ / ATOM / CONTRACT ----------
	cases := []zgCase{
		// pure denials
		{"ContributorOthersTaskOnly", "ben", "web", zgBulk("done", "t2"), 403, false},
		{"ContributorUnassignedTask", "ben", "web", zgBulk("done", "t3"), 403, false},
		{"ContributorCatOnBensTask", "cat", "web", zgBulk("done", "t1"), 403, false},
		{"ViewerWeb", "dan", "web", zgBulk("done", "t1"), 403, false},
		{"ViewerOpsAsAnn", "ann", "ops", zgBulk("done", "t4"), 403, false},
		{"OrgAdmin", "eve", "web", zgBulk("done", "t1", "t2"), 403, false},
		{"NonMember", "hal", "web", zgBulk("done", "t1"), 404, false},
		{"OtherOrgUser", "gus", "web", zgBulk("done", "t1"), 404, false},
		{"AnnOnOtherOrgProject", "ann", "gx", zgBulk("done", "t6"), 404, false},
		{"Anonymous", "", "web", zgBulk("done", "t1"), 401, false},
		{"UnknownUser", "zed", "web", zgBulk("done", "t1"), 401, false},
		{"NoSuchProject", "ann", "nope", zgBulk("done", "t1"), 404, false},
		{"TaskFromOtherProjectOnly", "ann", "web", zgBulk("done", "t4"), 404, false},
		{"TaskFromOtherOrgOnly", "ann", "web", zgBulk("done", "t6"), 404, false},
		{"NonexistentIDOnly", "ann", "web", zgBulk("done", "t999"), 404, false},
		{"ArchivedManager", "ann", "legacy", zgBulk("done", "t5"), 409, false},
		{"ArchivedContributorOwn", "ben", "legacy", zgBulk("done", "t5"), 409, false},
		// atomicity: mixed batches
		{"ContributorOwnPlusOthers", "ben", "web", zgBulk("done", "t1", "t2"), 403, true},
		{"ContributorOthersPlusOwn", "ben", "web", zgBulk("done", "t2", "t1"), 403, true},
		{"ContributorThreeOneOwn", "cat", "web", zgBulk("done", "t2", "t1", "t3"), 403, true},
		{"ManagerPlusOtherProjectTask", "ann", "web", zgBulk("done", "t1", "t4"), 404, true},
		{"ManagerOtherProjectTaskFirst", "ann", "web", zgBulk("done", "t4", "t1"), 404, true},
		{"ManagerPlusOtherOrgTask", "ann", "web", zgBulk("done", "t1", "t6"), 404, true},
		{"ManagerPlusNonexistentLast", "ann", "web", zgBulk("done", "t1", "t2", "t999"), 404, true},
		{"ManagerNonexistentFirst", "ann", "web", zgBulk("done", "t999", "t1"), 404, true},
		{"ContributorOwnPlusOtherProject", "ben", "web", zgBulk("done", "t1", "t4"), 0, true},
		{"ContributorOwnPlusOtherOrg", "ben", "web", zgBulk("done", "t1", "t6"), 0, true},
	}
	for _, c := range cases {
		c := c
		cat := "AUTHZ_"
		if c.atomic {
			cat = "ATOM_"
		}
		t.Run(cat+c.name, func(t *testing.T) { zgDenied(t, c.user, c.pid, c.body) })
		if c.want != 0 {
			t.Run("CONTRACT_"+c.name, func(t *testing.T) {
				h := newServer()
				code, body := zgDo(h, c.user, "PATCH", "/projects/"+c.pid+"/tasks", c.body)
				if code != c.want {
					t.Fatalf("got %d want %d body=%s", code, c.want, body)
				}
			})
		}
	}

	// ---------- VALID ----------
	vcases := []struct {
		name, body string
	}{
		{"InvalidStatus", zgBulk("finished", "t1", "t2")},
		{"EmptyStatus", zgBulk("", "t1", "t2")},
		{"MissingStatus", `{"taskIds":["t1","t2"]}`},
		{"UppercaseStatus", zgBulk("DONE", "t1")},
		{"MalformedJSON", `{"taskIds":["t1","t2"],"status":`},
		{"NotJSON", `status=done`},
		{"TaskIdsNotArray", `{"taskIds":"t1","status":"done"}`},
		{"TaskIdsWrongElemType", `{"taskIds":[1,2],"status":"done"}`},
		{"MissingTaskIds", `{"status":"done"}`},
	}
	for _, c := range vcases {
		c := c
		t.Run("VALID_"+c.name, func(t *testing.T) {
			h := newServer()
			before := zgSnap(t, h)
			code, body := zgDo(h, "ann", "PATCH", "/projects/web/tasks", c.body)
			if code != 400 {
				t.Errorf("got %d want 400 body=%s", code, body)
			}
			if d := zgDiff(before, zgSnap(t, h)); d != "" {
				t.Errorf("state changed: %s", d)
			}
		})
	}

	// ---------- INFO (never fails; ambiguous contract) ----------
	t.Run("INFO_EmptyTaskIds", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		code, body := zgDo(h, "ann", "PATCH", "/projects/web/tasks", zgBulk("done"))
		t.Logf("INFO empty taskIds: status=%d body=%s changed=%q", code, strings.TrimSpace(body), zgDiff(before, zgSnap(t, h)))
	})
	t.Run("INFO_DuplicateIDs", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		code, body := zgDo(h, "ann", "PATCH", "/projects/web/tasks", zgBulk("done", "t1", "t1"))
		t.Logf("INFO duplicate ids: status=%d body=%s changed=%q", code, strings.TrimSpace(body), zgDiff(before, zgSnap(t, h)))
	})
}
