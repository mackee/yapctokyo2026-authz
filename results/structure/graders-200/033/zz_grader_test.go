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

// Hidden grader for task 033 (POST /projects/{pid}/members/{uid}/move {"toProjectId":...}).
// Uses only newServer() and HTTP. Roles are observed through non-mutating probes (see zgSig).

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

func zgMoveRaw(h http.Handler, user, pid, uid, body string) (int, string) {
	return zgDo(h, user, "POST", "/projects/"+pid+"/members/"+uid+"/move", body)
}

func zgMove(h http.Handler, user, pid, uid, to string) (int, string) {
	return zgMoveRaw(h, user, pid, uid, `{"toProjectId":"`+to+`"}`)
}

var zgSeed = map[string]zgTask{
	"t1": {"t1", "web", "Fix header", "open", "ben"},
	"t2": {"t2", "web", "Update footer", "doing", "cat"},
	"t3": {"t3", "web", "Write copy", "open", ""},
	"t4": {"t4", "ops", "Rotate keys", "open", "cat"},
	"t5": {"t5", "legacy", "Old bug", "open", "ben"},
	"t6": {"t6", "gx", "Globex secret plan", "open", "gus"},
}

var zgReadable = map[string][]string{
	"ann": {"web", "ops", "legacy"}, "ben": {"web", "legacy"}, "cat": {"web", "ops"},
	"dan": {"web"}, "eve": {"web", "ops", "legacy"}, "ivy": {"web", "ops"}, "gus": {"gx"},
}

func zgLeaks(user, body string) []string {
	ok := map[string]bool{}
	for _, p := range zgReadable[user] {
		ok[p] = true
	}
	var l []string
	for _, tk := range zgSeed {
		if !ok[tk.ProjectID] && strings.Contains(body, tk.Title) {
			l = append(l, tk.Title)
		}
	}
	return l
}

