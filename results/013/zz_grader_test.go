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

// Hidden grader for task 013 (PATCH task with projectId re-homes it).
// Uses only newServer() and HTTP. Seed v2 (ivy = manager of web and ops).
// Rules assumed: source = existing PATCH rules; destination = create rules, and the
// resulting assignee (patched or existing) must be a contributor/manager of the destination.

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


func zgPatch(user, from, tid, body string) zgReq {
	return zgReq{user, "PATCH", "/projects/" + from + "/tasks/" + tid, body}
}

func zgNoop(h http.Handler) {}

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

// zgMoveOK applies a PATCH that must succeed and verifies the moved task, that the
// path/ID is preserved, and that nothing else changed. mod edits the expected task.
func zgMoveOK(t *testing.T, h http.Handler, user, from, tid, body, to string, mod func(*zgTask)) zgTask {
	t.Helper()
	before := zgSnap(t, h)
	code, resp := zgDo(h, user, "PATCH", "/projects/"+from+"/tasks/"+tid, body)
	if code != 200 {
		t.Fatalf("PATCH %s/%s %s as %s: status %d, want 200 (body=%s)", from, tid, body, user, code, resp)
	}
	exp := before[tid]
	exp.ProjectID = to
	if mod != nil {
		mod(&exp)
	}
	var got zgTask
	if err := json.Unmarshal([]byte(resp), &got); err != nil {
		t.Fatalf("response not a task: %v (%s)", err, resp)
	}
	if got != exp {
		t.Errorf("response task %+v, want %+v", got, exp)
	}
	after := zgSnap(t, h)
	want := map[string]zgTask{}
	for k, v := range before {
		want[k] = v
	}
	want[tid] = exp
	if d := zgDiff(want, after); d != "" {
		t.Errorf("state mismatch after move: %s", d)
	}
	if c, _ := zgDo(h, zgReader(to), "GET", "/projects/"+to+"/tasks/"+tid, ""); c != 200 {
		t.Errorf("task not readable in destination: %d", c)
	}
	if from != to {
		if c, _ := zgDo(h, zgReader(from), "GET", "/projects/"+from+"/tasks/"+tid, ""); c != 404 {
			t.Errorf("task still readable under source project: %d", c)
		}
	}
	return exp
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
		{"ViewerUpdate", zgPatch("dan", "web", "t1", `{"status":"done"}`), 403},
		{"ViewerDelete", zgReq{"dan", "DELETE", "/projects/web/tasks/t1", ""}, 403},
		{"OrgAdminUpdate", zgPatch("eve", "web", "t1", `{"status":"done"}`), 403},
		{"AnnViewerOpsUpdate", zgPatch("ann", "ops", "t4", `{"status":"done"}`), 403},
		{"NonMemberUpdate", zgPatch("hal", "web", "t1", `{"status":"done"}`), 404},
		{"ContribUpdateOthers", zgPatch("ben", "web", "t2", `{"status":"done"}`), 403},
		{"ContribUpdateUnassigned", zgPatch("ben", "web", "t3", `{"status":"done"}`), 403},
		{"ContribReassign", zgPatch("ben", "web", "t1", `{"assignee":"cat"}`), 403},
		{"ContribDelete", zgReq{"ben", "DELETE", "/projects/web/tasks/t1", ""}, 403},
		{"ContribCreateForOther", zgReq{"ben", "POST", "/projects/web/tasks", `{"title":"x","assignee":"cat"}`}, 403},
		{"OtherOrgUpdate", zgPatch("gus", "web", "t1", `{"status":"done"}`), 404},
		{"AnonUpdate", zgPatch("", "web", "t1", `{"status":"done"}`), 401},
		{"ArchivedManagerUpdate", zgPatch("ann", "legacy", "t5", `{"status":"done"}`), 409},
		{"ArchivedContribUpdate", zgPatch("ben", "legacy", "t5", `{"status":"done"}`), 409},
		{"ManagerUpdateWrongProject", zgPatch("ann", "web", "t4", `{"status":"done"}`), 404},
		{"ManagerInvalidStatus", zgPatch("ann", "web", "t1", `{"status":"bogus"}`), 400},
		{"ManagerEmptyTitle", zgPatch("ann", "web", "t1", `{"title":""}`), 400},
		{"ManagerReassignBadAssignee", zgPatch("ivy", "web", "t1", `{"assignee":"hal"}`), 400},
		{"ManagerCreateBadAssignee", zgReq{"ann", "POST", "/projects/web/tasks", `{"title":"x","assignee":"hal"}`}, 400},
	}
	for _, c := range regDeny {
		c := c
		t.Run("REG_"+c.n, func(t *testing.T) { zgDenied(t, c.r, c.w) })
	}
	t.Run("REG_ManagerPatchFields", func(t *testing.T) {
		h := newServer()
		code, b := zgDo(h, "ivy", "PATCH", "/projects/web/tasks/t1", `{"title":"New","status":"doing","assignee":"cat"}`)
		var tk zgTask
		json.Unmarshal([]byte(b), &tk)
		if code != 200 || tk.Title != "New" || tk.Status != "doing" || tk.Assignee != "cat" || tk.ProjectID != "web" {
			t.Fatalf("%d %s", code, b)
		}
		s := zgSnap(t, h)
		if s["t1"] != tk {
			t.Fatalf("stored %+v vs %+v", s["t1"], tk)
		}
	})
	t.Run("REG_ManagerUnassign", func(t *testing.T) {
		h := newServer()
		c, _ := zgDo(h, "ivy", "PATCH", "/projects/web/tasks/t1", `{"assignee":""}`)
		if c != 200 || zgSnap(t, h)["t1"].Assignee != "" {
			t.Fatalf("code %d", c)
		}
	})
	t.Run("REG_ContributorOwnUpdate", func(t *testing.T) {
		h := newServer()
		c, _ := zgDo(h, "ben", "PATCH", "/projects/web/tasks/t1", `{"status":"done","title":"mine"}`)
		s := zgSnap(t, h)["t1"]
		if c != 200 || s.Status != "done" || s.Title != "mine" || s.ProjectID != "web" {
			t.Fatalf("code=%d %+v", c, s)
		}
	})
	t.Run("REG_ManagerCreateUpdateDelete", func(t *testing.T) {
		h := newServer()
		code, body := zgDo(h, "ivy", "POST", "/projects/ops/tasks", `{"title":"n","assignee":"cat"}`)
		var tk zgTask
		json.Unmarshal([]byte(body), &tk)
		if code != 201 {
			t.Fatalf("create %d %s", code, body)
		}
		if c, b := zgDo(h, "ivy", "PATCH", "/projects/ops/tasks/"+tk.ID, `{"assignee":"ivy","status":"doing"}`); c != 200 {
			t.Fatalf("patch %d %s", c, b)
		}
		if c, _ := zgDo(h, "ivy", "DELETE", "/projects/ops/tasks/"+tk.ID, ""); c != 204 {
			t.Fatalf("delete %d", c)
		}
	})
	t.Run("REG_OtherOrgManagerGx", func(t *testing.T) {
		c, _ := zgDo(newServer(), "gus", "PATCH", "/projects/gx/tasks/t6", `{"status":"done"}`)
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
	funcs := []struct {
		n, user, from, tid, body, to string
		mod                          func(*zgTask)
	}{
		{"IvyMovesAssignedWebToOps", "ivy", "web", "t2", `{"projectId":"ops"}`, "ops", nil},
		{"IvyMovesUnassignedWebToOps", "ivy", "web", "t3", `{"projectId":"ops"}`, "ops", nil},
		{"IvyMovesOpsToWeb", "ivy", "ops", "t4", `{"projectId":"web"}`, "web", nil},
		{"ContribMovesOwnWebToOps", "cat", "web", "t2", `{"projectId":"ops"}`, "ops", nil},
		{"ContribMovesOwnOpsToWeb", "cat", "ops", "t4", `{"projectId":"web"}`, "web", nil},
		{"MoveWithTitleAndStatus", "ivy", "web", "t2", `{"projectId":"ops","title":"Renamed","status":"done"}`, "ops", func(x *zgTask) { x.Title = "Renamed"; x.Status = "done" }},
		{"MoveWithStatusOnly", "ivy", "web", "t3", `{"projectId":"ops","status":"doing"}`, "ops", func(x *zgTask) { x.Status = "doing" }},
		{"ContribMoveOwnWithTitle", "cat", "web", "t2", `{"projectId":"ops","title":"Mine now"}`, "ops", func(x *zgTask) { x.Title = "Mine now" }},
		{"MoveWithValidDestAssignee", "ivy", "web", "t1", `{"projectId":"ops","assignee":"cat"}`, "ops", func(x *zgTask) { x.Assignee = "cat" }},
		{"MoveWithAssigneeSelf", "ivy", "web", "t1", `{"projectId":"ops","assignee":"ivy"}`, "ops", func(x *zgTask) { x.Assignee = "ivy" }},
		{"MoveWithUnassign", "ivy", "web", "t1", `{"projectId":"ops","assignee":""}`, "ops", func(x *zgTask) { x.Assignee = "" }},
		{"MoveStatusKeptWhenNotGiven", "ivy", "web", "t2", `{"projectId":"ops"}`, "ops", nil},
	}
	for _, c := range funcs {
		c := c
		t.Run("FUNC_"+c.n, func(t *testing.T) {
			zgMoveOK(t, newServer(), c.user, c.from, c.tid, c.body, c.to, c.mod)
		})
	}
	t.Run("FUNC_StatusDoingPreservedOnMove", func(t *testing.T) {
		h := newServer()
		x := zgMoveOK(t, h, "ivy", "web", "t2", `{"projectId":"ops"}`, "ops", nil)
		if x.Status != "doing" {
			t.Errorf("status %q, want doing preserved", x.Status)
		}
	})
	t.Run("FUNC_RoundTrip", func(t *testing.T) {
		h := newServer()
		zgMoveOK(t, h, "ivy", "web", "t2", `{"projectId":"ops"}`, "ops", nil)
		zgMoveOK(t, h, "ivy", "ops", "t2", `{"projectId":"web"}`, "web", nil)
	})
	t.Run("FUNC_MovedTaskUsableInDest", func(t *testing.T) {
		h := newServer()
		zgMoveOK(t, h, "ivy", "web", "t2", `{"projectId":"ops"}`, "ops", nil)
		if c, b := zgDo(h, "ivy", "PATCH", "/projects/ops/tasks/t2", `{"status":"done"}`); c != 200 {
			t.Fatalf("patch in dest %d %s", c, b)
		}
		if c, _ := zgDo(h, "ivy", "PATCH", "/projects/web/tasks/t2", `{"status":"open"}`); c != 404 {
			t.Errorf("old path still addressable: %d", c)
		}
		_, lw := zgDo(h, "ivy", "GET", "/projects/web/tasks", "")
		_, lo := zgDo(h, "ivy", "GET", "/projects/ops/tasks", "")
		if strings.Contains(lw, `"t2"`) || !strings.Contains(lo, `"t2"`) {
			t.Errorf("list membership wrong: web=%s ops=%s", lw, lo)
		}
	})
	t.Run("FUNC_ContribMovesThenEditsInDest", func(t *testing.T) {
		h := newServer()
		zgMoveOK(t, h, "cat", "web", "t2", `{"projectId":"ops"}`, "ops", nil)
		if c, _ := zgDo(h, "cat", "PATCH", "/projects/ops/tasks/t2", `{"status":"done"}`); c != 200 {
			t.Fatalf("edit in dest %d", c)
		}
	})
	t.Run("FUNC_PatchWithoutProjectIdStaysInPlace", func(t *testing.T) {
		h := newServer()
		zgMoveOK(t, h, "ivy", "web", "t2", `{"title":"only title"}`, "web", func(x *zgTask) { x.Title = "only title" })
	})

	// ---------- AUTHZ ----------
	authz := []struct {
		n string
		r zgReq
	}{
		// resulting assignee must be a writer in destination
		{"IvyMovesBenTaskToOps_AssigneeNotInOps", zgPatch("ivy", "web", "t1", `{"projectId":"ops"}`)},
		{"IvyMovesWithExplicitBadAssignee", zgPatch("ivy", "web", "t2", `{"projectId":"ops","assignee":"ben"}`)},
		{"IvyMovesWithViewerAssignee", zgPatch("ivy", "web", "t3", `{"projectId":"ops","assignee":"ann"}`)},
		{"IvyMovesOpsToWebBadAssignee", zgPatch("ivy", "ops", "t4", `{"projectId":"web","assignee":"hal"}`)},
		{"IvyMovesWithViewerDanAssignee", zgPatch("ivy", "ops", "t4", `{"projectId":"web","assignee":"dan"}`)},
		// destination readable but not writable
		{"ViewerInDestMoves", zgPatch("ann", "web", "t2", `{"projectId":"ops"}`)},
		{"ViewerInDestMovesUnassigned", zgPatch("ann", "web", "t3", `{"projectId":"ops"}`)},
		// archived / other-org / nonexistent destination
		{"ManagerMovesToArchived", zgPatch("ann", "web", "t3", `{"projectId":"legacy"}`)},
		{"ManagerMovesAssignedToArchived", zgPatch("ann", "web", "t1", `{"projectId":"legacy"}`)},
		{"ContribOwnMovesToArchived", zgPatch("ben", "web", "t1", `{"projectId":"legacy"}`)},
		{"IvyMovesToArchivedNonMember", zgPatch("ivy", "web", "t3", `{"projectId":"legacy"}`)},
		{"IvyMovesToGx", zgPatch("ivy", "web", "t2", `{"projectId":"gx"}`)},
		{"IvyMovesUnassignedToGx", zgPatch("ivy", "web", "t3", `{"projectId":"gx"}`)},
		{"IvyMovesToUnknown", zgPatch("ivy", "web", "t3", `{"projectId":"nope"}`)},
		{"AnnMovesGxTaskIn", zgPatch("ann", "gx", "t6", `{"projectId":"web"}`)},
		{"GusMovesGxTaskToWeb", zgPatch("gus", "gx", "t6", `{"projectId":"web"}`)},
		{"GusMovesWebTaskToGx", zgPatch("gus", "web", "t1", `{"projectId":"gx"}`)},
		// destination not readable
		{"NonMemberOfDestMoves", zgPatch("ben", "web", "t1", `{"projectId":"ops"}`)},
		{"ContribNotInDestUnassigned", zgPatch("ben", "web", "t3", `{"projectId":"ops"}`)},
		// source rules
		{"OrgAdminMovesToOps", zgPatch("eve", "web", "t3", `{"projectId":"ops"}`)},
		{"OrgAdminMovesOpsToWeb", zgPatch("eve", "ops", "t4", `{"projectId":"web"}`)},
		{"ViewerOfSourceMoves", zgPatch("ann", "ops", "t4", `{"projectId":"web"}`)},
		{"ViewerDanMoves", zgPatch("dan", "web", "t3", `{"projectId":"ops"}`)},
		{"NonMemberSourceMoves", zgPatch("hal", "web", "t3", `{"projectId":"ops"}`)},
		{"ContribMovesOthersTask", zgPatch("cat", "web", "t1", `{"projectId":"ops"}`)},
		{"ContribMovesUnassignedTask", zgPatch("cat", "web", "t3", `{"projectId":"ops"}`)},
		{"ContribMovesWithReassign", zgPatch("cat", "web", "t2", `{"projectId":"ops","assignee":"ivy"}`)},
		{"ContribMovesWithReassignSelf", zgPatch("cat", "web", "t2", `{"projectId":"ops","assignee":"cat"}`)},
		{"ContribMovesWithUnassign", zgPatch("cat", "web", "t2", `{"projectId":"ops","assignee":""}`)},
		{"ContribNotInDestMovesOwn", zgPatch("cat", "ops", "t4", `{"projectId":"legacy"}`)},
		{"ArchivedSourceManager", zgPatch("ann", "legacy", "t5", `{"projectId":"web"}`)},
		{"ArchivedSourceContrib", zgPatch("ben", "legacy", "t5", `{"projectId":"web"}`)},
		{"NonMemberOfArchivedSource", zgPatch("ivy", "legacy", "t5", `{"projectId":"web"}`)},
		{"TaskNotInPathProject", zgPatch("ivy", "web", "t4", `{"projectId":"ops"}`)},
		{"TaskNotInPathProject2", zgPatch("ivy", "ops", "t1", `{"projectId":"web"}`)},
		{"UnknownTask", zgPatch("ivy", "web", "t99", `{"projectId":"ops"}`)},
		{"Anonymous", zgPatch("", "web", "t3", `{"projectId":"ops"}`)},
		{"UnknownUser", zgPatch("zed", "web", "t3", `{"projectId":"ops"}`)},
	}
	for _, c := range authz {
		c := c
		t.Run("AUTHZ_"+c.n, func(t *testing.T) { zgDenied(t, c.r, 0) })
	}
	t.Run("AUTHZ_DeniedMoveDoesNotLeakTitles", func(t *testing.T) {
		h := newServer()
		seed := zgSnap(t, h)
		for _, r := range []zgReq{zgPatch("ann", "gx", "t6", `{"projectId":"web"}`), zgPatch("ben", "ops", "t4", `{"projectId":"web"}`), zgPatch("hal", "web", "t1", `{"projectId":"ops"}`), zgPatch("ben", "web", "t1", `{"projectId":"ops"}`)} {
			_, body := zgDo(h, r.user, r.method, r.path, r.body)
			if l := zgLeaks(r.user, body, seed); len(l) > 0 {
				t.Errorf("leak: %s as %s exposes %v: %s", r.path, r.user, l, body)
			}
		}
	})

	// ---------- ATOM ----------
	atom := []struct {
		n string
		r zgReq
	}{
		{"InvalidStatusPlusMove", zgPatch("ivy", "web", "t2", `{"projectId":"ops","status":"bogus"}`)},
		{"EmptyTitlePlusMove", zgPatch("ivy", "web", "t2", `{"projectId":"ops","title":""}`)},
		{"TitleStatusPlusBadAssigneeInDest", zgPatch("ivy", "web", "t2", `{"title":"HIJACK","status":"done","projectId":"ops","assignee":"ben"}`)},
		{"TitlePlusBenTaskToOps", zgPatch("ivy", "web", "t1", `{"title":"HIJACK","projectId":"ops"}`)},
		{"TitlePlusViewerDest", zgPatch("ann", "web", "t2", `{"title":"HIJACK","projectId":"ops"}`)},
		{"TitleStatusPlusArchivedDest", zgPatch("ann", "web", "t3", `{"title":"HIJACK","status":"done","projectId":"legacy"}`)},
		{"TitlePlusOtherOrgDest", zgPatch("ivy", "web", "t2", `{"title":"HIJACK","projectId":"gx"}`)},
		{"TitlePlusUnknownDest", zgPatch("ivy", "web", "t2", `{"title":"HIJACK","projectId":"nope"}`)},
		{"TitlePlusNonMemberDest", zgPatch("ben", "web", "t1", `{"title":"HIJACK","projectId":"ops"}`)},
		{"ContribTitlePlusReassignPlusMove", zgPatch("cat", "web", "t2", `{"title":"HIJACK","projectId":"ops","assignee":"ivy"}`)},
		{"ContribOthersTitlePlusMove", zgPatch("cat", "web", "t1", `{"title":"HIJACK","projectId":"ops"}`)},
		{"ArchivedSourceTitlePlusMove", zgPatch("ann", "legacy", "t5", `{"title":"HIJACK","projectId":"web"}`)},
		{"ViewerTitlePlusMove", zgPatch("dan", "web", "t3", `{"title":"HIJACK","projectId":"ops"}`)},
		{"BadAssigneeOnlyPlusMove", zgPatch("ivy", "ops", "t4", `{"status":"done","projectId":"web","assignee":"hal"}`)},
	}
	for _, c := range atom {
		c := c
		t.Run("ATOM_"+c.n, func(t *testing.T) { zgDenied(t, c.r, 0) })
	}
	t.Run("ATOM_AllDeniedBatchChangesNothing", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		n := 0
		for _, set := range [][]struct {
			n string
			r zgReq
		}{authz, atom} {
			for _, c := range set {
				code, _ := zgDo(h, c.r.user, c.r.method, c.r.path, c.r.body)
				if code >= 200 && code < 300 {
					n++
				}
			}
		}
		after := zgSnap(t, h)
		if n > 0 {
			t.Errorf("unexpected acceptance: %d denied requests accepted", n)
		}
		if d := zgDiff(before, after); d != "" {
			t.Errorf("state changed: %s", d)
		}
	})
	t.Run("ATOM_DeniedBetweenAllowedOnlyAllowedLand", func(t *testing.T) {
		h := newServer()
		zgMoveOK(t, h, "ivy", "web", "t2", `{"projectId":"ops"}`, "ops", nil)
		mid := zgSnap(t, h)
		for _, r := range []zgReq{zgPatch("ivy", "web", "t1", `{"projectId":"ops"}`), zgPatch("ann", "web", "t3", `{"title":"X","projectId":"legacy"}`), zgPatch("ivy", "web", "t3", `{"status":"bogus","projectId":"ops"}`)} {
			zgDo(h, r.user, r.method, r.path, r.body)
		}
		if d := zgDiff(mid, zgSnap(t, h)); d != "" {
			t.Errorf("state changed by denied requests: %s", d)
		}
		zgMoveOK(t, h, "ivy", "web", "t3", `{"projectId":"ops"}`, "ops", nil)
	})

	// ---------- VALID ----------
	valid := []struct{ n, body string }{
		{"MalformedJSON", `{"projectId":`},
		{"NotJSON", `ops`},
		{"NumberProjectId", `{"projectId":5}`},
		{"ArrayProjectId", `{"projectId":["ops"]}`},
		{"BadStatusWithMove", `{"projectId":"ops","status":"bogus"}`},
		{"EmptyTitleWithMove", `{"projectId":"ops","title":""}`},
	}
	for _, c := range valid {
		c := c
		t.Run("VALID_"+c.n, func(t *testing.T) { zgDenied(t, zgPatch("ivy", "web", "t2", c.body), 0) })
	}
	t.Run("VALID_BodyIdAndOtherFieldsIgnored", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		code, _ := zgDo(h, "ivy", "PATCH", "/projects/web/tasks/t2", `{"projectId":"ops","id":"t99"}`)
		after := zgSnap(t, h)
		if code == 200 {
			if _, bad := after["t99"]; bad {
				t.Errorf("client-supplied id applied")
			}
			if after["t2"].ProjectID != "ops" {
				t.Errorf("t2 not moved: %+v", after["t2"])
			}
		} else if d := zgDiff(before, after); d != "" {
			t.Errorf("state changed on %d: %s", code, d)
		}
	})

	// ---------- CONTRACT ----------
	for _, c := range []struct {
		n string
		r zgReq
		w int
	}{
		{"AssigneeNotInDest400", zgPatch("ivy", "web", "t1", `{"projectId":"ops"}`), 400},
		{"ExplicitBadAssignee400", zgPatch("ivy", "web", "t2", `{"projectId":"ops","assignee":"ben"}`), 400},
		{"ViewerInDest403", zgPatch("ann", "web", "t2", `{"projectId":"ops"}`), 403},
		{"ArchivedDest409", zgPatch("ann", "web", "t3", `{"projectId":"legacy"}`), 409},
		{"ArchivedDestContrib409", zgPatch("ben", "web", "t1", `{"projectId":"legacy"}`), 409},
		{"OtherOrgDest404", zgPatch("ivy", "web", "t3", `{"projectId":"gx"}`), 404},
		{"UnknownDest404", zgPatch("ivy", "web", "t3", `{"projectId":"nope"}`), 404},
		{"NonMemberDest404", zgPatch("ben", "web", "t1", `{"projectId":"ops"}`), 404},
		{"ViewerSource403", zgPatch("ann", "ops", "t4", `{"projectId":"web"}`), 403},
		{"OrgAdminSource403", zgPatch("eve", "web", "t3", `{"projectId":"ops"}`), 403},
		{"NonMemberSource404", zgPatch("hal", "web", "t3", `{"projectId":"ops"}`), 404},
		{"CrossOrgSource404", zgPatch("gus", "web", "t1", `{"projectId":"gx"}`), 404},
		{"ContribOthersTask403", zgPatch("cat", "web", "t1", `{"projectId":"ops"}`), 403},
		{"ContribReassignWhileMoving403", zgPatch("cat", "web", "t2", `{"projectId":"ops","assignee":"ivy"}`), 403},
		{"ArchivedSource409", zgPatch("ann", "legacy", "t5", `{"projectId":"web"}`), 409},
		{"SourceNotInPath404", zgPatch("ivy", "web", "t4", `{"projectId":"ops"}`), 404},
		{"Anon401", zgPatch("", "web", "t3", `{"projectId":"ops"}`), 401},
		{"InvalidStatus400", zgPatch("ivy", "web", "t2", `{"projectId":"ops","status":"bogus"}`), 400},
		{"MalformedBody400", zgPatch("ivy", "web", "t2", `{"projectId":`), 400},
	} {
		c := c
		t.Run("CONTRACT_"+c.n, func(t *testing.T) { zgDenied(t, c.r, c.w) })
	}

	// ---------- INFO (log only) ----------
	logPatch := func(t *testing.T, r zgReq) {
		h := newServer()
		before := zgSnap(t, h)
		code, body := zgDo(h, r.user, r.method, r.path, r.body)
		after := zgSnap(t, h)
		t.Logf("INFO %s %s %s as %q -> %d; diff=[%s]; body=%s", r.method, r.path, r.body, r.user, code, zgDiff(before, after), strings.TrimSpace(body))
	}
	for _, c := range []struct {
		n string
		r zgReq
	}{
		{"AssigneeValidOnlyInDest_IvyOpsT4ToWebBen", zgPatch("ivy", "ops", "t4", `{"projectId":"web","assignee":"ben"}`)},
		{"SameProjectId_IvyT2", zgPatch("ivy", "web", "t2", `{"projectId":"web"}`)},
		{"SameProjectIdWithTitle_IvyT2", zgPatch("ivy", "web", "t2", `{"projectId":"web","title":"Same"}`)},
		{"SameProjectId_ContribCatT2", zgPatch("cat", "web", "t2", `{"projectId":"web"}`)},
		{"SameProjectId_ViewerDanT3", zgPatch("dan", "web", "t3", `{"projectId":"web"}`)},
		{"EmptyProjectId_IvyT2", zgPatch("ivy", "web", "t2", `{"projectId":""}`)},
		{"EmptyProjectIdWithTitle_IvyT2", zgPatch("ivy", "web", "t2", `{"projectId":"","title":"E"}`)},
		{"NullProjectId_IvyT2", zgPatch("ivy", "web", "t2", `{"projectId":null,"title":"N"}`)},
		{"ContribOwnMoveWithAssigneeSelf_CatT2", zgPatch("cat", "web", "t2", `{"projectId":"ops","assignee":"cat"}`)},
		{"OrderSourceUnreadableDestUnknown_HalT1", zgPatch("hal", "web", "t1", `{"projectId":"nope"}`)},
		{"OrderViewerSourceDestUnreadable_DanT3", zgPatch("dan", "web", "t3", `{"projectId":"ops"}`)},
		{"OrderArchivedSourceDestViewer_AnnT5ToOps", zgPatch("ann", "legacy", "t5", `{"projectId":"ops"}`)},
		{"OrderBadStatusVsDestViewer_AnnT2", zgPatch("ann", "web", "t2", `{"projectId":"ops","status":"bogus"}`)},
		{"OrderBadStatusVsBadAssignee_IvyT1", zgPatch("ivy", "web", "t1", `{"projectId":"ops","status":"bogus"}`)},
		{"OrderArchivedDestVsBadAssignee_AnnT2", zgPatch("ann", "web", "t2", `{"projectId":"legacy"}`)},
		{"OrderContribNotOwnVsDestMissing_CatT1ToNope", zgPatch("cat", "web", "t1", `{"projectId":"nope"}`)},
		{"EveToGx_T5", zgPatch("eve", "legacy", "t5", `{"projectId":"gx"}`)},
	} {
		c := c
		t.Run("INFO_"+c.n, func(t *testing.T) { logPatch(t, c.r) })
	}
}
