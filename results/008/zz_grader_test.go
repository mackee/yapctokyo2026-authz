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

// Hidden grader for task 008 (copy a task to another project).
// Uses only newServer() and HTTP. Seed v2 (ivy = manager of web and ops).
// Copy rules assumed: source must be readable (task belongs to path project);
// destination must satisfy the create rules (readable, writable, not archived,
// assignee valid in destination, contributors only assign to self/unassigned).

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

// zgSnap lists every project. A task listed in two projects at once is a partial-state error.
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

func zgCopy(user, from, tid, to string) zgReq {
	return zgReq{user, "POST", "/projects/" + from + "/tasks/" + tid + "/copy", `{"toProjectId":"` + to + `"}`}
}

func zgNoop(h http.Handler) {}

// zgDeniedSetup: non-2xx, no state change anywhere (re-lists every project), no leak.
// setup runs against the fresh server before the "before" snapshot.
func zgDeniedSetup(t *testing.T, setup func(h http.Handler), r zgReq, want int) {
	t.Helper()
	h := newServer()
	if setup != nil {
		setup(h)
	}
	before := zgSnap(t, h)
	code, resp := zgDo(h, r.user, r.method, r.path, r.body)
	after := zgSnap(t, h)
	if code >= 200 && code < 300 {
		t.Errorf("unexpected acceptance: %s %s %s as %q -> %d body=%s", r.method, r.path, r.body, r.user, code, resp)
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

func zgDenied(t *testing.T, r zgReq, want int) {
	t.Helper()
	zgDeniedSetup(t, nil, r, want)
}

func zgReader(pid string) string {
	switch pid {
	case "gx":
		return "gus"
	case "legacy":
		return "ann"
	}
	return "ivy"
}

// zgCopyOK performs the copy on a fresh server and verifies the full success contract.
func zgCopyOK(t *testing.T, h http.Handler, user, from, tid, to string) zgTask {
	t.Helper()
	before := zgSnap(t, h)
	src := before[tid]
	code, body := zgDo(h, user, zgCopy(user, from, tid, to).method, zgCopy(user, from, tid, to).path, zgCopy(user, from, tid, to).body)
	if code != 201 {
		t.Fatalf("copy %s/%s -> %s as %s: status %d, want 201 (body=%s)", from, tid, to, user, code, body)
	}
	var nt zgTask
	if err := json.Unmarshal([]byte(body), &nt); err != nil {
		t.Fatalf("response not a task: %v (%s)", err, body)
	}
	if nt.ID == "" {
		t.Fatalf("response has no id: %s", body)
	}
	if _, exists := before[nt.ID]; exists {
		t.Fatalf("ID collision: new task reuses existing ID %s (body=%s)", nt.ID, body)
	}
	if nt.ProjectID != to {
		t.Errorf("projectId %q, want %q", nt.ProjectID, to)
	}
	if nt.Title != src.Title {
		t.Errorf("title %q, want %q", nt.Title, src.Title)
	}
	if nt.Assignee != src.Assignee {
		t.Errorf("assignee %q, want %q", nt.Assignee, src.Assignee)
	}
	if nt.Status != "open" {
		t.Errorf("status %q, want open", nt.Status)
	}
	after := zgSnap(t, h)
	// source and every other task unchanged; exactly one task added
	exp := map[string]zgTask{}
	for k, v := range before {
		exp[k] = v
	}
	exp[nt.ID] = zgTask{ID: nt.ID, ProjectID: to, Title: src.Title, Status: "open", Assignee: src.Assignee}
	if d := zgDiff(exp, after); d != "" {
		t.Errorf("state mismatch after copy (source mutated or extra/missing task): %s", d)
	}
	// readable in destination, not through the source project
	rd := zgReader(to)
	c1, b1 := zgDo(h, rd, "GET", "/projects/"+to+"/tasks/"+nt.ID, "")
	if c1 != 200 {
		t.Errorf("GET new task in destination as %s -> %d (%s)", rd, c1, b1)
	} else {
		var g zgTask
		json.Unmarshal([]byte(b1), &g)
		if g != exp[nt.ID] {
			t.Errorf("stored task %+v, want %+v", g, exp[nt.ID])
		}
	}
	if from != to {
		if c2, _ := zgDo(h, zgReader(from), "GET", "/projects/"+from+"/tasks/"+nt.ID, ""); c2 != 404 {
			t.Errorf("new task visible under source project path: %d", c2)
		}
	}
	return nt
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
		{"ListOpsIvy", "ivy", "/projects/ops/tasks", 200},
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
		{"AnnViewerOpsCreate", zgReq{"ann", "POST", "/projects/ops/tasks", `{"title":"x"}`}, 403},
		{"NonMemberCreate", zgReq{"hal", "POST", "/projects/web/tasks", `{"title":"x"}`}, 404},
		{"ContribUpdateOthers", zgReq{"ben", "PATCH", "/projects/web/tasks/t2", `{"status":"done"}`}, 403},
		{"ContribReassign", zgReq{"ben", "PATCH", "/projects/web/tasks/t1", `{"assignee":"cat"}`}, 403},
		{"ContribDelete", zgReq{"ben", "DELETE", "/projects/web/tasks/t1", ""}, 403},
		{"ContribCreateForOther", zgReq{"ben", "POST", "/projects/web/tasks", `{"title":"x","assignee":"cat"}`}, 403},
		{"OtherOrgManagerUpdate", zgReq{"gus", "PATCH", "/projects/web/tasks/t1", `{"status":"done"}`}, 404},
		{"AnonUpdate", zgReq{"", "PATCH", "/projects/web/tasks/t1", `{"status":"done"}`}, 401},
		{"ArchivedManagerCreate", zgReq{"ann", "POST", "/projects/legacy/tasks", `{"title":"x"}`}, 409},
		{"ArchivedManagerUpdate", zgReq{"ann", "PATCH", "/projects/legacy/tasks/t5", `{"status":"done"}`}, 409},
		{"ArchivedManagerDelete", zgReq{"ann", "DELETE", "/projects/legacy/tasks/t5", ""}, 409},
		{"ManagerUpdateWrongProject", zgReq{"ann", "PATCH", "/projects/web/tasks/t4", `{"status":"done"}`}, 404},
		{"ManagerCreateBadAssignee", zgReq{"ann", "POST", "/projects/web/tasks", `{"title":"x","assignee":"hal"}`}, 400},
		{"IvyCreateOpsBadAssignee", zgReq{"ivy", "POST", "/projects/ops/tasks", `{"title":"x","assignee":"ben"}`}, 400},
	}
	for _, c := range regDeny {
		c := c
		t.Run("REG_"+c.n, func(t *testing.T) { zgDenied(t, c.r, c.w) })
	}
	t.Run("REG_ManagerCreateUpdateDelete", func(t *testing.T) {
		h := newServer()
		code, body := zgDo(h, "ivy", "POST", "/projects/ops/tasks", `{"title":"n","assignee":"cat"}`)
		var tk zgTask
		json.Unmarshal([]byte(body), &tk)
		if code != 201 || tk.ProjectID != "ops" || tk.Assignee != "cat" {
			t.Fatalf("create %d %s", code, body)
		}
		if c, b := zgDo(h, "ivy", "PATCH", "/projects/ops/tasks/"+tk.ID, `{"assignee":"ivy","status":"doing"}`); c != 200 {
			t.Fatalf("patch %d %s", c, b)
		}
		if c, _ := zgDo(h, "ivy", "DELETE", "/projects/ops/tasks/"+tk.ID, ""); c != 204 {
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
		if c1 != 201 || c2 != 201 {
			t.Fatalf("got %d %d", c1, c2)
		}
	})
	t.Run("REG_OtherOrgManagerGx", func(t *testing.T) {
		h := newServer()
		c, _ := zgDo(h, "gus", "PATCH", "/projects/gx/tasks/t6", `{"status":"done"}`)
		if c != 200 {
			t.Fatalf("got %d", c)
		}
	})
	t.Run("REG_SeedUnchanged", func(t *testing.T) {
		s := zgSnap(t, newServer())
		if len(s) != 6 || s["t2"].Status != "doing" || s["t4"].Assignee != "cat" || s["t5"].ProjectID != "legacy" {
			t.Fatalf("seed unexpected: %+v", s)
		}
	})

	// ---------- FUNC ----------
	funcs := []struct{ n, user, from, tid, to string }{
		{"IvyCopiesAssignedWebToOps", "ivy", "web", "t2", "ops"},
		{"IvyCopiesUnassignedWebToOps", "ivy", "web", "t3", "ops"},
		{"IvyCopiesOpsToWeb", "ivy", "ops", "t4", "web"},
		{"AnnCopiesArchivedSourceToWeb", "ann", "legacy", "t5", "web"},
		{"ContributorCopiesOwnWebToOps", "cat", "web", "t2", "ops"},
		{"ContributorCopiesOwnOpsToWeb", "cat", "ops", "t4", "web"},
	}
	for _, c := range funcs {
		c := c
		t.Run("FUNC_"+c.n, func(t *testing.T) {
			zgCopyOK(t, newServer(), c.user, c.from, c.tid, c.to)
		})
	}
	t.Run("FUNC_StatusResetToOpenFromDoing", func(t *testing.T) {
		h := newServer()
		if s := zgSnap(t, h)["t2"]; s.Status != "doing" {
			t.Fatalf("seed t2 status %q", s.Status)
		}
		nt := zgCopyOK(t, h, "ivy", "web", "t2", "ops")
		if nt.Status != "open" {
			t.Errorf("status %q", nt.Status)
		}
	})
	t.Run("FUNC_CopyTwiceDistinctIDs", func(t *testing.T) {
		h := newServer()
		a := zgCopyOK(t, h, "ivy", "web", "t2", "ops")
		b := zgCopyOK(t, h, "ivy", "web", "t2", "ops")
		if a.ID == b.ID {
			t.Fatalf("two copies share ID %s", a.ID)
		}
	})
	t.Run("FUNC_CopyOfCopy", func(t *testing.T) {
		h := newServer()
		a := zgCopyOK(t, h, "ivy", "web", "t3", "ops")
		b := zgCopyOK(t, h, "ivy", "ops", a.ID, "web")
		if a.ID == b.ID {
			t.Fatalf("copy of copy reuses ID")
		}
	})
	t.Run("FUNC_CopyIsIndependentOfSource", func(t *testing.T) {
		h := newServer()
		nt := zgCopyOK(t, h, "ivy", "web", "t2", "ops")
		if c, b := zgDo(h, "ivy", "PATCH", "/projects/ops/tasks/"+nt.ID, `{"status":"done","title":"changed"}`); c != 200 {
			t.Fatalf("patch copy %d %s", c, b)
		}
		s := zgSnap(t, h)
		if s["t2"].Status != "doing" || s["t2"].Title != "Update footer" {
			t.Errorf("editing the copy changed the source: %+v", s["t2"])
		}
	})
	t.Run("FUNC_CopyAppearsInDestinationList", func(t *testing.T) {
		h := newServer()
		nt := zgCopyOK(t, h, "ivy", "web", "t3", "ops")
		_, body := zgDo(h, "ivy", "GET", "/projects/ops/tasks", "")
		if !strings.Contains(body, `"`+nt.ID+`"`) {
			t.Errorf("ops list lacks copied task %s: %s", nt.ID, body)
		}
		_, wb := zgDo(h, "ivy", "GET", "/projects/web/tasks", "")
		if strings.Contains(wb, `"`+nt.ID+`"`) {
			t.Errorf("web list contains copied task %s", nt.ID)
		}
	})

	// ---------- AUTHZ ----------
	authz := []struct {
		n string
		r zgReq
	}{
		// destination assignee must be a writer in destination
		{"IvyCopiesBenTaskToOps_AssigneeNotInOps", zgCopy("ivy", "web", "t1", "ops")},
		{"ContribCopiesBenTaskToOps", zgCopy("cat", "web", "t1", "ops")},
		// archived / other-org / nonexistent destination
		{"ManagerCopyToArchived", zgCopy("ann", "web", "t3", "legacy")},
		{"ManagerCopyAssignedToArchived", zgCopy("ann", "web", "t1", "legacy")},
		{"ContribCopyOwnToArchived", zgCopy("ben", "web", "t1", "legacy")},
		{"IvyCopyToArchivedNonMember", zgCopy("ivy", "web", "t3", "legacy")},
		{"IvyCopyToGx", zgCopy("ivy", "web", "t2", "gx")},
		{"IvyCopyUnassignedToGx", zgCopy("ivy", "web", "t3", "gx")},
		{"EveCopyToGx", zgCopy("eve", "web", "t3", "gx")},
		{"CopyToUnknownProject", zgCopy("ivy", "web", "t3", "nope")},
		// destination readable but not writable
		{"ViewerInDestCopy", zgCopy("ann", "web", "t2", "ops")},
		{"ViewerInDestCopyUnassigned", zgCopy("ann", "web", "t3", "ops")},
		{"OrgAdminCopyToOps", zgCopy("eve", "web", "t2", "ops")},
		{"OrgAdminCopyToWeb", zgCopy("eve", "ops", "t4", "web")},
		{"OrgAdminCopyToLegacy", zgCopy("eve", "web", "t3", "legacy")},
		// destination not readable
		{"NonMemberOfDestCopy", zgCopy("ben", "web", "t1", "ops")},
		{"NonMemberOfDestCopyUnassigned", zgCopy("ben", "web", "t3", "ops")},
		{"ViewerNoDestAccess", zgCopy("dan", "web", "t3", "ops")},
		{"ViewerNoDestAccessLegacy", zgCopy("dan", "web", "t3", "legacy")},
		// source not readable
		{"NonMemberSource", zgCopy("hal", "web", "t3", "ops")},
		{"NonMemberSourceToSelfProject", zgCopy("hal", "web", "t3", "web")},
		{"NonMemberOfSourceOps", zgCopy("ben", "ops", "t4", "web")},
		{"NonMemberOfSourceLegacy", zgCopy("cat", "legacy", "t5", "web")},
		{"NonMemberOfSourceLegacyIvy", zgCopy("ivy", "legacy", "t5", "web")},
		{"CrossOrgSourceByAcme", zgCopy("ann", "gx", "t6", "web")},
		{"CrossOrgSourceByAdmin", zgCopy("eve", "gx", "t6", "web")},
		{"CrossOrgCallerCopiesAcmeTask", zgCopy("gus", "web", "t1", "gx")},
		{"CrossOrgCallerToAcme", zgCopy("gus", "gx", "t6", "web")},
		{"TaskNotInPathProject", zgCopy("ivy", "web", "t4", "ops")},
		{"TaskNotInPathProject2", zgCopy("ivy", "ops", "t1", "web")},
		{"TaskInOtherOrgViaWeb", zgCopy("ivy", "web", "t6", "ops")},
		{"UnknownTask", zgCopy("ivy", "web", "t99", "ops")},
		{"Anonymous", zgCopy("", "web", "t3", "ops")},
		{"UnknownUser", zgCopy("zed", "web", "t3", "ops")},
	}
	for _, c := range authz {
		c := c
		t.Run("AUTHZ_"+c.n, func(t *testing.T) { zgDenied(t, c.r, 0) })
	}
	t.Run("AUTHZ_ContribCopiesOthersTaskToOps", func(t *testing.T) {
		// ivy owns a web task; cat (contributor in both) may not create ops tasks assigned to ivy
		zgDeniedSetup(t, func(h http.Handler) {
			if c, b := zgDo(h, "ivy", "POST", "/projects/web/tasks", `{"title":"Ivy private","assignee":"ivy"}`); c != 201 {
				t.Fatalf("setup %d %s", c, b)
			}
		}, zgCopy("cat", "web", "t7", "ops"), 0)
	})
	t.Run("AUTHZ_ContribCopiesOthersTaskToWeb", func(t *testing.T) {
		zgDeniedSetup(t, func(h http.Handler) {
			if c, b := zgDo(h, "ivy", "POST", "/projects/ops/tasks", `{"title":"Ivy ops","assignee":"ivy"}`); c != 201 {
				t.Fatalf("setup %d %s", c, b)
			}
		}, zgCopy("cat", "ops", "t7", "web"), 0)
	})
	t.Run("AUTHZ_DeniedCopyDoesNotLeakTitleViaError", func(t *testing.T) {
		h := newServer()
		seed := zgSnap(t, h)
		for _, r := range []zgReq{zgCopy("ben", "ops", "t4", "web"), zgCopy("ann", "gx", "t6", "web"), zgCopy("hal", "web", "t1", "ops")} {
			_, body := zgDo(h, r.user, r.method, r.path, r.body)
			if l := zgLeaks(r.user, body, seed); len(l) > 0 {
				t.Errorf("leak: %s as %s exposes %v: %s", r.path, r.user, l, body)
			}
		}
	})

	// ---------- ATOM ----------
	t.Run("ATOM_AllDeniedBatchCreatesNothing", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		n := 0
		for _, c := range authz {
			code, _ := zgDo(h, c.r.user, c.r.method, c.r.path, c.r.body)
			if code >= 200 && code < 300 {
				n++
			}
		}
		after := zgSnap(t, h)
		if n > 0 {
			t.Errorf("unexpected acceptance: %d denied copies were accepted", n)
		}
		if d := zgDiff(before, after); d != "" {
			t.Errorf("state changed: %s", d)
		}
	})
	t.Run("ATOM_DeniedBetweenAllowedOnlyAllowedLand", func(t *testing.T) {
		h := newServer()
		a := zgCopyOK(t, h, "ivy", "web", "t2", "ops")
		mid := zgSnap(t, h)
		for _, r := range []zgReq{zgCopy("ivy", "web", "t1", "ops"), zgCopy("ann", "web", "t3", "legacy"), zgCopy("ivy", "web", "t2", "gx"), zgCopy("ann", "web", "t2", "ops")} {
			zgDo(h, r.user, r.method, r.path, r.body)
		}
		if d := zgDiff(mid, zgSnap(t, h)); d != "" {
			t.Errorf("state changed by denied copies: %s", d)
		}
		b := zgCopyOK(t, h, "ivy", "web", "t3", "ops")
		if a.ID == b.ID {
			t.Errorf("ID reused after denied attempts")
		}
	})
	t.Run("ATOM_FailedCopyDoesNotConsumeOrDuplicate", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		for i := 0; i < 3; i++ {
			zgDo(h, "ivy", "POST", "/projects/web/tasks/t1/copy", `{"toProjectId":"ops"}`)
		}
		if d := zgDiff(before, zgSnap(t, h)); d != "" {
			t.Errorf("state changed after repeated denied copies: %s", d)
		}
	})

	// ---------- VALID ----------
	valid := []struct{ n, user, body string }{
		{"MalformedJSON", "ivy", `{"toProjectId":`},
		{"NotJSON", "ivy", `ops`},
		{"NumberProject", "ivy", `{"toProjectId":5}`},
		{"NullBody", "ivy", `null`},
		{"ArrayBody", "ivy", `["ops"]`},
	}
	for _, c := range valid {
		c := c
		t.Run("VALID_"+c.n, func(t *testing.T) {
			zgDenied(t, zgReq{c.user, "POST", "/projects/web/tasks/t3/copy", c.body}, 0)
		})
	}
	t.Run("VALID_EmptyBodyRejected", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		req := httptest.NewRequest("POST", "/projects/web/tasks/t3/copy", nil)
		req.Header.Set("X-User", "ivy")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code >= 200 && rec.Code < 300 {
			t.Errorf("unexpected acceptance: empty body -> %d", rec.Code)
		}
		if d := zgDiff(before, zgSnap(t, h)); d != "" {
			t.Errorf("state changed: %s", d)
		}
	})
	t.Run("VALID_ExtraFieldsDoNotOverrideCopy", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		code, body := zgDo(h, "ivy", "POST", "/projects/web/tasks/t2/copy", `{"toProjectId":"ops","title":"hijack","status":"done","assignee":"ivy","id":"t2","projectId":"web"}`)
		after := zgSnap(t, h)
		if code == 201 {
			var nt zgTask
			json.Unmarshal([]byte(body), &nt)
			if nt.ID == "t2" {
				t.Errorf("ID collision with source via client-supplied id")
			}
			if nt.Title != "Update footer" || nt.Status != "open" || nt.Assignee != "cat" || nt.ProjectID != "ops" {
				t.Errorf("client-supplied fields overrode the copy: %+v", nt)
			}
			delete(after, nt.ID)
		}
		if d := zgDiff(before, after); d != "" {
			t.Errorf("state changed (source mutated): %s", d)
		}
	})

	// ---------- CONTRACT ----------
	for _, c := range []struct {
		n string
		r zgReq
		w int
	}{
		{"AssigneeNotInDest400", zgCopy("ivy", "web", "t1", "ops"), 400},
		{"ViewerInDest403", zgCopy("ann", "web", "t2", "ops"), 403},
		{"OrgAdminDest403", zgCopy("eve", "web", "t2", "ops"), 403},
		{"ArchivedDest409", zgCopy("ann", "web", "t3", "legacy"), 409},
		{"ArchivedDestContrib409", zgCopy("ben", "web", "t1", "legacy"), 409},
		{"OtherOrgDest404", zgCopy("ivy", "web", "t3", "gx"), 404},
		{"UnknownDest404", zgCopy("ivy", "web", "t3", "nope"), 404},
		{"NonMemberDest404", zgCopy("ben", "web", "t1", "ops"), 404},
		{"NonMemberSource404", zgCopy("hal", "web", "t3", "ops"), 404},
		{"CrossOrgSource404", zgCopy("ann", "gx", "t6", "web"), 404},
		{"SourceNotInPath404", zgCopy("ivy", "web", "t4", "ops"), 404},
		{"Anon401", zgCopy("", "web", "t3", "ops"), 401},
		{"MalformedBody400", zgReq{"ivy", "POST", "/projects/web/tasks/t3/copy", `{"toProjectId":`}, 400},
	} {
		c := c
		t.Run("CONTRACT_"+c.n, func(t *testing.T) { zgDenied(t, c.r, c.w) })
	}
	t.Run("CONTRACT_ContribOthersTask403", func(t *testing.T) {
		zgDeniedSetup(t, func(h http.Handler) {
			zgDo(h, "ivy", "POST", "/projects/web/tasks", `{"title":"Ivy private","assignee":"ivy"}`)
		}, zgCopy("cat", "web", "t7", "ops"), 403)
	})
	t.Run("CONTRACT_SuccessBodyIsTaskJSON", func(t *testing.T) {
		h := newServer()
		code, body := zgDo(h, "ivy", "POST", "/projects/web/tasks/t3/copy", `{"toProjectId":"ops"}`)
		if code != 201 {
			t.Fatalf("status %d", code)
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(body), &m); err != nil {
			t.Fatalf("bad json: %v", err)
		}
		for _, k := range []string{"id", "projectId", "title", "status", "assignee"} {
			if _, ok := m[k]; !ok {
				t.Errorf("response lacks %q: %s", k, body)
			}
		}
	})

	// ---------- INFO (log only) ----------
	info := func(name string, f func(t *testing.T)) {
		t.Run("INFO_"+name, func(t *testing.T) { f(t) })
	}
	logCopy := func(t *testing.T, setup func(h http.Handler), r zgReq) {
		h := newServer()
		if setup != nil {
			setup(h)
		}
		before := zgSnap(t, h)
		code, body := zgDo(h, r.user, r.method, r.path, r.body)
		after := zgSnap(t, h)
		t.Logf("INFO %s %s %s as %q -> %d; added=%d; body=%s", r.method, r.path, r.body, r.user, code, len(after)-len(before), strings.TrimSpace(body))
	}
	info("ViewerOfSourceCopiesToWhereManager_AnnOpsT4ToWeb", func(t *testing.T) {
		logCopy(t, nil, zgCopy("ann", "ops", "t4", "web"))
	})
	info("ContribCopiesUnassignedNotOwn_CatWebT3ToOps", func(t *testing.T) {
		logCopy(t, nil, zgCopy("cat", "web", "t3", "ops"))
	})
	info("ContribCopiesOthersAssignedBen_CatT1ToOps_ExactCode", func(t *testing.T) {
		logCopy(t, nil, zgCopy("cat", "web", "t1", "ops"))
	})
	info("ManagerCopiesBenToOps_IvyT1_ExactCode", func(t *testing.T) {
		logCopy(t, nil, zgCopy("ivy", "web", "t1", "ops"))
	})
	info("BenCopiesOwnArchivedT5ToWeb", func(t *testing.T) {
		logCopy(t, nil, zgCopy("ben", "legacy", "t5", "web"))
	})
	info("SameProjectCopy_IvyT2", func(t *testing.T) {
		logCopy(t, nil, zgCopy("ivy", "web", "t2", "web"))
	})
	info("SameProjectCopy_ContribCatT2", func(t *testing.T) {
		logCopy(t, nil, zgCopy("cat", "web", "t2", "web"))
	})
	info("MissingToProjectId", func(t *testing.T) {
		logCopy(t, nil, zgReq{"ivy", "POST", "/projects/web/tasks/t3/copy", `{}`})
	})
	info("EmptyToProjectId", func(t *testing.T) {
		logCopy(t, nil, zgReq{"ivy", "POST", "/projects/web/tasks/t3/copy", `{"toProjectId":""}`})
	})
	info("OrderSourceUnreadableAndDestUnknown_HalT1ToNope", func(t *testing.T) {
		logCopy(t, nil, zgCopy("hal", "web", "t1", "nope"))
	})
	info("OrderSourceUnreadableAndDestArchived_IvyT5ToLegacy", func(t *testing.T) {
		logCopy(t, nil, zgCopy("ivy", "legacy", "t5", "legacy"))
	})
	info("OrderDestViewerAndBadAssignee_AnnT1ToOps", func(t *testing.T) {
		logCopy(t, nil, zgCopy("ann", "web", "t1", "ops"))
	})
	info("OrderArchivedDestAndBadAssignee_AnnT2ToLegacy", func(t *testing.T) {
		logCopy(t, nil, zgCopy("ann", "web", "t2", "legacy"))
	})
	info("OrgAdminReadSourceDestViewerOnly_EveT5ToWeb", func(t *testing.T) {
		logCopy(t, nil, zgCopy("eve", "legacy", "t5", "web"))
	})
}