// zgTasks reads every task through existing endpoints (eve is org admin, gus reads gx).
func zgTasks(t *testing.T, h http.Handler) map[string]zgTask {
	t.Helper()
	out := map[string]zgTask{}
	for _, c := range []struct{ user, pid string }{{"eve", "web"}, {"eve", "ops"}, {"eve", "legacy"}, {"gus", "gx"}} {
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

// zgSig observes every user's role in web/ops/legacy without mutating anything:
//
//	PATCH <task> {"assignee":"zzz"}: 404 non-member, 403 viewer/org admin/contributor, 400 manager (409 if archived, 403 viewer)
//	POST  tasks  {}               : 404 non-member, 403 viewer/org admin, 400 contributor/manager (409 if archived)
//
// and gx membership by GET (200 member, 404 otherwise).
func zgSig(h http.Handler) map[string]string {
	out := map[string]string{}
	us := []string{"ann", "ben", "cat", "dan", "eve", "hal", "ivy", "gus"}
	for _, p := range []struct{ pid, tid string }{{"web", "t3"}, {"ops", "t4"}, {"legacy", "t5"}} {
		for _, u := range us {
			c1, _ := zgDo(h, u, "PATCH", "/projects/"+p.pid+"/tasks/"+p.tid, `{"assignee":"zzz"}`)
			c2, _ := zgDo(h, u, "POST", "/projects/"+p.pid+"/tasks", `{"title":"probe","assignee":"zzz"}`)
			out[p.pid+"/"+u] = fmt.Sprintf("%d/%d", c1, c2)
		}
	}
	for _, u := range us {
		c, _ := zgDo(h, u, "GET", "/projects/gx/tasks", "")
		out["gx/"+u] = fmt.Sprintf("%d", c)
	}
	return out
}

type zgState struct {
	tasks map[string]zgTask
	sig   map[string]string
}

func zgSnap(t *testing.T, h http.Handler) zgState {
	t.Helper()
	return zgState{zgTasks(t, h), zgSig(h)}
}

func zgDiff(a, b zgState) string {
	var d []string
	for id, x := range a.tasks {
		if y, ok := b.tasks[id]; !ok {
			d = append(d, id+" vanished")
		} else if x != y {
			d = append(d, fmt.Sprintf("%s %+v -> %+v", id, x, y))
		}
	}
	for id, y := range b.tasks {
		if _, ok := a.tasks[id]; !ok {
			d = append(d, fmt.Sprintf("%s appeared %+v", id, y))
		}
	}
	for k, x := range a.sig {
		if y := b.sig[k]; x != y {
			d = append(d, fmt.Sprintf("role-probe %s %s -> %s", k, x, y))
		}
	}
	sort.Strings(d)
	return strings.Join(d, "; ")
}

func zgMust(t *testing.T, h http.Handler, user, method, path, body string, want int) string {
	t.Helper()
	code, resp := zgDo(h, user, method, path, body)
	if code != want {
		t.Fatalf("%s %s as %s -> %d, want %d body=%s", method, path, user, code, want, resp)
	}
	return resp
}

func zgMoveOK(t *testing.T, h http.Handler, user, pid, uid, to string) string {
	t.Helper()
	code, body := zgMove(h, user, pid, uid, to)
	if code != 200 {
		t.Fatalf("move %s/%s -> %s as %s -> %d body=%s", pid, uid, to, user, code, body)
	}
	return body
}

// zgInvariant: in web/ops every assignee must currently be a contributor or manager there.
func zgInvariant(t *testing.T, st zgState) {
	t.Helper()
	for _, x := range st.tasks {
		if x.Assignee == "" || (x.ProjectID != "web" && x.ProjectID != "ops") {
			continue
		}
		s := st.sig[x.ProjectID+"/"+x.Assignee]
		if s != "403/400" && s != "400/400" {
			t.Errorf("invariant violated: task %s in %s is assigned to %s who is not a contributor/manager there (probe %s)", x.ID, x.ProjectID, x.Assignee, s)
		}
	}
}

type zgReq struct{ user, pid, uid, body string }

// zgDenied: non-2xx, nothing changed (tasks, roles in web/ops/legacy/gx), no leak.
func zgDeniedOn(t *testing.T, h http.Handler, r zgReq) int {
	t.Helper()
	before := zgSnap(t, h)
	code, resp := zgMoveRaw(h, r.user, r.pid, r.uid, r.body)
	after := zgSnap(t, h)
	if code >= 200 && code < 300 {
		t.Errorf("unexpected acceptance: move %s/%s %s as %q -> %d body=%s", r.pid, r.uid, r.body, r.user, code, resp)
	}
	if d := zgDiff(before, after); d != "" {
		t.Errorf("state changed: %s", d)
	}
	if l := zgLeaks(r.user, resp); len(l) > 0 {
		t.Errorf("leak: response exposes titles %v: %s", l, resp)
	}
	return code
}

func zgDenied(t *testing.T, user, pid, uid, to string) {
	t.Helper()
	zgDeniedOn(t, newServer(), zgReq{user, pid, uid, `{"toProjectId":"` + to + `"}`})
}

// zgAcceptableMove: the move is either rejected with nothing changed, or succeeds (200), uid lost
// access to src, has the same role in dst as it had in src, the assignee invariant holds, and the
// ONLY collateral is unassigning uid's tasks in src.
func zgAcceptableMove(t *testing.T, actor, src, uid, dst string, setup func(h http.Handler)) {
	t.Helper()
	h := newServer()
	if setup != nil {
		setup(h)
	}
	before := zgSnap(t, h)
	code, body := zgMove(h, actor, src, uid, dst)
	after := zgSnap(t, h)
	if code >= 200 && code < 300 {
		if code != 200 {
			t.Errorf("success status %d, want 200", code)
		}
		if after.sig[src+"/"+uid] != "404/404" {
			t.Errorf("2xx but %s still has access to %s (probe %s)", uid, src, after.sig[src+"/"+uid])
		}
		if c, _ := zgDo(h, uid, "GET", "/projects/"+src+"/tasks", ""); c != 404 {
			t.Errorf("2xx but %s GET %s/tasks -> %d, want 404", uid, src, c)
		}
		if after.sig[dst+"/"+uid] != before.sig[src+"/"+uid] {
			t.Errorf("2xx but %s role in %s is probe %s, want same as in %s (%s)", uid, dst, after.sig[dst+"/"+uid], src, before.sig[src+"/"+uid])
		}
		zgInvariant(t, after)
		for id, b := range before.tasks {
			a := after.tasks[id]
			u := b
			if b.ProjectID == src && b.Assignee == uid {
				u.Assignee = ""
			}
			if a != b && a != u {
				t.Errorf("collateral change: %s %+v -> %+v (only unassigning %s's tasks in %s is allowed)", id, b, a, uid, src)
			}
		}
		for k, x := range before.sig {
			if k == src+"/"+uid || k == dst+"/"+uid {
				continue
			}
			if y := after.sig[k]; x != y {
				t.Errorf("collateral role change: probe %s %s -> %s", k, x, y)
			}
		}
	} else if d := zgDiff(before, after); d != "" {
		t.Errorf("state changed despite %d: %s (body=%s)", code, d, body)
	}
}

func zgUnassign(t *testing.T, h http.Handler, pid, tid string) {
	t.Helper()
	zgMust(t, h, "ivy", "PATCH", "/projects/"+pid+"/tasks/"+tid, `{"assignee":""}`, 200)
}

func TestGrader(t *testing.T) {
	// ---------- REG ----------
	for _, c := range []struct {
		n, user, method, path, body string
		want                        int
	}{
		{"ListWeb", "ann", "GET", "/projects/web/tasks", "", 200},
		{"ListLegacyArchived", "ben", "GET", "/projects/legacy/tasks", "", 200},
		{"ListWebNonMember404", "hal", "GET", "/projects/web/tasks", "", 404},
		{"ListWebOtherOrg404", "gus", "GET", "/projects/web/tasks", "", 404},
		{"ListAnon401", "", "GET", "/projects/web/tasks", "", 401},
		{"GetTask", "dan", "GET", "/projects/web/tasks/t1", "", 200},
		{"CreateViewer403", "dan", "POST", "/projects/web/tasks", `{"title":"x"}`, 403},
		{"CreateArchived409", "ann", "POST", "/projects/legacy/tasks", `{"title":"x"}`, 409},
		{"CreateOK", "ann", "POST", "/projects/web/tasks", `{"title":"x","assignee":"ben"}`, 200},
		{"CreateBadAssignee400", "ann", "POST", "/projects/web/tasks", `{"title":"x","assignee":"dan"}`, 400},
		{"PatchContributorOthers403", "ben", "PATCH", "/projects/web/tasks/t2", `{"status":"done"}`, 403},
		{"PatchContributorOwn", "ben", "PATCH", "/projects/web/tasks/t1", `{"status":"doing"}`, 200},
		{"PatchContributorReassign403", "ben", "PATCH", "/projects/web/tasks/t1", `{"assignee":"cat"}`, 403},
		{"PatchManagerReassign", "ivy", "PATCH", "/projects/web/tasks/t1", `{"assignee":"cat"}`, 200},
		{"PatchManagerBadAssignee400", "ivy", "PATCH", "/projects/web/tasks/t1", `{"assignee":"dan"}`, 400},
		{"PatchManagerUnassign", "ivy", "PATCH", "/projects/web/tasks/t1", `{"assignee":""}`, 200},
		{"PatchArchived409", "ann", "PATCH", "/projects/legacy/tasks/t5", `{"status":"done"}`, 409},
		{"DeleteContributor403", "ben", "DELETE", "/projects/web/tasks/t1", "", 403},
		{"DeleteManager", "ann", "DELETE", "/projects/web/tasks/t3", "", 204},
		{"DeleteOrgAdmin403", "eve", "DELETE", "/projects/web/tasks/t1", "", 403},
	} {
		c := c
		t.Run("REG_"+c.n, func(t *testing.T) {
			h := newServer()
			code, body := zgDo(h, c.user, c.method, c.path, c.body)
			if code != c.want {
				t.Errorf("%s %s as %q -> %d, want %d body=%s", c.method, c.path, c.user, code, c.want, body)
			}
		})
	}
	t.Run("REG_ProbeSanity", func(t *testing.T) {
		sig := zgSig(newServer())
		want := map[string]string{
			"web/ann": "400/400", "web/ben": "403/400", "web/dan": "403/403", "web/eve": "403/403",
			"web/hal": "404/404", "web/ivy": "400/400", "web/gus": "404/404",
			"ops/ann": "403/403", "ops/cat": "403/400", "ops/ivy": "400/400", "ops/ben": "404/404", "ops/dan": "404/404",
			"legacy/ann": "409/409", "legacy/ben": "409/409", "legacy/cat": "404/404",
			"gx/gus": "200", "gx/ann": "404", "gx/ivy": "404", "gx/eve": "404", "gx/dan": "404",
		}
		for k, v := range want {
			if sig[k] != v {
				t.Errorf("probe %s = %s, want %s", k, sig[k], v)
			}
		}
	})

	// ---------- FUNC (never depend on the assignee policy: dan has no tasks, or ben is unassigned first) ----------
	t.Run("FUNC_MoveViewerDan_200Body", func(t *testing.T) {
		h := newServer()
		body := zgMoveOK(t, h, "ivy", "web", "dan", "ops")
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(body), &m); err != nil {
			t.Fatalf("body not JSON: %s", body)
		}
		if m["userId"] != "dan" || m["projectId"] != "ops" || m["role"] != "viewer" {
			t.Errorf("body = %s, want userId dan, projectId ops, role viewer", body)
		}
	})
	t.Run("FUNC_MovedViewerAccess", func(t *testing.T) {
		h := newServer()
		zgMust(t, h, "dan", "GET", "/projects/web/tasks", "", 200)
		zgMust(t, h, "dan", "GET", "/projects/ops/tasks", "", 404)
		zgMoveOK(t, h, "ivy", "web", "dan", "ops")
		zgMust(t, h, "dan", "GET", "/projects/web/tasks", "", 404)
		zgMust(t, h, "dan", "GET", "/projects/web/tasks/t1", "", 404)
		zgMust(t, h, "dan", "GET", "/projects/ops/tasks", "", 200)
		zgMust(t, h, "dan", "GET", "/projects/ops/tasks/t4", "", 200)
	})
	t.Run("FUNC_MovedViewerKeepsViewerRole", func(t *testing.T) {
		h := newServer()
		zgMoveOK(t, h, "ivy", "web", "dan", "ops")
		sig := zgSig(h)
		if sig["ops/dan"] != "403/403" || sig["web/dan"] != "404/404" {
			t.Errorf("dan probes ops=%s web=%s, want 403/403 and 404/404", sig["ops/dan"], sig["web/dan"])
		}
		zgMust(t, h, "dan", "POST", "/projects/ops/tasks", `{"title":"x"}`, 403)
		zgMust(t, h, "dan", "PATCH", "/projects/ops/tasks/t4", `{"status":"done"}`, 403)
	})
	t.Run("FUNC_MoveDoesNotTouchOthers", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		zgMoveOK(t, h, "ivy", "web", "dan", "ops")
		after := zgSnap(t, h)
		for id, b := range before.tasks {
			if after.tasks[id] != b {
				t.Errorf("task %s changed: %+v -> %+v", id, b, after.tasks[id])
			}
		}
		for k, x := range before.sig {
			if k != "web/dan" && k != "ops/dan" && after.sig[k] != x {
				t.Errorf("probe %s %s -> %s", k, x, after.sig[k])
			}
		}
		zgMust(t, h, "eve", "GET", "/projects/web/tasks", "", 200)
	})
	t.Run("FUNC_MoveContributorBen_AfterUnassign", func(t *testing.T) {
		h := newServer()
		zgUnassign(t, h, "web", "t1")
		body := zgMoveOK(t, h, "ivy", "web", "ben", "ops")
		var m map[string]interface{}
		json.Unmarshal([]byte(body), &m)
		if m["userId"] != "ben" || m["projectId"] != "ops" || m["role"] != "contributor" {
			t.Errorf("body = %s, want userId ben, projectId ops, role contributor", body)
		}
		sig := zgSig(h)
		if sig["ops/ben"] != "403/400" || sig["web/ben"] != "404/404" {
			t.Errorf("ben probes ops=%s web=%s, want 403/400 and 404/404", sig["ops/ben"], sig["web/ben"])
		}
	})
	t.Run("FUNC_MovedContributorCanWorkInDestination", func(t *testing.T) {
		h := newServer()
		zgUnassign(t, h, "web", "t1")
		zgMoveOK(t, h, "ivy", "web", "ben", "ops")
		zgMust(t, h, "ben", "POST", "/projects/ops/tasks", `{"title":"from ben"}`, 200)
		zgMust(t, h, "ben", "PATCH", "/projects/ops/tasks/t4", `{"status":"done"}`, 403)
		zgMust(t, h, "ivy", "PATCH", "/projects/ops/tasks/t4", `{"assignee":"ben"}`, 200)
		zgMust(t, h, "ben", "PATCH", "/projects/ops/tasks/t4", `{"status":"doing"}`, 200)
	})
	t.Run("FUNC_MovedContributorLostSource", func(t *testing.T) {
		h := newServer()
		zgUnassign(t, h, "web", "t1")
		zgMoveOK(t, h, "ivy", "web", "ben", "ops")
		zgMust(t, h, "ben", "GET", "/projects/web/tasks", "", 404)
		zgMust(t, h, "ben", "POST", "/projects/web/tasks", `{"title":"x"}`, 404)
		zgMust(t, h, "ben", "PATCH", "/projects/web/tasks/t1", `{"status":"done"}`, 404)
		zgMust(t, h, "ivy", "PATCH", "/projects/web/tasks/t3", `{"assignee":"ben"}`, 400)
		zgMust(t, h, "ivy", "POST", "/projects/web/tasks", `{"title":"x","assignee":"ben"}`, 400)
	})
	t.Run("FUNC_MoveIsPerProject_LegacyKept", func(t *testing.T) {
		h := newServer()
		zgUnassign(t, h, "web", "t1")
		zgMoveOK(t, h, "ivy", "web", "ben", "ops")
		zgMust(t, h, "ben", "GET", "/projects/legacy/tasks", "", 200)
		if s := zgSig(h)["legacy/ben"]; s != "409/409" {
			t.Errorf("ben lost legacy membership: probe %s", s)
		}
		if _, b := zgDo(h, "ann", "GET", "/projects/legacy/tasks/t5", ""); !strings.Contains(b, `"ben"`) {
			t.Errorf("legacy t5 assignee changed: %s", b)
		}
	})
	t.Run("FUNC_MoveTwiceSecondRejected", func(t *testing.T) {
		h := newServer()
		zgMoveOK(t, h, "ivy", "web", "dan", "ops")
		if code, body := zgMove(h, "ivy", "web", "dan", "ops"); code >= 200 && code < 300 {
			t.Errorf("second move accepted: %d %s", code, body)
		}
		if s := zgSig(h)["ops/dan"]; s != "403/403" {
			t.Errorf("dan ops probe %s, want 403/403", s)
		}
	})
	t.Run("FUNC_MoveBackAndForth", func(t *testing.T) {
		h := newServer()
		zgMoveOK(t, h, "ivy", "web", "dan", "ops")
		zgMoveOK(t, h, "ivy", "ops", "dan", "web")
		sig := zgSig(h)
		if sig["web/dan"] != "403/403" || sig["ops/dan"] != "404/404" {
			t.Errorf("dan probes web=%s ops=%s, want 403/403 and 404/404", sig["web/dan"], sig["ops/dan"])
		}
		zgMust(t, h, "dan", "GET", "/projects/web/tasks", "", 200)
	})
	t.Run("FUNC_MoveCatUnassignedOpsToWeb_Rejected", func(t *testing.T) {
		// cat is a contributor in both: moving ops->web must not drop her from web or change role
		h := newServer()
		zgUnassign(t, h, "ops", "t4")
		code, _ := zgMove(h, "ivy", "ops", "cat", "web")
		sig := zgSig(h)
		if sig["web/cat"] != "403/400" {
			t.Errorf("after move attempt (%d) cat web probe %s, want 403/400", code, sig["web/cat"])
		}
		if code >= 200 && code < 300 && sig["ops/cat"] != "404/404" {
			t.Errorf("2xx but cat still in ops: %s", sig["ops/cat"])
		}
		if code >= 300 && sig["ops/cat"] != "403/400" {
			t.Errorf("rejected but cat ops probe %s", sig["ops/cat"])
		}
		zgMust(t, h, "cat", "PATCH", "/projects/web/tasks/t2", `{"status":"done"}`, 200)
	})
	t.Run("FUNC_AssignedBenMoveEndsConsistently", func(t *testing.T) {
		// without any setup: whichever policy, a later manager unassign + move must work
		h := newServer()
		zgMove(h, "ivy", "web", "ben", "ops")
		code, _ := zgDo(h, "ivy", "GET", "/projects/ops/tasks", "")
		if code != 200 {
			t.Fatalf("ivy GET ops -> %d", code)
		}
		if s := zgSig(h)["ops/ben"]; s == "404/404" {
			// rejected: the documented way out must work
			zgUnassign(t, h, "web", "t1")
			zgMoveOK(t, h, "ivy", "web", "ben", "ops")
		} else if s != "403/400" {
			t.Errorf("ben ops probe %s, want 404/404 or 403/400", s)
		}
	})

	// ---------- ATOM: assignee invariant ----------
	// Accept either rejection (non-2xx, nothing changed) or success with only that user's tasks
	// in the source project unassigned. A moved user left as assignee in the source is the failure.
	for _, c := range []struct{ n, actor, src, uid, dst string }{
		{"MoveAssigneeBenWebToOps", "ivy", "web", "ben", "ops"},
		{"MoveAssigneeCatOpsToWeb", "ivy", "ops", "cat", "web"},
		{"MoveAssigneeCatWebToOps", "ivy", "web", "cat", "ops"},
	} {
		c := c
		t.Run("ATOM_"+c.n, func(t *testing.T) { zgAcceptableMove(t, c.actor, c.src, c.uid, c.dst, nil) })
	}
	t.Run("ATOM_TwoTasksOfBen", func(t *testing.T) {
		zgAcceptableMove(t, "ivy", "web", "ben", "ops", func(h http.Handler) {
			zgMust(t, h, "ann", "POST", "/projects/web/tasks", `{"title":"Second ben task","assignee":"ben"}`, 200)
			zgMust(t, h, "ivy", "PATCH", "/projects/web/tasks/t3", `{"assignee":"ben"}`, 200)
		})
	})
	t.Run("ATOM_RoundTripOpsToWeb", func(t *testing.T) {
		// ben (unassigned) goes web->ops, gets an ops task, then goes back ops->web
		h := newServer()
		zgUnassign(t, h, "web", "t1")
		if code, _ := zgMove(h, "ivy", "web", "ben", "ops"); code != 200 {
			return // FUNC_ covers the plain move
		}
		if c, _ := zgDo(h, "ivy", "PATCH", "/projects/ops/tasks/t4", `{"assignee":"ben"}`); c != 200 {
			return
		}
		before := zgSnap(t, h)
		code, body := zgMove(h, "ivy", "ops", "ben", "web")
		after := zgSnap(t, h)
		if code >= 200 && code < 300 {
			if after.sig["ops/ben"] != "404/404" {
				t.Errorf("2xx but ben still in ops: %s", after.sig["ops/ben"])
			}
			if after.sig["web/ben"] != "403/400" {
				t.Errorf("2xx but ben web probe %s, want 403/400", after.sig["web/ben"])
			}
			zgInvariant(t, after)
			if after.tasks["t4"].Assignee != "" && after.tasks["t4"].Assignee != "ben" {
				t.Errorf("t4 assignee %q", after.tasks["t4"].Assignee)
			}
			if after.tasks["t2"] != before.tasks["t2"] {
				t.Errorf("web t2 changed: %+v", after.tasks["t2"])
			}
		} else if d := zgDiff(before, after); d != "" {
			t.Errorf("state changed despite %d: %s (body=%s)", code, d, body)
		}
	})
	t.Run("ATOM_DestinationTasksUntouched_catOpsToWeb", func(t *testing.T) {
		h := newServer()
		zgMove(h, "ivy", "ops", "cat", "web")
		_, b := zgDo(h, "eve", "GET", "/projects/web/tasks/t2", "")
		var x zgTask
		json.Unmarshal([]byte(b), &x)
		if x.Assignee != "cat" {
			t.Errorf("web t2 (cat's task in the destination) assignee = %q, must stay cat", x.Assignee)
		}
		if c, _ := zgDo(h, "cat", "PATCH", "/projects/web/tasks/t2", `{"status":"done"}`); c != 200 {
			t.Errorf("cat can no longer work on her web task: %d", c)
		}
	})
	t.Run("ATOM_DestinationTasksUntouched_catWebToOps", func(t *testing.T) {
		h := newServer()
		zgMove(h, "ivy", "web", "cat", "ops")
		_, b := zgDo(h, "eve", "GET", "/projects/ops/tasks/t4", "")
		var x zgTask
		json.Unmarshal([]byte(b), &x)
		if x.Assignee != "cat" {
			t.Errorf("ops t4 (cat's task in the destination) assignee = %q, must stay cat", x.Assignee)
		}
		if c, _ := zgDo(h, "cat", "PATCH", "/projects/ops/tasks/t4", `{"status":"doing"}`); c != 200 {
			t.Errorf("cat can no longer work on her ops task: %d", c)
		}
	})
	t.Run("ATOM_LegacyTaskOfBenUntouched", func(t *testing.T) {
		h := newServer()
		code, _ := zgMove(h, "ivy", "web", "ben", "ops")
		_, b := zgDo(h, "ann", "GET", "/projects/legacy/tasks/t5", "")
		var x zgTask
		json.Unmarshal([]byte(b), &x)
		if x.Assignee != "ben" {
			t.Errorf("move web/ben -> %d but legacy t5 assignee = %q (must stay ben)", code, x.Assignee)
		}
		if c, _ := zgDo(h, "ben", "GET", "/projects/legacy/tasks", ""); c != 200 {
			t.Errorf("ben lost legacy read: %d", c)
		}
	})
	t.Run("ATOM_ConsistentAfterBenAttempt", func(t *testing.T) {
		h := newServer()
		code, _ := zgMove(h, "ivy", "web", "ben", "ops")
		c2, _ := zgDo(h, "ivy", "PATCH", "/projects/web/tasks/t1", `{"assignee":"ben"}`)
		if code >= 200 && code < 300 && c2 != 400 {
			t.Errorf("moved ben can still be assigned in web: PATCH -> %d", c2)
		}
		if code >= 300 && c2 != 200 {
			t.Errorf("rejected move but ben (still member of web) cannot be assigned: PATCH -> %d", c2)
		}
		zgInvariant(t, zgSnap(t, h))
	})
	t.Run("ATOM_MovedAssigneeCannotStillModifyOldTask", func(t *testing.T) {
		h := newServer()
		code, _ := zgMove(h, "ivy", "web", "ben", "ops")
		if code < 200 || code >= 300 {
			return
		}
		if c, b := zgDo(h, "ben", "PATCH", "/projects/web/tasks/t1", `{"status":"done"}`); c >= 200 && c < 300 {
			t.Errorf("unexpected acceptance: moved ben PATCH web t1 -> %d %s", c, b)
		}
		_, tb := zgDo(h, "eve", "GET", "/projects/web/tasks/t1", "")
		if strings.Contains(tb, `"done"`) {
			t.Errorf("state changed by moved user: %s", tb)
		}
	})

	// ---------- AUTHZ ----------
	for _, c := range []struct{ n, user, src, uid, dst string }{
		// caller manages only the source (likely mistake: source-only check)
		{"ManagerOfSourceOnly_annMovesBen", "ann", "web", "ben", "ops"},
		{"ManagerOfSourceOnly_annMovesDan", "ann", "web", "dan", "ops"},
		{"ManagerOfSourceOnly_annMovesCat", "ann", "web", "cat", "ops"},
		// caller manages only the destination / is viewer in the source
		{"ViewerInSource_annMovesCat", "ann", "ops", "cat", "web"},
		{"ViewerInSource_annMovesIvy", "ann", "ops", "ivy", "web"},
		{"ViewerInSource_annMovesSelf", "ann", "ops", "ann", "web"},
		// contributors, viewers, org admin
		{"Contributor_benMovesDan", "ben", "web", "dan", "ops"},
		{"Contributor_benMovesSelf", "ben", "web", "ben", "ops"},
		{"Contributor_catMovesDan", "cat", "web", "dan", "ops"},
		{"Contributor_catMovesIvyOpsToWeb", "cat", "ops", "ivy", "web"},
		{"Contributor_catMovesSelfOpsToWeb", "cat", "ops", "cat", "web"},
		{"Viewer_danMovesSelf", "dan", "web", "dan", "ops"},
		{"Viewer_danMovesBen", "dan", "web", "ben", "ops"},
		{"OrgAdmin_eveMovesDan", "eve", "web", "dan", "ops"},
		{"OrgAdmin_eveMovesBen", "eve", "web", "ben", "ops"},
		{"OrgAdmin_eveMovesCatOps", "eve", "ops", "cat", "web"},
		// non-members, cross-org, identity
		{"NonMember_hal", "hal", "web", "dan", "ops"},
		{"NonMember_benInOps", "ben", "ops", "cat", "web"},
		{"NonMember_danInOps", "dan", "ops", "ivy", "web"},
		{"CrossOrg_gusInWeb", "gus", "web", "dan", "ops"},
		{"CrossOrg_gusInWebManager", "gus", "web", "ann", "ops"},
		{"CrossOrg_annInGx", "ann", "gx", "gus", "web"},
		{"CrossOrg_ivyInGx", "ivy", "gx", "gus", "ops"},
		{"CrossOrg_eveInGx", "eve", "gx", "gus", "web"},
		{"CrossOrg_gusMovesSelfToWeb", "gus", "gx", "gus", "web"},
		{"Anonymous", "", "web", "dan", "ops"},
		{"UnknownUser", "zed", "web", "dan", "ops"},
		{"UnknownSourceProject", "ivy", "nope", "dan", "ops"},
		// destination in another org / foreign
		{"DestOtherOrg_ivyMovesDan", "ivy", "web", "dan", "gx"},
		{"DestOtherOrg_ivyMovesCat", "ivy", "ops", "cat", "gx"},
		{"DestOtherOrg_annMovesDan", "ann", "web", "dan", "gx"},
		{"DestOtherOrg_gusMovesSelfFromGx", "gus", "gx", "gus", "gx"},
		// destination unknown
		{"DestUnknown_ivy", "ivy", "web", "dan", "nope"},
		// archived destination (ann manages both web and legacy)
		{"ArchivedDest_annMovesDan", "ann", "web", "dan", "legacy"},
		{"ArchivedDest_ivyMovesDan", "ivy", "web", "dan", "legacy"},
		{"ArchivedDest_annMovesCat", "ann", "web", "cat", "legacy"},
		// archived source
		{"ArchivedSource_annMovesBen", "ann", "legacy", "ben", "web"},
		{"ArchivedSource_annMovesBenToOps", "ann", "legacy", "ben", "ops"},
		{"ArchivedSource_benMovesSelf", "ben", "legacy", "ben", "web"},
		{"ArchivedSource_ivy", "ivy", "legacy", "ben", "ops"},
		// target not a member of the source
		{"TargetNotMember_hal", "ivy", "web", "hal", "ops"},
		{"TargetNotMember_eveAdmin", "ivy", "web", "eve", "ops"},
		{"TargetNotMember_gus", "ivy", "web", "gus", "ops"},
		{"TargetNotMember_zed", "ivy", "web", "zed", "ops"},
		{"TargetNotMember_danInOps", "ivy", "ops", "dan", "web"},
		{"TargetNotMember_benInOps", "ivy", "ops", "ben", "web"},
	} {
		c := c
		t.Run("AUTHZ_"+c.n, func(t *testing.T) { zgDenied(t, c.user, c.src, c.uid, c.dst) })
	}
	t.Run("AUTHZ_ArchivedSource_ManagerOfBoth_AfterSetup", func(t *testing.T) {
		// ben leaves web (moved to ops) so ann (manager of legacy and web) tries legacy->web with a free destination
		h := newServer()
		zgUnassign(t, h, "web", "t1")
		if code, _ := zgMove(h, "ivy", "web", "ben", "ops"); code != 200 {
			return
		}
		zgDeniedOn(t, h, zgReq{"ann", "legacy", "ben", `{"toProjectId":"web"}`})
	})
	t.Run("AUTHZ_AnnCannotCompleteMoveViaTwoSteps", func(t *testing.T) {
		h := newServer()
		for _, c := range [][3]string{{"web", "dan", "ops"}, {"web", "ben", "ops"}, {"web", "cat", "ops"}} {
			zgMove(h, "ann", c[0], c[1], c[2])
		}
		sig := zgSig(h)
		if sig["ops/dan"] != "404/404" || sig["ops/ben"] != "404/404" || sig["web/dan"] != "403/403" || sig["web/ben"] != "403/400" {
			t.Errorf("ann changed memberships: %v", sig)
		}
	})
	t.Run("AUTHZ_DeniedNoLeakBodies", func(t *testing.T) {
		h := newServer()
		for _, r := range []zgReq{
			{"hal", "web", "dan", `{"toProjectId":"ops"}`}, {"gus", "web", "dan", `{"toProjectId":"ops"}`},
			{"", "web", "dan", `{"toProjectId":"ops"}`}, {"ann", "gx", "gus", `{"toProjectId":"web"}`},
			{"ivy", "web", "dan", `{"toProjectId":"gx"}`}, {"ann", "web", "dan", `{"toProjectId":"gx"}`},
		} {
			_, body := zgMoveRaw(h, r.user, r.pid, r.uid, r.body)
			if l := zgLeaks(r.user, body); len(l) > 0 {
				t.Errorf("leak: %v in %s", l, body)
			}
		}
	})
	t.Run("AUTHZ_NobodyAddedToGx", func(t *testing.T) {
		h := newServer()
		for _, r := range []zgReq{
			{"ivy", "web", "dan", `{"toProjectId":"gx"}`}, {"ivy", "ops", "cat", `{"toProjectId":"gx"}`},
			{"ann", "web", "ben", `{"toProjectId":"gx"}`}, {"eve", "web", "dan", `{"toProjectId":"gx"}`},
			{"gus", "gx", "gus", `{"toProjectId":"gx"}`},
		} {
			zgMoveRaw(h, r.user, r.pid, r.uid, r.body)
		}
		sig := zgSig(h)
		for _, u := range []string{"ann", "ben", "cat", "dan", "eve", "hal", "ivy"} {
			if sig["gx/"+u] != "404" {
				t.Errorf("%s gained access to gx: %s", u, sig["gx/"+u])
			}
		}
		if sig["web/dan"] != "403/403" || sig["ops/cat"] != "403/400" {
			t.Errorf("members vanished from source projects: %v", sig)
		}
		zgMust(t, h, "gus", "GET", "/projects/gx/tasks", "", 200)
	})
	t.Run("AUTHZ_DeniedMoveKeepsEveryone", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		for _, r := range []zgReq{
			{"ann", "web", "ben", `{"toProjectId":"ops"}`}, {"ben", "web", "dan", `{"toProjectId":"ops"}`},
			{"dan", "web", "dan", `{"toProjectId":"ops"}`}, {"eve", "web", "dan", `{"toProjectId":"ops"}`},
			{"hal", "web", "ivy", `{"toProjectId":"ops"}`}, {"", "web", "dan", `{"toProjectId":"ops"}`},
		} {
			zgMoveRaw(h, r.user, r.pid, r.uid, r.body)
		}
		if d := zgDiff(before, zgSnap(t, h)); d != "" {
			t.Errorf("state changed: %s", d)
		}
	})

	// ---------- VALID ----------
	for _, c := range []struct{ n, user, src, uid, body string }{
		{"EmptyBody", "ivy", "web", "dan", ``},
		{"EmptyObject", "ivy", "web", "dan", `{}`},
		{"NotJSON", "ivy", "web", "dan", `ops`},
		{"BrokenJSON", "ivy", "web", "dan", `{"toProjectId":`},
		{"EmptyDest", "ivy", "web", "dan", `{"toProjectId":""}`},
		{"NumericDest", "ivy", "web", "dan", `{"toProjectId":5}`},
		{"NullDest", "ivy", "web", "dan", `{"toProjectId":null}`},
		{"ArrayDest", "ivy", "web", "dan", `{"toProjectId":["ops"]}`},
		{"WrongKey", "ivy", "web", "dan", `{"projectId":"ops"}`},
		{"DestCaseVariant", "ivy", "web", "dan", `{"toProjectId":"OPS"}`},
		{"DestWithSpace", "ivy", "web", "dan", `{"toProjectId":"ops "}`},
		{"DestSlash", "ivy", "web", "dan", `{"toProjectId":"ops/"}`},
		{"DestTraversal", "ivy", "web", "dan", `{"toProjectId":"../ops"}`},
		{"DestPathStyle", "ivy", "web", "dan", `{"toProjectId":"ops/tasks"}`},
		{"TargetCaseVariant", "ivy", "web", "DAN", `{"toProjectId":"ops"}`},
		{"TargetOddChars", "ivy", "web", "dan%20", `{"toProjectId":"ops"}`},
		{"TargetDotDot", "ivy", "web", "%2e%2e", `{"toProjectId":"ops"}`},
	} {
		c := c
		t.Run("VALID_"+c.n, func(t *testing.T) { zgDeniedOn(t, newServer(), zgReq{c.user, c.src, c.uid, c.body}) })
	}
	t.Run("FUNC_JunkExtraFieldsIgnored", func(t *testing.T) {
		h := newServer()
		code, body := zgMoveRaw(h, "ivy", "web", "dan", `{"toProjectId":"ops","role":"manager","userId":"hal"}`)
		if code != 200 {
			t.Fatalf("-> %d body=%s", code, body)
		}
		sig := zgSig(h)
		if sig["ops/dan"] != "403/403" {
			t.Errorf("extra body fields changed the role: ops/dan probe %s, want 403/403 (viewer)", sig["ops/dan"])
		}
		if sig["ops/hal"] != "404/404" {
			t.Errorf("hal was added via extra field: %s", sig["ops/hal"])
		}
	})
	t.Run("VALID_WrongMethodsDoNotMove", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		for _, m := range []string{"GET", "PUT", "PATCH", "DELETE"} {
			if code, body := zgDo(h, "ivy", m, "/projects/web/members/dan/move", `{"toProjectId":"ops"}`); code >= 200 && code < 300 {
				t.Errorf("%s accepted: %d %s", m, code, body)
			}
		}
		if d := zgDiff(before, zgSnap(t, h)); d != "" {
			t.Errorf("state changed: %s", d)
		}
	})
	t.Run("VALID_TrailingSlashOrBareMember", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		for _, p := range []string{"/projects/web/members/move", "/projects/web/members//move", "/projects/web/members/dan/move/x"} {
			if code, body := zgDo(h, "ivy", "POST", p, `{"toProjectId":"ops"}`); code >= 200 && code < 300 {
				t.Errorf("POST %s accepted: %d %s", p, code, body)
			}
		}
		if d := zgDiff(before, zgSnap(t, h)); d != "" {
			t.Errorf("state changed: %s", d)
		}
	})

	// ---------- CONTRACT ----------
	for _, c := range []struct {
		n                  string
		user, src, uid, to string
		want               int
	}{
		{"SourceOnlyManagerAnn403", "ann", "web", "ben", "ops", 403},
		{"Contributor403", "ben", "web", "dan", "ops", 403},
		{"Viewer403", "dan", "web", "dan", "ops", 403},
		{"OrgAdmin403", "eve", "web", "dan", "ops", 403},
		{"ViewerAnnOpsSource403", "ann", "ops", "cat", "web", 403},
		{"NonMember404", "hal", "web", "dan", "ops", 404},
		{"CrossOrg404", "gus", "web", "dan", "ops", 404},
		{"NotMemberOfSource404", "ben", "ops", "cat", "web", 404},
		{"UnknownProject404", "ann", "nope", "dan", "ops", 404},
		{"Anonymous401", "", "web", "dan", "ops", 401},
		{"UnknownUser401", "zed", "web", "dan", "ops", 401},
		{"ArchivedDest409", "ann", "web", "dan", "legacy", 409},
		{"TargetNotMemberHal404", "ivy", "web", "hal", "ops", 404},
		{"TargetOrgAdminNotMember404", "ivy", "web", "eve", "ops", 404},
		{"TargetOtherOrg404", "ivy", "web", "gus", "ops", 404},
		{"TargetMemberElsewhere404", "ivy", "ops", "dan", "web", 404},
	} {
		c := c
		t.Run("CONTRACT_"+c.n, func(t *testing.T) {
			h := newServer()
			code, body := zgMove(h, c.user, c.src, c.uid, c.to)
			if code != c.want {
				t.Errorf("move as %q -> %d, want %d body=%s", c.user, code, c.want, body)
			}
		})
	}
	t.Run("CONTRACT_Success200", func(t *testing.T) {
		h := newServer()
		if code, body := zgMove(h, "ivy", "web", "dan", "ops"); code != 200 {
			t.Errorf("-> %d body=%s, want 200", code, body)
		}
	})
	t.Run("CONTRACT_BodyKeysExactly", func(t *testing.T) {
		h := newServer()
		_, body := zgMove(h, "ivy", "web", "dan", "ops")
		var m map[string]interface{}
		if json.Unmarshal([]byte(body), &m) != nil {
			t.Fatalf("not JSON: %s", body)
		}
		for _, k := range []string{"userId", "projectId", "role"} {
			if _, ok := m[k]; !ok {
				t.Errorf("missing key %s in %s", k, body)
			}
		}
	})
	t.Run("CONTRACT_ErrorShape", func(t *testing.T) {
		h := newServer()
		code, body := zgMove(h, "dan", "web", "dan", "ops")
		var e map[string]string
		if code != 403 || json.Unmarshal([]byte(body), &e) != nil || e["error"] == "" {
			t.Errorf("-> %d body=%s, want 403 {\"error\":...}", code, body)
		}
		code, body = zgMove(h, "ivy", "web", "hal", "ops")
		e = nil
		if code != 404 || json.Unmarshal([]byte(body), &e) != nil || e["error"] == "" {
			t.Errorf("-> %d body=%s, want 404 {\"error\":...}", code, body)
		}
	})
	t.Run("CONTRACT_ArchivedSourceNonMemberTarget", func(t *testing.T) {
		h := newServer()
		code, body := zgMove(h, "ann", "legacy", "hal", "web")
		if code != 404 && code != 409 {
			t.Errorf("-> %d body=%s, want 404 or 409", code, body)
		}
	})

	// ---------- INFO ----------
	t.Run("INFO_MoveToSameProject", func(t *testing.T) {
		h := newServer()
		code, body := zgMove(h, "ivy", "web", "dan", "web")
		t.Logf("INFO move dan web->web -> %d body=%s; probes web/dan=%s", code, body, zgSig(h)["web/dan"])
	})
	t.Run("INFO_MoveYourself", func(t *testing.T) {
		h := newServer()
		code, body := zgMove(h, "ivy", "web", "ivy", "ops")
		sig := zgSig(h)
		t.Logf("INFO ivy moves self web->ops (already ops manager) -> %d body=%s; web/ivy=%s ops/ivy=%s", code, body, sig["web/ivy"], sig["ops/ivy"])
	})
	t.Run("INFO_DestinationAlreadyMember", func(t *testing.T) {
		h := newServer()
		code, body := zgMove(h, "ivy", "web", "ann", "ops")
		sig := zgSig(h)
		t.Logf("INFO ivy moves ann (web mgr, ops viewer) web->ops -> %d body=%s; web/ann=%s ops/ann=%s", code, body, sig["web/ann"], sig["ops/ann"])
	})
	t.Run("INFO_DestinationAlreadyMemberCat", func(t *testing.T) {
		h := newServer()
		zgUnassign(t, h, "ops", "t4")
		code, body := zgMove(h, "ivy", "ops", "cat", "web")
		sig := zgSig(h)
		t.Logf("INFO ivy moves cat (contrib in both) ops->web -> %d body=%s; ops/cat=%s web/cat=%s", code, body, sig["ops/cat"], sig["web/cat"])
	})
	t.Run("INFO_MoveAssigneeOutcome", func(t *testing.T) {
		h := newServer()
		code, body := zgMove(h, "ivy", "web", "ben", "ops")
		_, t1 := zgDo(h, "eve", "GET", "/projects/web/tasks/t1", "")
		t.Logf("INFO move ben (assigned t1): -> %d body=%s; t1 now %s", code, body, strings.TrimSpace(t1))
	})
	t.Run("INFO_DestUnknownOrBadStatus", func(t *testing.T) {
		h := newServer()
		for _, b := range []string{`{"toProjectId":"nope"}`, `{}`, `{"toProjectId":5}`, `{"toProjectId":"gx"}`, `{"toProjectId":"legacy"}`} {
			code, body := zgMoveRaw(h, "ivy", "web", "dan", b)
			t.Logf("INFO ivy move dan %s -> %d %s", b, code, strings.TrimSpace(body))
		}
	})
	t.Run("INFO_ArchivedSourceCodes", func(t *testing.T) {
		h := newServer()
		code, body := zgMove(h, "ann", "legacy", "ben", "web")
		t.Logf("INFO ann moves ben legacy->web (ben already in web) -> %d body=%s", code, body)
	})
	t.Run("INFO_OrgAdminCanStillRead", func(t *testing.T) {
		h := newServer()
		zgMove(h, "ivy", "web", "dan", "ops")
		c, _ := zgDo(h, "eve", "GET", "/projects/ops/tasks", "")
		t.Logf("INFO eve reads ops after move -> %d", c)
	})
	t.Run("INFO_LastManagerMove", func(t *testing.T) {
		h := newServer()
		c1, _ := zgMove(h, "ivy", "web", "ann", "ops")
		code, body := zgMove(h, "ivy", "web", "ivy", "ops")
		t.Logf("INFO move ann (%d) then ivy (last web manager) web->ops -> %d body=%s", c1, code, body)
	})
}
