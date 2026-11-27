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

// Hidden grader for task 002 (org admin may write like a manager).
// Uses only newServer() and HTTP.

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
	for id, y := range b {
		if _, ok := a[id]; !ok {
			d = append(d, fmt.Sprintf("%s created %+v", id, y))
		}
	}
	sort.Strings(d)
	return strings.Join(d, "; ")
}

var zgReadable = map[string][]string{
	"ann": {"web", "ops", "legacy"}, "ben": {"web", "legacy"}, "cat": {"web", "ops"},
	"dan": {"web"}, "eve": {"web", "ops", "legacy"}, "gus": {"gx"},
}

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

// zgDenied: non-2xx, state unchanged, no leak. If want != 0 and strict, exact code required too.
func zgDenied(t *testing.T, user, method, path, body string, want int) {
	t.Helper()
	h := newServer()
	before := zgSnap(t, h)
	code, resp := zgDo(h, user, method, path, body)
	after := zgSnap(t, h)
	if code >= 200 && code < 300 {
		t.Errorf("unexpected acceptance: %s %s as %q -> %d body=%s", method, path, user, code, resp)
	}
	if d := zgDiff(before, after); d != "" {
		t.Errorf("state changed: %s", d)
	}
	if l := zgLeaks(user, resp, before); len(l) > 0 {
		t.Errorf("leak: response exposes titles %v: %s", l, resp)
	}
	if want != 0 && code != want {
		t.Errorf("status %d, want %d (body=%s)", code, want, resp)
	}
}

type zgReq struct {
	user, method, path, body string
}

func zgJ(v any) string { b, _ := json.Marshal(v); return string(b) }

