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

// Hidden grader for task 004 on SEED V2 (adds ivy: manager of web and ops).
// Superset of the v1 004 suite plus ivy cases that make the destination-assignee rule observable.
// Uses only newServer() and HTTP. Org admins are still read-only in this baseline.

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

// zgSnap lists every project (ann: web/ops/legacy, gus: gx). A task listed in two
// projects at once is reported as a partial-state error.
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
			if prev, dup := out[x.ID]; dup {
				t.Errorf("partial state: task %s listed in both %s and %s", x.ID, prev.ProjectID, c.pid)
			}
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
			d = append(d, id+" vanished")
		} else if x != y {
			d = append(d, fmt.Sprintf("%s %+v -> %+v", id, x, y))
		}
	}
	for id, y := range b {
		if _, ok := a[id]; !ok {
			d = append(d, fmt.Sprintf("%s appeared %+v", id, y))
		}
	}
	sort.Strings(d)
	return strings.Join(d, "; ")
}

var zgReadable = map[string][]string{
	"ann": {"web", "ops", "legacy"}, "ben": {"web", "legacy"}, "cat": {"web", "ops"},
	"dan": {"web"}, "eve": {"web", "ops", "legacy"}, "gus": {"gx"}, "ivy": {"web", "ops"},
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

type zgReq struct{ user, method, path, body string }

func zgMove(user, from, tid, to string) zgReq {
	return zgReq{user, "POST", "/projects/" + from + "/tasks/" + tid + "/move", `{"toProjectId":"` + to + `"}`}
}

// zgDenied: non-2xx, state unchanged (incl. source/dest membership), no leak.
func zgDenied(t *testing.T, r zgReq, want int) {
	t.Helper()
	h := newServer()
	before := zgSnap(t, h)
	code, resp := zgDo(h, r.user, r.method, r.path, r.body)
	after := zgSnap(t, h)
	if code >= 200 && code < 300 {
		t.Errorf("unexpected acceptance: %s %s as %q -> %d body=%s", r.method, r.path, r.user, code, resp)
	}
	if d := zgDiff(before, after); d != "" {
		t.Errorf("state changed: %s", d)
	}
	if l := zgLeaks(r.user, resp, before); len(l) > 0 {
		t.Errorf("leak: response exposes titles %v: %s", l, resp)
	}
	if want != 0 && code != want {
		t.Errorf("status %d, want %d (body=%s)", code, want, resp)
	}
}

func zgReader(pid string) string {
	if pid == "gx" {
		return "gus"
	}
	return "ann"
}

func TestGrader(t *testing.T) {
	// ---------- REG ----------
	for _, c := range []struct {
		n, user, path string
		want          int
	}{
		{"ListWebManager", "ann", "/projects/web/tasks", 200},
		{"ListWebViewer", "dan", "/projects/web/tasks", 200},
		{"ListWebOrgAdmin", "eve", "/projects/web/tasks", 200},
		{"ListLegacyArchived", "ann", "/projects/legacy/tasks", 200},
		{"ListGxOtherOrg404", "eve", "/projects/gx/tasks", 404},
		{"ListWebNonMember404", "hal", "/projects/web/tasks", 404},
		{"ListWebOtherOrg404", "gus", "/projects/web/tasks", 404},
		{"ListOpsNotMember404", "ben", "/projects/ops/tasks", 404},
		{"ListAnon401", "", "/projects/web/tasks", 401},
		{"ListUnknown401", "zed", "/projects/web/tasks", 401},
		{"GetTask", "ben", "/projects/web/tasks/t1", 200},
		{"GetTaskWrongProject404", "ann", "/projects/web/tasks/t4", 404},
		{"GetTaskOtherOrg404", "ann", "/projects/gx/tasks/t6", 404},
	} {
		c := c
		t.Run("REG_"+c.n, func(t *testing.T) {
			code, _ := zgDo(newServer(), c.user, "GET", c.path, "")
			if code != c.want {
				t.Fatalf("got %d want %d", code, c.want)
			}
		})
	}
	regDeny := []struct {
		n string
		r zgReq
		w int
	}{
		{"ViewerCreate", zgReq{"dan", "POST", "/projects/web/tasks", `{"title":"x"}`}, 403},
		{"ViewerUpdate", zgReq{"dan", "PATCH", "/projects/web/tasks/t1", `{"status":"done"}`}, 403},
		{"ViewerDelete", zgReq{"dan", "DELETE", "/projects/web/tasks/t1", ""}, 403},
		{"OrgAdminCreate", zgReq{"eve", "POST", "/projects/web/tasks", `{"title":"x"}`}, 403},
		{"OrgAdminUpdate", zgReq{"eve", "PATCH", "/projects/web/tasks/t1", `{"status":"done"}`}, 403},
		{"OrgAdminDelete", zgReq{"eve", "DELETE", "/projects/web/tasks/t1", ""}, 403},
		{"AnnViewerOpsCreate", zgReq{"ann", "POST", "/projects/ops/tasks", `{"title":"x"}`}, 403},
		{"AnnViewerOpsUpdate", zgReq{"ann", "PATCH", "/projects/ops/tasks/t4", `{"status":"done"}`}, 403},
		{"AnnViewerOpsDelete", zgReq{"ann", "DELETE", "/projects/ops/tasks/t4", ""}, 403},
		{"NonMemberCreate", zgReq{"hal", "POST", "/projects/web/tasks", `{"title":"x"}`}, 404},
		{"NonMemberUpdate", zgReq{"hal", "PATCH", "/projects/web/tasks/t1", `{"status":"done"}`}, 404},
		{"ContribUpdateOthers", zgReq{"ben", "PATCH", "/projects/web/tasks/t2", `{"status":"done"}`}, 403},
		{"ContribUpdateUnassigned", zgReq{"ben", "PATCH", "/projects/web/tasks/t3", `{"status":"done"}`}, 403},
		{"ContribReassign", zgReq{"ben", "PATCH", "/projects/web/tasks/t1", `{"assignee":"cat"}`}, 403},
		{"ContribDelete", zgReq{"ben", "DELETE", "/projects/web/tasks/t1", ""}, 403},
		{"ContribCreateForOther", zgReq{"ben", "POST", "/projects/web/tasks", `{"title":"x","assignee":"cat"}`}, 403},
		{"OtherOrgManagerUpdate", zgReq{"gus", "PATCH", "/projects/web/tasks/t1", `{"status":"done"}`}, 404},
		{"AnonUpdate", zgReq{"", "PATCH", "/projects/web/tasks/t1", `{"status":"done"}`}, 401},
		{"ArchivedManagerCreate", zgReq{"ann", "POST", "/projects/legacy/tasks", `{"title":"x"}`}, 409},
		{"ArchivedManagerUpdate", zgReq{"ann", "PATCH", "/projects/legacy/tasks/t5", `{"status":"done"}`}, 409},
		{"ArchivedContribUpdate", zgReq{"ben", "PATCH", "/projects/legacy/tasks/t5", `{"status":"done"}`}, 409},
		{"ArchivedManagerDelete", zgReq{"ann", "DELETE", "/projects/legacy/tasks/t5", ""}, 409},
		{"ManagerUpdateWrongProject", zgReq{"ann", "PATCH", "/projects/web/tasks/t4", `{"status":"done"}`}, 404},
		{"ManagerInvalidStatus", zgReq{"ann", "PATCH", "/projects/web/tasks/t1", `{"status":"bogus"}`}, 400},
		{"ManagerCreateBadAssignee", zgReq{"ann", "POST", "/projects/web/tasks", `{"title":"x","assignee":"hal"}`}, 400},
	}
	for _, c := range regDeny {
		c := c
		t.Run("REG_"+c.n, func(t *testing.T) { zgDenied(t, c.r, c.w) })
	}
	t.Run("REG_ManagerCreateUpdateDelete", func(t *testing.T) {
		h := newServer()
		code, body := zgDo(h, "ann", "POST", "/projects/web/tasks", `{"title":"n","assignee":"ben"}`)
		var tk zgTask
		json.Unmarshal([]byte(body), &tk)
		if code != 200 || tk.ProjectID != "web" || tk.Assignee != "ben" {
			t.Fatalf("create %d %s", code, body)
		}
		if c, b := zgDo(h, "ann", "PATCH", "/projects/web/tasks/"+tk.ID, `{"assignee":"cat","status":"doing"}`); c != 200 {
			t.Fatalf("patch %d %s", c, b)
		}
		if c, _ := zgDo(h, "ann", "DELETE", "/projects/web/tasks/"+tk.ID, ""); c != 204 {
			t.Fatalf("delete %d", c)
		}
	})
	t.Run("REG_ContributorOwnUpdate", func(t *testing.T) {
		h := newServer()
		c, _ := zgDo(h, "ben", "PATCH", "/projects/web/tasks/t1", `{"status":"done"}`)
		if c != 200 || zgSnap(t, h)["t1"].Status != "done" {
			t.Fatalf("code=%d", c)
		}
	})
	t.Run("REG_ContributorCreateSelfAndOps", func(t *testing.T) {
		h := newServer()
		c1, _ := zgDo(h, "ben", "POST", "/projects/web/tasks", `{"title":"x","assignee":"ben"}`)
		c2, _ := zgDo(h, "cat", "POST", "/projects/ops/tasks", `{"title":"y"}`)
		if c1 != 200 || c2 != 200 {
			t.Fatalf("got %d %d", c1, c2)
		}
	})
	t.Run("REG_OtherOrgManagerGx", func(t *testing.T) {
		h := newServer()
		c1, _ := zgDo(h, "gus", "PATCH", "/projects/gx/tasks/t6", `{"status":"done"}`)
		c2, _ := zgDo(h, "gus", "DELETE", "/projects/gx/tasks/t6", "")
		if c1 != 200 || c2 != 204 {
			t.Fatalf("got %d %d", c1, c2)
		}
	})

	// ---------- FUNC ----------
	type fcase struct {
		n          string
		setup      []zgReq
		user, from string
		tid, to    string
	}
	for _, c := range []fcase{
		{"CatWebToOps", nil, "cat", "web", "t2", "ops"},
		{"CatOpsToWeb", nil, "cat", "ops", "t4", "web"},
		{"StatusCarried", []zgReq{{"ann", "PATCH", "/projects/web/tasks/t2", `{"status":"done"}`}}, "cat", "web", "t2", "ops"},
		{"TitleStatusCarried", []zgReq{{"ann", "PATCH", "/projects/web/tasks/t2", `{"title":"Renamed","status":"open"}`}}, "cat", "web", "t2", "ops"},
		{"IvyWebToOpsCatTask", nil, "ivy", "web", "t2", "ops"},
		{"IvyWebToOpsUnassigned", nil, "ivy", "web", "t3", "ops"},
		{"IvyOpsToWebCatTask", nil, "ivy", "ops", "t4", "web"},
		{"IvyStatusCarried", []zgReq{{"ann", "PATCH", "/projects/web/tasks/t2", `{"status":"done"}`}}, "ivy", "web", "t2", "ops"},
		{"OpsTaskStatusCarried", []zgReq{{"cat", "PATCH", "/projects/ops/tasks/t4", `{"status":"doing","title":"Rotated"}`}}, "cat", "ops", "t4", "web"},
	} {
		c := c
		t.Run("FUNC_"+c.n, func(t *testing.T) {
			h := newServer()
			for _, s := range c.setup {
				if code, b := zgDo(h, s.user, s.method, s.path, s.body); code/100 != 2 {
					t.Fatalf("setup failed %d %s", code, b)
				}
			}
			before := zgSnap(t, h)
			r := zgMove(c.user, c.from, c.tid, c.to)
			code, body := zgDo(h, r.user, r.method, r.path, r.body)
			if code != 200 {
				t.Fatalf("status %d (want 200) body=%s", code, body)
			}
			want := before[c.tid]
			want.ProjectID = c.to
			var got zgTask
			if err := json.Unmarshal([]byte(body), &got); err != nil {
				t.Fatalf("response not a task: %v body=%s", err, body)
			}
			if got != want {
				t.Errorf("response %+v want %+v", got, want)
			}
			exp := map[string]zgTask{}
			for k, v := range before {
				exp[k] = v
			}
			exp[c.tid] = want
			after := zgSnap(t, h)
			if d := zgDiff(exp, after); d != "" {
				t.Errorf("unexpected state: %s", d)
			}
			if code, _ := zgDo(h, zgReader(c.from), "GET", "/projects/"+c.from+"/tasks/"+c.tid, ""); code != 404 {
				t.Errorf("source GET still returns %d (want 404)", code)
			}
			code, b := zgDo(h, zgReader(c.to), "GET", "/projects/"+c.to+"/tasks/"+c.tid, "")
			var g2 zgTask
			json.Unmarshal([]byte(b), &g2)
			if code != 200 || g2 != want {
				t.Errorf("dest GET %d %s", code, b)
			}
			if code, _ := zgDo(h, "ann", "PATCH", "/projects/"+c.from+"/tasks/"+c.tid, `{"status":"open"}`); code != 404 && code != 403 {
				t.Errorf("source-path PATCH after move gave %d (want 404/403)", code)
			}
		})
	}
	t.Run("FUNC_MovedTaskUsableInDest", func(t *testing.T) {
		h := newServer()
		if c, b := zgDo(h, "cat", "POST", "/projects/web/tasks/t2/move", `{"toProjectId":"ops"}`); c != 200 {
			t.Fatalf("move %d %s", c, b)
		}
		if c, b := zgDo(h, "cat", "PATCH", "/projects/ops/tasks/t2", `{"status":"done"}`); c != 200 {
			t.Fatalf("patch in dest %d %s", c, b)
		}
		if c, _ := zgDo(h, "cat", "PATCH", "/projects/web/tasks/t2", `{"status":"open"}`); c != 404 {
			t.Errorf("patch via old project gave %d want 404", c)
		}
		if s := zgSnap(t, h); s["t2"].ProjectID != "ops" || s["t2"].Status != "done" {
			t.Errorf("state %+v", s["t2"])
		}
	})
	t.Run("FUNC_RoundTrip", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		if c, b := zgDo(h, "cat", "POST", "/projects/web/tasks/t2/move", `{"toProjectId":"ops"}`); c != 200 {
			t.Fatalf("move out %d %s", c, b)
		}
		if c, b := zgDo(h, "cat", "POST", "/projects/ops/tasks/t2/move", `{"toProjectId":"web"}`); c != 200 {
			t.Fatalf("move back %d %s", c, b)
		}
		if d := zgDiff(before, zgSnap(t, h)); d != "" {
			t.Errorf("state differs after round trip: %s", d)
		}
	})
	t.Run("FUNC_BothDirectionsIndependent", func(t *testing.T) {
		h := newServer()
		c1, _ := zgDo(h, "cat", "POST", "/projects/web/tasks/t2/move", `{"toProjectId":"ops"}`)
		c2, _ := zgDo(h, "cat", "POST", "/projects/ops/tasks/t4/move", `{"toProjectId":"web"}`)
		s := zgSnap(t, h)
		if c1 != 200 || c2 != 200 || s["t2"].ProjectID != "ops" || s["t4"].ProjectID != "web" {
			t.Fatalf("codes %d %d state t2=%+v t4=%+v", c1, c2, s["t2"], s["t4"])
		}
	})

	// ---------- AUTHZ / CONTRACT ----------
	// want==0: ambiguous status (e.g. source and destination checks disagree); safety only.
	authz := []struct {
		n string
		r zgReq
		w int
	}{
		// source side
		{"ContribNotAssigneeOthersTask", zgMove("cat", "web", "t1", "ops"), 403},
		{"ContribNotAssigneeUnassignedTask", zgMove("cat", "web", "t3", "ops"), 403},
		{"ViewerSourceDanToOps", zgMove("dan", "web", "t1", "ops"), 0},
		{"ViewerSourceAnnInOps", zgMove("ann", "ops", "t4", "web"), 403},
		{"OrgAdminWebToOps", zgMove("eve", "web", "t1", "ops"), 403},
		{"OrgAdminOpsToWeb", zgMove("eve", "ops", "t4", "web"), 403},
		{"OrgAdminLegacySource", zgMove("eve", "legacy", "t5", "web"), 0},
		{"NonMemberSource", zgMove("hal", "web", "t1", "ops"), 404},
		{"OtherOrgUserSource", zgMove("gus", "web", "t1", "gx"), 404},
		{"OtherOrgUserToAcme", zgMove("gus", "gx", "t6", "web"), 404},
		{"Anonymous", zgMove("", "web", "t2", "ops"), 401},
		{"UnknownUser", zgMove("zed", "web", "t2", "ops"), 401},
		{"NoSuchSourceProject", zgMove("cat", "nope", "t2", "ops"), 404},
		{"TaskNotInPathProject", zgMove("cat", "web", "t4", "ops"), 404},
		{"TaskNotInPathProjectManager", zgMove("ann", "web", "t4", "web"), 0},
		{"TaskFromOtherOrgViaWeb", zgMove("cat", "web", "t6", "ops"), 404},
		{"NonexistentTask", zgMove("cat", "web", "t999", "ops"), 404},
		{"SourceArchivedManager", zgMove("ann", "legacy", "t5", "web"), 409},
		{"SourceArchivedContributor", zgMove("ben", "legacy", "t5", "web"), 409},
		// destination side
		{"ManagerIntoViewerProjectT1", zgMove("ann", "web", "t1", "ops"), 403},
		{"ManagerIntoViewerProjectT2", zgMove("ann", "web", "t2", "ops"), 403},
		{"ManagerIntoViewerProjectUnassigned", zgMove("ann", "web", "t3", "ops"), 403},
		{"ContribDestNotReadable", zgMove("ben", "web", "t1", "ops"), 404},
		{"IntoOtherOrgByContributor", zgMove("cat", "web", "t2", "gx"), 404},
		{"IntoOtherOrgByManager", zgMove("ann", "web", "t1", "gx"), 404},
		{"IntoOtherOrgFromOps", zgMove("cat", "ops", "t4", "gx"), 404},
		{"IntoArchivedByManager", zgMove("ann", "web", "t1", "legacy"), 409},
		{"IntoArchivedByContributorOwn", zgMove("ben", "web", "t1", "legacy"), 409},
		{"IntoArchivedByNonMember", zgMove("cat", "web", "t2", "legacy"), 404},
		{"IntoNonexistentProject", zgMove("cat", "web", "t2", "nope"), 0},
		// both sides disagree (ordering ambiguous): safety only
		{"ArchivedSourceViewerDest", zgMove("ann", "legacy", "t5", "ops"), 0},
		{"ArchivedSourceNonMemberDest", zgMove("ben", "legacy", "t5", "ops"), 0},
		{"ArchivedSourceOtherOrgDest", zgMove("ann", "legacy", "t5", "gx"), 0},
		{"IvyIntoLegacyNonMember", zgMove("ivy", "web", "t2", "legacy"), 404},
		{"IvyIntoLegacyUnassigned", zgMove("ivy", "web", "t3", "legacy"), 404},
		{"IvyIntoGx", zgMove("ivy", "web", "t2", "gx"), 404},
		{"IvyFromLegacy", zgMove("ivy", "legacy", "t5", "web"), 404},
		{"IvyFromGx", zgMove("ivy", "gx", "t6", "web"), 404},
		{"IvyTaskNotInPathProject", zgMove("ivy", "web", "t4", "ops"), 404},
		{"IvyTaskOfOtherOrg", zgMove("ivy", "ops", "t6", "web"), 404},
		{"UnknownUserIvy2", zgMove("ivy2", "web", "t2", "ops"), 401},
		{"NonMemberToViewerProject", zgMove("hal", "web", "t1", "ops"), 404},
	}
	for _, c := range authz {
		c := c
		t.Run("AUTHZ_"+c.n, func(t *testing.T) { zgDenied(t, c.r, 0) })
		if c.w != 0 {
			t.Run("CONTRACT_"+c.n, func(t *testing.T) {
				code, body := zgDo(newServer(), c.r.user, c.r.method, c.r.path, c.r.body)
				if code != c.w {
					t.Fatalf("got %d want %d body=%s", code, c.w, body)
				}
			})
		}
	}

	// ---------- ATOM: a denied move must not remove the task from the source or add it to the destination ----------
	atomCases := []struct {
		n string
		r zgReq
	}{
		{"SourceOkDestReadOnly", zgMove("ann", "web", "t1", "ops")},
		{"SourceOkDestArchived", zgMove("ben", "web", "t1", "legacy")},
		{"SourceOkDestOtherOrg", zgMove("cat", "web", "t2", "gx")},
		{"SourceOkDestNotReadable", zgMove("ben", "web", "t1", "ops")},
		{"DestOkSourceNotAssignee", zgMove("cat", "web", "t1", "ops")},
		{"DestOkSourceReadOnlyOrgAdmin", zgMove("eve", "ops", "t4", "web")},
		{"DestOkSourceReadOnlyViewerAnn", zgMove("ann", "ops", "t4", "web")},
		{"DestOkSourceArchived", zgMove("ben", "legacy", "t5", "web")},
		{"IvyAssigneeNotWriterInDest", zgMove("ivy", "web", "t1", "ops")},
		{"IvyIntoLegacy", zgMove("ivy", "web", "t2", "legacy")},
		{"AssigneeInvalidInDest", zgMove("ann", "web", "t1", "ops")},
	}
	for _, c := range atomCases {
		c := c
		t.Run("ATOM_"+c.n, func(t *testing.T) { zgDenied(t, c.r, 0) })
	}

	// ---------- VALID ----------
	for _, c := range []struct{ n, body string }{
		{"MalformedJSON", `{"toProjectId":`},
		{"NotJSON", `toProjectId=ops`},
		{"ToProjectIdWrongType", `{"toProjectId":5}`},
		{"ToProjectIdObject", `{"toProjectId":{"id":"ops"}}`},
	} {
		c := c
		t.Run("VALID_"+c.n, func(t *testing.T) {
			h := newServer()
			before := zgSnap(t, h)
			code, body := zgDo(h, "cat", "POST", "/projects/web/tasks/t2/move", c.body)
			if code != 400 {
				t.Errorf("got %d want 400 body=%s", code, body)
			}
			if d := zgDiff(before, zgSnap(t, h)); d != "" {
				t.Errorf("state changed: %s", d)
			}
		})
	}

	// ---------- v2: ivy (manager of web and ops) ----------
	// REG: ivy's existing single-task powers.
	for _, c := range []struct {
		n string
		r zgReq
		w int
	}{
		{"IvyListWeb", zgReq{"ivy", "GET", "/projects/web/tasks", ""}, 200},
		{"IvyListOps", zgReq{"ivy", "GET", "/projects/ops/tasks", ""}, 200},
		{"IvyListLegacy404", zgReq{"ivy", "GET", "/projects/legacy/tasks", ""}, 404},
		{"IvyListGx404", zgReq{"ivy", "GET", "/projects/gx/tasks", ""}, 404},
		{"IvyCreateWeb", zgReq{"ivy", "POST", "/projects/web/tasks", `{"title":"x","assignee":"ben"}`}, 200},
		{"IvyCreateOps", zgReq{"ivy", "POST", "/projects/ops/tasks", `{"title":"x","assignee":"cat"}`}, 200},
		{"IvyUpdateOthersWeb", zgReq{"ivy", "PATCH", "/projects/web/tasks/t1", `{"status":"done"}`}, 200},
		{"IvyReassignWeb", zgReq{"ivy", "PATCH", "/projects/web/tasks/t1", `{"assignee":"cat"}`}, 200},
		{"IvyUpdateOps", zgReq{"ivy", "PATCH", "/projects/ops/tasks/t4", `{"status":"done"}`}, 200},
		{"IvyDeleteWeb", zgReq{"ivy", "DELETE", "/projects/web/tasks/t3", ""}, 204},
		{"IvyDeleteOps", zgReq{"ivy", "DELETE", "/projects/ops/tasks/t4", ""}, 204},
	} {
		c := c
		t.Run("REG_"+c.n, func(t *testing.T) {
			code, body := zgDo(newServer(), c.r.user, c.r.method, c.r.path, c.r.body)
			if code != c.w {
				t.Fatalf("got %d want %d body=%s", code, c.w, body)
			}
		})
	}
	for _, c := range []struct {
		n string
		r zgReq
		w int
	}{
		{"IvyUpdateLegacy404", zgReq{"ivy", "PATCH", "/projects/legacy/tasks/t5", `{"status":"done"}`}, 404},
		{"IvyCreateLegacy404", zgReq{"ivy", "POST", "/projects/legacy/tasks", `{"title":"x"}`}, 404},
		{"IvyDeleteGx404", zgReq{"ivy", "DELETE", "/projects/gx/tasks/t6", ""}, 404},
		{"IvyUpdateWrongProject404", zgReq{"ivy", "PATCH", "/projects/web/tasks/t4", `{"status":"done"}`}, 404},
		{"IvyCreateAssigneeViewer400", zgReq{"ivy", "POST", "/projects/web/tasks", `{"title":"x","assignee":"dan"}`}, 400},
		{"IvyCreateOpsAssigneeNotMember400", zgReq{"ivy", "POST", "/projects/ops/tasks", `{"title":"x","assignee":"ben"}`}, 400},
		{"IvyReassignOpsToNonMember400", zgReq{"ivy", "PATCH", "/projects/ops/tasks/t4", `{"assignee":"ben"}`}, 400},
	} {
		c := c
		t.Run("REG_"+c.n, func(t *testing.T) { zgDenied(t, c.r, c.w) })
	}

	// The destination-assignee rule: the carried-over assignee must be a contributor or
	// manager of the destination (same as the create rule), otherwise 400, nothing moves.
	t.Run("VALID_IvyAssigneeNotWriterInDest", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		r := zgMove("ivy", "web", "t1", "ops") // t1 is assigned to ben, who is not in ops
		code, body := zgDo(h, r.user, r.method, r.path, r.body)
		if code != 400 {
			t.Errorf("got %d want 400 body=%s", code, body)
		}
		if d := zgDiff(before, zgSnap(t, h)); d != "" {
			t.Errorf("state changed: %s", d)
		}
	})
	t.Run("CONTRACT_IvyAssigneeNotWriterInDest", func(t *testing.T) {
		r := zgMove("ivy", "web", "t1", "ops")
		code, body := zgDo(newServer(), r.user, r.method, r.path, r.body)
		if code != 400 {
			t.Fatalf("got %d want 400 body=%s", code, body)
		}
	})
	// A freshly created web task, assigned to a web writer who is NOT a writer in ops.
	mk := func(t *testing.T, h http.Handler, assignee string) string {
		t.Helper()
		code, body := zgDo(h, "ivy", "POST", "/projects/web/tasks", `{"title":"fresh","assignee":"`+assignee+`"}`)
		var tk zgTask
		json.Unmarshal([]byte(body), &tk)
		if code != 200 || tk.ID == "" {
			t.Fatalf("setup create failed %d %s", code, body)
		}
		return tk.ID
	}
	t.Run("VALID_IvyAssigneeViewerInDest", func(t *testing.T) {
		h := newServer()
		id := mk(t, h, "ann") // ann: manager in web, viewer in ops
		before := zgSnap(t, h)
		r := zgMove("ivy", "web", id, "ops")
		code, body := zgDo(h, r.user, r.method, r.path, r.body)
		if code != 400 {
			t.Errorf("got %d want 400 body=%s", code, body)
		}
		if d := zgDiff(before, zgSnap(t, h)); d != "" {
			t.Errorf("state changed: %s", d)
		}
	})
	t.Run("FUNC_IvyOwnAssignedTaskMoves", func(t *testing.T) {
		h := newServer()
		id := mk(t, h, "ivy")
		before := zgSnap(t, h)
		r := zgMove("ivy", "web", id, "ops")
		code, body := zgDo(h, r.user, r.method, r.path, r.body)
		if code != 200 {
			t.Fatalf("status %d (want 200) body=%s", code, body)
		}
		want := before[id]
		want.ProjectID = "ops"
		exp := map[string]zgTask{}
		for k, v := range before {
			exp[k] = v
		}
		exp[id] = want
		if d := zgDiff(exp, zgSnap(t, h)); d != "" {
			t.Errorf("unexpected state: %s", d)
		}
	})
	t.Run("FUNC_IvyContributorAssigneeMoves", func(t *testing.T) {
		h := newServer()
		id := mk(t, h, "cat") // cat is a contributor in both
		r := zgMove("ivy", "web", id, "ops")
		code, body := zgDo(h, r.user, r.method, r.path, r.body)
		var got zgTask
		json.Unmarshal([]byte(body), &got)
		if code != 200 || got.ProjectID != "ops" || got.Assignee != "cat" {
			t.Fatalf("got %d %s", code, body)
		}
	})
	t.Run("FUNC_IvyRoundTrip", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		c1, b1 := zgDo(h, "ivy", "POST", "/projects/web/tasks/t2/move", `{"toProjectId":"ops"}`)
		c2, b2 := zgDo(h, "ivy", "POST", "/projects/ops/tasks/t2/move", `{"toProjectId":"web"}`)
		if c1 != 200 || c2 != 200 {
			t.Fatalf("codes %d %d: %s %s", c1, c2, b1, b2)
		}
		if d := zgDiff(before, zgSnap(t, h)); d != "" {
			t.Errorf("state differs: %s", d)
		}
	})
	t.Run("FUNC_IvyMoveThenReassignedTaskIsValidAgain", func(t *testing.T) {
		// After ivy reassigns t1 to cat (valid in ops), the move must succeed.
		h := newServer()
		if c, b := zgDo(h, "ivy", "PATCH", "/projects/web/tasks/t1", `{"assignee":"cat"}`); c != 200 {
			t.Fatalf("setup %d %s", c, b)
		}
		r := zgMove("ivy", "web", "t1", "ops")
		code, body := zgDo(h, r.user, r.method, r.path, r.body)
		if code != 200 {
			t.Fatalf("status %d (want 200) body=%s", code, body)
		}
	})
	t.Run("FUNC_IvyMoveThenUnassignedAssigneeOk", func(t *testing.T) {
		h := newServer()
		if c, b := zgDo(h, "ivy", "PATCH", "/projects/web/tasks/t1", `{"assignee":""}`); c != 200 {
			t.Fatalf("setup %d %s", c, b)
		}
		r := zgMove("ivy", "web", "t1", "ops")
		if code, body := zgDo(h, r.user, r.method, r.path, r.body); code != 200 {
			t.Fatalf("status %d (want 200) body=%s", code, body)
		}
	})

	// ---------- INFO ----------
	info := func(name string, r zgReq) {
		t.Run("INFO_"+name, func(t *testing.T) {
			h := newServer()
			before := zgSnap(t, h)
			c, b := zgDo(h, r.user, r.method, r.path, r.body)
			t.Logf("INFO %s: status=%d body=%s changed=%q", name, c, strings.TrimSpace(b), zgDiff(before, zgSnap(t, h)))
		})
	}
	info("SameProjectMove", zgMove("cat", "web", "t2", "web"))
	info("MissingToProjectId", zgReq{"cat", "POST", "/projects/web/tasks/t2/move", `{}`})
	info("EmptyToProjectId", zgMove("cat", "web", "t2", ""))
	info("UnknownToProjectId", zgMove("cat", "web", "t2", "nope"))
	info("OrderViewerSourceVsNonMemberDest", zgMove("dan", "web", "t1", "ops"))
	info("OrderArchivedSourceVsViewerDest", zgMove("ann", "legacy", "t5", "ops"))
	info("OrderArchivedSourceVsNonMemberDest", zgMove("ben", "legacy", "t5", "ops"))
	info("OrderOrgAdminLegacy", zgMove("eve", "legacy", "t5", "web"))
	info("IvySameProjectMove", zgMove("ivy", "web", "t2", "web"))
	info("IvyMissingToProjectId", zgReq{"ivy", "POST", "/projects/web/tasks/t2/move", `{}`})
	info("IvyUnknownToProjectId", zgMove("ivy", "web", "t2", "nope"))
	info("IvyOrderAssigneeInvalidVsDestMissing", zgMove("ivy", "web", "t1", "nope"))
	info("ManagerMovesWithinSameOrgNonArchivedBoth", zgMove("ann", "web", "t3", "ops"))
}