func TestGrader(t *testing.T) {
	// ---------- REG: unchanged existing behaviour (must hold on baseline and after) ----------
	// Reads.
	for _, c := range []struct {
		n, user, path string
		want          int
	}{
		{"ListWebAsManager", "ann", "/projects/web/tasks", 200},
		{"ListWebAsViewer", "dan", "/projects/web/tasks", 200},
		{"ListWebAsOrgAdmin", "eve", "/projects/web/tasks", 200},
		{"ListLegacyAsOrgAdmin", "eve", "/projects/legacy/tasks", 200},
		{"ListOpsAsOrgAdmin", "eve", "/projects/ops/tasks", 200},
		{"ListGxAsOrgAdminOtherOrg404", "eve", "/projects/gx/tasks", 404},
		{"ListWebNonMember404", "hal", "/projects/web/tasks", 404},
		{"ListWebOtherOrg404", "gus", "/projects/web/tasks", 404},
		{"ListWebAnon401", "", "/projects/web/tasks", 401},
		{"ListWebUnknown401", "zed", "/projects/web/tasks", 401},
		{"ListOpsNotMemberBen404", "ben", "/projects/ops/tasks", 404},
		{"GetTaskOrgAdmin", "eve", "/projects/web/tasks/t1", 200},
		{"GetTaskWrongProject404", "eve", "/projects/web/tasks/t4", 404},
		{"GetGxTaskAsEve404", "eve", "/projects/gx/tasks/t6", 404},
		{"GetTaskAnonymous401", "", "/projects/web/tasks/t1", 401},
	} {
		c := c
		t.Run("REG_"+c.n, func(t *testing.T) {
			code, _ := zgDo(newServer(), c.user, "GET", c.path, "")
			if code != c.want {
				t.Fatalf("got %d want %d", code, c.want)
			}
		})
	}
	// Existing denials that must not loosen: exact code AND no state change.
	regDeny := []struct {
		n string
		r zgReq
		w int
	}{
		// viewer dan
		{"ViewerCreate", zgReq{"dan", "POST", "/projects/web/tasks", `{"title":"x"}`}, 403},
		{"ViewerUpdate", zgReq{"dan", "PATCH", "/projects/web/tasks/t1", `{"status":"done"}`}, 403},
		{"ViewerReassign", zgReq{"dan", "PATCH", "/projects/web/tasks/t1", `{"assignee":"ben"}`}, 403},
		{"ViewerDelete", zgReq{"dan", "DELETE", "/projects/web/tasks/t1", ""}, 403},
		// ann viewer in ops
		{"AnnViewerOpsCreate", zgReq{"ann", "POST", "/projects/ops/tasks", `{"title":"x"}`}, 403},
		{"AnnViewerOpsUpdate", zgReq{"ann", "PATCH", "/projects/ops/tasks/t4", `{"status":"done"}`}, 403},
		{"AnnViewerOpsDelete", zgReq{"ann", "DELETE", "/projects/ops/tasks/t4", ""}, 403},
		// non-member
		{"NonMemberCreate", zgReq{"hal", "POST", "/projects/web/tasks", `{"title":"x"}`}, 404},
		{"NonMemberUpdate", zgReq{"hal", "PATCH", "/projects/web/tasks/t1", `{"status":"done"}`}, 404},
		{"NonMemberDelete", zgReq{"hal", "DELETE", "/projects/web/tasks/t1", ""}, 404},
		{"BenNotInOpsUpdate", zgReq{"ben", "PATCH", "/projects/ops/tasks/t4", `{"status":"done"}`}, 404},
		{"BenNotInOpsDelete", zgReq{"ben", "DELETE", "/projects/ops/tasks/t4", ""}, 404},
		// contributor limits
		{"ContribUpdateOthers", zgReq{"ben", "PATCH", "/projects/web/tasks/t2", `{"status":"done"}`}, 403},
		{"ContribUpdateUnassigned", zgReq{"ben", "PATCH", "/projects/web/tasks/t3", `{"status":"done"}`}, 403},
		{"ContribReassignOwn", zgReq{"ben", "PATCH", "/projects/web/tasks/t1", `{"assignee":"cat"}`}, 403},
		{"ContribDeleteOwn", zgReq{"ben", "DELETE", "/projects/web/tasks/t1", ""}, 403},
		{"ContribDeleteOthers", zgReq{"ben", "DELETE", "/projects/web/tasks/t2", ""}, 403},
		{"ContribCreateForOther", zgReq{"ben", "POST", "/projects/web/tasks", `{"title":"x","assignee":"cat"}`}, 403},
		{"CatUpdateBens", zgReq{"cat", "PATCH", "/projects/web/tasks/t1", `{"title":"hijack"}`}, 403},
		// other org / anon / unknown
		{"OtherOrgManagerCreateInAcme", zgReq{"gus", "POST", "/projects/web/tasks", `{"title":"x"}`}, 404},
		{"OtherOrgManagerUpdateInAcme", zgReq{"gus", "PATCH", "/projects/web/tasks/t1", `{"status":"done"}`}, 404},
		{"OtherOrgManagerDeleteInAcme", zgReq{"gus", "DELETE", "/projects/web/tasks/t1", ""}, 404},
		{"OtherOrgManagerUpdateOps", zgReq{"gus", "PATCH", "/projects/ops/tasks/t4", `{"status":"done"}`}, 404},
		{"AnonCreate", zgReq{"", "POST", "/projects/web/tasks", `{"title":"x"}`}, 401},
		{"AnonUpdate", zgReq{"", "PATCH", "/projects/web/tasks/t1", `{"status":"done"}`}, 401},
		{"AnonDelete", zgReq{"", "DELETE", "/projects/web/tasks/t1", ""}, 401},
		{"UnknownCreate", zgReq{"zed", "POST", "/projects/web/tasks", `{"title":"x"}`}, 401},
		{"UnknownDelete", zgReq{"zed", "DELETE", "/projects/web/tasks/t1", ""}, 401},
		{"AnnOnOtherOrgProject", zgReq{"ann", "PATCH", "/projects/gx/tasks/t6", `{"status":"done"}`}, 404},
		// archived project for real members
		{"ArchivedManagerCreate", zgReq{"ann", "POST", "/projects/legacy/tasks", `{"title":"x"}`}, 409},
		{"ArchivedManagerUpdate", zgReq{"ann", "PATCH", "/projects/legacy/tasks/t5", `{"status":"done"}`}, 409},
		{"ArchivedManagerDelete", zgReq{"ann", "DELETE", "/projects/legacy/tasks/t5", ""}, 409},
		{"ArchivedContribUpdate", zgReq{"ben", "PATCH", "/projects/legacy/tasks/t5", `{"status":"done"}`}, 409},
		// wrong-project task id for a real manager
		{"ManagerUpdateWrongProjectTask", zgReq{"ann", "PATCH", "/projects/web/tasks/t4", `{"status":"done"}`}, 404},
		{"ManagerDeleteWrongProjectTask", zgReq{"ann", "DELETE", "/projects/web/tasks/t4", ""}, 404},
		// validation
		{"ManagerInvalidStatus", zgReq{"ann", "PATCH", "/projects/web/tasks/t1", `{"status":"bogus"}`}, 400},
		{"ManagerEmptyTitle", zgReq{"ann", "PATCH", "/projects/web/tasks/t1", `{"title":""}`}, 400},
		{"ManagerReassignToViewer", zgReq{"ann", "PATCH", "/projects/web/tasks/t1", `{"assignee":"dan"}`}, 400},
		{"ManagerCreateEmptyTitle", zgReq{"ann", "POST", "/projects/web/tasks", `{"title":""}`}, 400},
		{"ManagerCreateBadAssignee", zgReq{"ann", "POST", "/projects/web/tasks", `{"title":"x","assignee":"hal"}`}, 400},
	}
	for _, c := range regDeny {
		c := c
		t.Run("REG_"+c.n, func(t *testing.T) { zgDenied(t, c.r.user, c.r.method, c.r.path, c.r.body, c.w) })
	}
	// Existing allowed behaviour.
	t.Run("REG_ManagerCreateUpdateDelete", func(t *testing.T) {
		h := newServer()
		code, body := zgDo(h, "ann", "POST", "/projects/web/tasks", `{"title":"n","assignee":"ben"}`)
		var tk zgTask
		json.Unmarshal([]byte(body), &tk)
		if code != 201 || tk.ID == "" || tk.ProjectID != "web" || tk.Status != "open" || tk.Assignee != "ben" {
			t.Fatalf("create %d %s", code, body)
		}
		if c, b := zgDo(h, "ann", "PATCH", "/projects/web/tasks/"+tk.ID, `{"assignee":"cat","status":"doing"}`); c != 200 {
			t.Fatalf("patch %d %s", c, b)
		}
		if c, _ := zgDo(h, "ann", "DELETE", "/projects/web/tasks/"+tk.ID, ""); c != 204 {
			t.Fatalf("delete %d", c)
		}
		if _, ok := zgSnap(t, h)[tk.ID]; ok {
			t.Fatalf("task still exists")
		}
	})
	t.Run("REG_ContributorOwnTaskUpdate", func(t *testing.T) {
		h := newServer()
		c, _ := zgDo(h, "ben", "PATCH", "/projects/web/tasks/t1", `{"status":"done","title":"mine"}`)
		s := zgSnap(t, h)
		if c != 200 || s["t1"].Status != "done" || s["t1"].Title != "mine" {
			t.Fatalf("code=%d %+v", c, s["t1"])
		}
	})
	t.Run("REG_ContributorCreateSelf", func(t *testing.T) {
		c, _ := zgDo(newServer(), "ben", "POST", "/projects/web/tasks", `{"title":"x","assignee":"ben"}`)
		if c != 201 {
			t.Fatalf("got %d", c)
		}
	})
	t.Run("REG_ContributorCreateUnassigned", func(t *testing.T) {
		c, _ := zgDo(newServer(), "cat", "POST", "/projects/ops/tasks", `{"title":"x"}`)
		if c != 201 {
			t.Fatalf("got %d", c)
		}
	})
	t.Run("REG_ManagerOtherOrgGusWrites", func(t *testing.T) {
		h := newServer()
		c1, _ := zgDo(h, "gus", "PATCH", "/projects/gx/tasks/t6", `{"status":"done"}`)
		c2, _ := zgDo(h, "gus", "DELETE", "/projects/gx/tasks/t6", "")
		if c1 != 200 || c2 != 204 {
			t.Fatalf("got %d %d", c1, c2)
		}
	})
	t.Run("REG_ManagerDeleteOwnProjectTask", func(t *testing.T) {
		c, _ := zgDo(newServer(), "ann", "DELETE", "/projects/web/tasks/t3", "")
		if c != 204 {
			t.Fatalf("got %d", c)
		}
	})

	// ---------- FUNC: eve behaves like a manager ----------
	type upd struct {
		n, path, body string
		mut           func(x *zgTask)
	}
	for _, u := range []upd{
		{"UpdateTitle", "/projects/web/tasks/t1", `{"title":"New title"}`, func(x *zgTask) { x.Title = "New title" }},
		{"UpdateStatusOthersTask", "/projects/web/tasks/t2", `{"status":"done"}`, func(x *zgTask) { x.Status = "done" }},
		{"UpdateUnassignedTask", "/projects/web/tasks/t3", `{"status":"doing"}`, func(x *zgTask) { x.Status = "doing" }},
		{"UpdateOpsTask", "/projects/ops/tasks/t4", `{"status":"done"}`, func(x *zgTask) { x.Status = "done" }},
		{"ReassignToContributor", "/projects/web/tasks/t1", `{"assignee":"cat"}`, func(x *zgTask) { x.Assignee = "cat" }},
		{"ReassignToManager", "/projects/web/tasks/t2", `{"assignee":"ann"}`, func(x *zgTask) { x.Assignee = "ann" }},
		{"Unassign", "/projects/web/tasks/t1", `{"assignee":""}`, func(x *zgTask) { x.Assignee = "" }},
		{"AssignUnassigned", "/projects/web/tasks/t3", `{"assignee":"ben"}`, func(x *zgTask) { x.Assignee = "ben" }},
		{"UnassignOps", "/projects/ops/tasks/t4", `{"assignee":""}`, func(x *zgTask) { x.Assignee = "" }},
		{"MultiField", "/projects/web/tasks/t2", `{"title":"T","status":"open","assignee":"ben"}`, func(x *zgTask) { x.Title, x.Status, x.Assignee = "T", "open", "ben" }},
	} {
		u := u
		t.Run("FUNC_Eve"+u.n, func(t *testing.T) {
			h := newServer()
			before := zgSnap(t, h)
			code, body := zgDo(h, "eve", "PATCH", u.path, u.body)
			if code != 200 {
				t.Fatalf("status %d (want 200) body=%s", code, body)
			}
			var got zgTask
			if err := json.Unmarshal([]byte(body), &got); err != nil {
				t.Fatalf("response not a task: %v", err)
			}
			id := u.path[strings.LastIndex(u.path, "/")+1:]
			want := before[id]
			u.mut(&want)
			if got != want {
				t.Errorf("response %+v want %+v", got, want)
			}
			after := zgSnap(t, h)
			exp := map[string]zgTask{}
			for k, v := range before {
				exp[k] = v
			}
			exp[id] = want
			if d := zgDiff(exp, after); d != "" {
				t.Errorf("unexpected state: %s", d)
			}
		})
	}
	for _, c := range []struct {
		n, pid, body, assignee string
	}{
		{"CreateWeb", "web", `{"title":"Eve task"}`, ""},
		{"CreateWebAssignContributor", "web", `{"title":"Eve task","assignee":"ben"}`, "ben"},
		{"CreateWebAssignManager", "web", `{"title":"Eve task","assignee":"ann"}`, "ann"},
		{"CreateOps", "ops", `{"title":"Eve ops","assignee":"cat"}`, "cat"},
	} {
		c := c
		t.Run("FUNC_Eve"+c.n, func(t *testing.T) {
			h := newServer()
			before := zgSnap(t, h)
			code, body := zgDo(h, "eve", "POST", "/projects/"+c.pid+"/tasks", c.body)
			if code != 201 {
				t.Fatalf("status %d (want 201) body=%s", code, body)
			}
			var got zgTask
			if err := json.Unmarshal([]byte(body), &got); err != nil || got.ID == "" {
				t.Fatalf("bad response %s", body)
			}
			if got.ProjectID != c.pid || got.Status != "open" || got.Assignee != c.assignee || !strings.HasPrefix(c.body, `{"title":"`+got.Title) {
				t.Errorf("response %+v", got)
			}
			exp := map[string]zgTask{}
			for k, v := range before {
				exp[k] = v
			}
			exp[got.ID] = got
			if d := zgDiff(exp, zgSnap(t, h)); d != "" {
				t.Errorf("unexpected state: %s", d)
			}
			c2, b2 := zgDo(h, "eve", "GET", "/projects/"+c.pid+"/tasks/"+got.ID, "")
			if c2 != 200 {
				t.Errorf("created task not readable: %d %s", c2, b2)
			}
		})
	}
	for _, c := range []struct{ n, path, id string }{
		{"DeleteUnassigned", "/projects/web/tasks/t3", "t3"},
		{"DeleteOthersTask", "/projects/web/tasks/t1", "t1"},
		{"DeleteOpsTask", "/projects/ops/tasks/t4", "t4"},
	} {
		c := c
		t.Run("FUNC_Eve"+c.n, func(t *testing.T) {
			h := newServer()
			before := zgSnap(t, h)
			code, body := zgDo(h, "eve", "DELETE", c.path, "")
			if code != 204 {
				t.Fatalf("status %d (want 204) body=%s", code, body)
			}
			exp := map[string]zgTask{}
			for k, v := range before {
				if k != c.id {
					exp[k] = v
				}
			}
			if d := zgDiff(exp, zgSnap(t, h)); d != "" {
				t.Errorf("unexpected state: %s", d)
			}
			if c2, _ := zgDo(h, "eve", "GET", c.path, ""); c2 != 404 {
				t.Errorf("deleted task still readable: %d", c2)
			}
		})
	}
	t.Run("FUNC_EveCreateUpdateDeleteRoundTrip", func(t *testing.T) {
		h := newServer()
		code, body := zgDo(h, "eve", "POST", "/projects/web/tasks", `{"title":"rt","assignee":"cat"}`)
		var tk zgTask
		json.Unmarshal([]byte(body), &tk)
		if code != 201 {
			t.Fatalf("create %d %s", code, body)
		}
		if c, b := zgDo(h, "eve", "PATCH", "/projects/web/tasks/"+tk.ID, `{"status":"done","assignee":"ben"}`); c != 200 {
			t.Fatalf("patch %d %s", c, b)
		}
		if c, b := zgDo(h, "eve", "DELETE", "/projects/web/tasks/"+tk.ID, ""); c != 204 {
			t.Fatalf("delete %d %s", c, b)
		}
		if _, ok := zgSnap(t, h)[tk.ID]; ok {
			t.Fatalf("still exists")
		}
	})

	// ---------- AUTHZ: eve's reach stays bounded; others gain nothing ----------
	authz := []struct {
		n string
		r zgReq
		w int
	}{
		// archived stays read-only for the org admin
		{"EveArchivedCreate", zgReq{"eve", "POST", "/projects/legacy/tasks", `{"title":"x"}`}, 409},
		{"EveArchivedUpdate", zgReq{"eve", "PATCH", "/projects/legacy/tasks/t5", `{"status":"done"}`}, 409},
		{"EveArchivedReassign", zgReq{"eve", "PATCH", "/projects/legacy/tasks/t5", `{"assignee":"ann"}`}, 409},
		{"EveArchivedDelete", zgReq{"eve", "DELETE", "/projects/legacy/tasks/t5", ""}, 409},
		// other org
		{"EveOtherOrgCreate", zgReq{"eve", "POST", "/projects/gx/tasks", `{"title":"x"}`}, 404},
		{"EveOtherOrgUpdate", zgReq{"eve", "PATCH", "/projects/gx/tasks/t6", `{"status":"done"}`}, 404},
		{"EveOtherOrgDelete", zgReq{"eve", "DELETE", "/projects/gx/tasks/t6", ""}, 404},
		{"EveNoSuchProjectCreate", zgReq{"eve", "POST", "/projects/nope/tasks", `{"title":"x"}`}, 404},
		// task from another project through the wrong path
		{"EveUpdateTaskOfOtherProject", zgReq{"eve", "PATCH", "/projects/web/tasks/t4", `{"status":"done"}`}, 404},
		{"EveDeleteTaskOfOtherProject", zgReq{"eve", "DELETE", "/projects/web/tasks/t4", ""}, 404},
		{"EveUpdateTaskOfOtherOrgViaWeb", zgReq{"eve", "PATCH", "/projects/web/tasks/t6", `{"status":"done"}`}, 404},
		{"EveDeleteTaskOfOtherOrgViaWeb", zgReq{"eve", "DELETE", "/projects/web/tasks/t6", ""}, 404},
		{"EveUpdateTaskOfOtherOrgViaOps", zgReq{"eve", "PATCH", "/projects/ops/tasks/t6", `{"status":"done"}`}, 404},
		{"EveUpdateNonexistentTask", zgReq{"eve", "PATCH", "/projects/web/tasks/t999", `{"status":"done"}`}, 404},
		{"EveDeleteNonexistentTask", zgReq{"eve", "DELETE", "/projects/web/tasks/t999", ""}, 404},
		// gus (manager of globex) gains nothing in acme
		{"GusCreateInWeb", zgReq{"gus", "POST", "/projects/web/tasks", `{"title":"x"}`}, 404},
		{"GusUpdateInOps", zgReq{"gus", "PATCH", "/projects/ops/tasks/t4", `{"status":"done"}`}, 404},
		{"GusDeleteInWeb", zgReq{"gus", "DELETE", "/projects/web/tasks/t1", ""}, 404},
		{"GusUpdateAcmeTaskViaGx", zgReq{"gus", "PATCH", "/projects/gx/tasks/t1", `{"status":"done"}`}, 404},
		// non-admin, non-member
		{"HalCreateOps", zgReq{"hal", "POST", "/projects/ops/tasks", `{"title":"x"}`}, 404},
		{"HalDeleteLegacy", zgReq{"hal", "DELETE", "/projects/legacy/tasks/t5", ""}, 404},
		// viewer / contributor did not gain manager powers (admin loosening bug)
		{"DanCreateOps", zgReq{"dan", "POST", "/projects/ops/tasks", `{"title":"x"}`}, 404},
		{"DanDeleteWeb", zgReq{"dan", "DELETE", "/projects/web/tasks/t3", ""}, 403},
		{"BenDeleteWeb", zgReq{"ben", "DELETE", "/projects/web/tasks/t3", ""}, 403},
		{"BenReassignOthers", zgReq{"ben", "PATCH", "/projects/web/tasks/t2", `{"assignee":"ben"}`}, 403},
		{"AnnViewerOpsReassign", zgReq{"ann", "PATCH", "/projects/ops/tasks/t4", `{"assignee":"cat"}`}, 403},
	}
	for _, c := range authz {
		c := c
		t.Run("AUTHZ_"+c.n, func(t *testing.T) { zgDenied(t, c.r.user, c.r.method, c.r.path, c.r.body, 0) })
		t.Run("CONTRACT_"+c.n, func(t *testing.T) {
			code, body := zgDo(newServer(), c.r.user, c.r.method, c.r.path, c.r.body)
			if code != c.w {
				t.Fatalf("got %d want %d body=%s", code, c.w, body)
			}
		})
	}

	// ---------- ATOM: invalid parts of a request must not partially apply ----------
	atom := []struct {
		n string
		r zgReq
	}{
		{"EveTitleOkStatusBad", zgReq{"eve", "PATCH", "/projects/web/tasks/t1", `{"title":"changed","status":"bogus"}`}},
		{"EveTitleOkAssigneeBad", zgReq{"eve", "PATCH", "/projects/web/tasks/t1", `{"title":"changed","assignee":"hal"}`}},
		{"EveStatusOkAssigneeBad", zgReq{"eve", "PATCH", "/projects/web/tasks/t2", `{"status":"done","assignee":"dan"}`}},
		{"EveStatusOkTitleEmpty", zgReq{"eve", "PATCH", "/projects/ops/tasks/t4", `{"status":"done","title":""}`}},
		{"EveCreateBadAssignee", zgReq{"eve", "POST", "/projects/web/tasks", `{"title":"x","assignee":"hal"}`}},
		{"EveCreateAssigneeFromOtherOrg", zgReq{"eve", "POST", "/projects/web/tasks", `{"title":"x","assignee":"gus"}`}},
		{"EveCreateAssigneeNonexistent", zgReq{"eve", "POST", "/projects/web/tasks", `{"title":"x","assignee":"ghost"}`}},
	}
	for _, c := range atom {
		c := c
		t.Run("ATOM_"+c.n, func(t *testing.T) { zgDenied(t, c.r.user, c.r.method, c.r.path, c.r.body, 0) })
	}

	// ---------- VALID: eve gets manager-grade validation, not a blanket 403 ----------
	valid := []struct {
		n string
		r zgReq
	}{
		{"CreateEmptyTitle", zgReq{"eve", "POST", "/projects/web/tasks", `{"title":""}`}},
		{"CreateMalformedJSON", zgReq{"eve", "POST", "/projects/web/tasks", `{"title":`}},
		{"CreateAssigneeNonMember", zgReq{"eve", "POST", "/projects/web/tasks", `{"title":"x","assignee":"hal"}`}},
		{"CreateAssigneeViewer", zgReq{"eve", "POST", "/projects/web/tasks", `{"title":"x","assignee":"dan"}`}},
		{"UpdateInvalidStatus", zgReq{"eve", "PATCH", "/projects/web/tasks/t1", `{"status":"finished"}`}},
		{"UpdateEmptyTitle", zgReq{"eve", "PATCH", "/projects/web/tasks/t1", `{"title":""}`}},
		{"UpdateMalformedJSON", zgReq{"eve", "PATCH", "/projects/web/tasks/t1", `{"status":`}},
		{"ReassignToNonMember", zgReq{"eve", "PATCH", "/projects/web/tasks/t1", `{"assignee":"hal"}`}},
		{"ReassignToViewer", zgReq{"eve", "PATCH", "/projects/web/tasks/t1", `{"assignee":"dan"}`}},
		{"ReassignToNobodyKnown", zgReq{"eve", "PATCH", "/projects/web/tasks/t1", `{"assignee":"zed"}`}},
	}
	for _, c := range valid {
		c := c
		t.Run("VALID_"+c.n, func(t *testing.T) {
			h := newServer()
			before := zgSnap(t, h)
			code, body := zgDo(h, c.r.user, c.r.method, c.r.path, c.r.body)
			if code != 400 {
				t.Errorf("got %d want 400 body=%s", code, body)
			}
			if d := zgDiff(before, zgSnap(t, h)); d != "" {
				t.Errorf("state changed: %s", d)
			}
		})
	}

	// ---------- INFO ----------
	t.Run("INFO_EveAsAssigneeOnCreate", func(t *testing.T) {
		h := newServer()
		c, b := zgDo(h, "eve", "POST", "/projects/web/tasks", `{"title":"self","assignee":"eve"}`)
		t.Logf("INFO create assignee=eve (non-member): status=%d body=%s", c, strings.TrimSpace(b))
	})
	t.Run("INFO_ReassignToEve", func(t *testing.T) {
		h := newServer()
		c, b := zgDo(h, "ann", "PATCH", "/projects/web/tasks/t1", `{"assignee":"eve"}`)
		t.Logf("INFO ann reassigns t1 to eve: status=%d body=%s", c, strings.TrimSpace(b))
		c, b = zgDo(h, "eve", "PATCH", "/projects/web/tasks/t1", `{"assignee":"eve"}`)
		t.Logf("INFO eve reassigns t1 to herself: status=%d body=%s", c, strings.TrimSpace(b))
	})
	t.Run("INFO_EveArchivedInvalidBodyOrder", func(t *testing.T) {
		c, b := zgDo(newServer(), "eve", "PATCH", "/projects/legacy/tasks/t5", `{"status":"bogus"}`)
		t.Logf("INFO eve invalid status on archived: status=%d body=%s", c, strings.TrimSpace(b))
	})
}
