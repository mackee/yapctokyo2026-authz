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

// Hidden grader for task 022 (POST /projects/{pid}/freeze). Uses only newServer() and HTTP.
// Roles are observed through non-mutating probes on existing endpoints (see zgSig).

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

func zgFreeze(h http.Handler, user, pid string) (int, string) {
	return zgDo(h, user, "POST", "/projects/"+pid+"/freeze", "")
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

var zgMembers = map[string][]string{
	"web": {"ann", "ben", "cat", "dan", "ivy"},
	"ops": {"ann", "cat", "ivy"},
	"gx":  {"gus"},
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

// zgTasks reads every task through existing endpoints (as eve, who is never affected, and gus for gx).
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
//	PATCH <task> {"assignee":"zzz"}: 404 non-member, 403 viewer/org admin/contributor, 400 manager (409 archived manager)
//	POST  tasks  {}               : 404 non-member, 403 viewer/org admin, 400 contributor/manager (409 archived)
//
// A viewer (or org admin) is "403/403".
func zgSig(h http.Handler) map[string]string {
	out := map[string]string{}
	for _, p := range []struct{ pid, tid string }{{"web", "t3"}, {"ops", "t4"}, {"legacy", "t5"}} {
		for _, u := range []string{"ann", "ben", "cat", "dan", "eve", "hal", "ivy", "gus"} {
			c1, _ := zgDo(h, u, "PATCH", "/projects/"+p.pid+"/tasks/"+p.tid, `{"assignee":"zzz"}`)
			c2, _ := zgDo(h, u, "POST", "/projects/"+p.pid+"/tasks", `{"title":"probe","assignee":"zzz"}`)
			out[p.pid+"/"+u] = fmt.Sprintf("%d/%d", c1, c2)
		}
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

type zgReq struct{ user, pid string }

// zgDenied: non-2xx, nothing changed (tasks and roles), no leak; want!=0 checks exact status.
func zgDenied(t *testing.T, r zgReq, want int) {
	t.Helper()
	h := newServer()
	before := zgSnap(t, h)
	code, resp := zgFreeze(h, r.user, r.pid)
	after := zgSnap(t, h)
	if code >= 200 && code < 300 {
		t.Errorf("unexpected acceptance: POST %s/freeze as %q -> %d body=%s", r.pid, r.user, code, resp)
	}
	if d := zgDiff(before, after); d != "" {
		t.Errorf("state changed: %s", d)
	}
	if l := zgLeaks(r.user, resp); len(l) > 0 {
		t.Errorf("leak: response exposes titles %v: %s", l, resp)
	}
	if want != 0 && code != want {
		t.Errorf("status %d, want %d (body=%s)", code, want, resp)
	}
}

func zgMust(t *testing.T, h http.Handler, user, method, path, body string, want int) string {
	t.Helper()
	code, resp := zgDo(h, user, method, path, body)
	if code != want {
		t.Fatalf("%s %s as %s -> %d, want %d body=%s", method, path, user, code, want, resp)
	}
	return resp
}

func zgNon2xx(t *testing.T, h http.Handler, user, method, path, body string) {
	t.Helper()
	if code, resp := zgDo(h, user, method, path, body); code >= 200 && code < 300 {
		t.Errorf("unexpected acceptance: %s %s as %s -> %d body=%s", method, path, user, code, resp)
	}
}

type zgMember struct {
	UserID string `json:"userId"`
	Role   string `json:"role"`
}

// zgCheckMembers verifies body is a JSON array containing exactly the members of pid, all viewer.
func zgCheckMembers(t *testing.T, pid, body string) {
	t.Helper()
	var ms []zgMember
	if err := json.Unmarshal([]byte(body), &ms); err != nil {
		t.Errorf("response is not a JSON array of {userId,role}: %v body=%s", err, body)
		return
	}
	got := map[string]int{}
	for _, m := range ms {
		got[m.UserID]++
		if m.Role != "viewer" {
			t.Errorf("member %q has role %q in response, want viewer", m.UserID, m.Role)
		}
	}
	for _, u := range zgMembers[pid] {
		if got[u] != 1 {
			t.Errorf("member %q appears %d times in response, want 1 (body=%s)", u, got[u], body)
		}
	}
	if len(ms) != len(zgMembers[pid]) {
		t.Errorf("response has %d entries, want %d (body=%s)", len(ms), len(zgMembers[pid]), body)
	}
}

// zgUnassign clears the assignee of the given tasks of pid as actor (a manager), so FUNC tests do not
// depend on the policy chosen for assigned tasks.
func zgUnassign(t *testing.T, h http.Handler, actor, pid string, tids ...string) {
	t.Helper()
	for _, id := range tids {
		zgMust(t, h, actor, "PATCH", "/projects/"+pid+"/tasks/"+id, `{"assignee":""}`, 200)
	}
}

func zgUnassignWeb(t *testing.T, h http.Handler) { zgUnassign(t, h, "ivy", "web", "t1", "t2") }

func zgFreezeOK(t *testing.T, h http.Handler, user, pid string) string {
	t.Helper()
	code, body := zgFreeze(h, user, pid)
	if code != 200 {
		t.Fatalf("POST %s/freeze as %s -> %d body=%s", pid, user, code, body)
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

// zgAcceptableFreeze: freezing pid either is rejected (non-2xx) with nothing changed, or succeeds with 200,
// lists every member as viewer, every member of pid probes as viewer, the invariant holds, and the ONLY
// collateral is unassigning tasks of pid. Other projects are untouched.
func zgAcceptableFreeze(t *testing.T, actor, pid string, extra func(h http.Handler)) {
	t.Helper()
	h := newServer()
	if extra != nil {
		extra(h)
	}
	before := zgSnap(t, h)
	code, body := zgFreeze(h, actor, pid)
	after := zgSnap(t, h)
	if code >= 200 && code < 300 {
		if code != 200 {
			t.Errorf("success status %d, want 200", code)
		}
		zgCheckMembers(t, pid, body)
		if pid == "web" || pid == "ops" {
			for _, u := range zgMembers[pid] {
				if s := after.sig[pid+"/"+u]; s != "403/403" {
					t.Errorf("2xx but %s in %s probes %s, want 403/403 (viewer)", u, pid, s)
				}
			}
			zgInvariant(t, after)
		}
		for id, b := range before.tasks {
			a := after.tasks[id]
			u := b
			if b.ProjectID == pid {
				u.Assignee = ""
			}
			if a != b && a != u {
				t.Errorf("collateral change: %s %+v -> %+v (only unassigning tasks of %s is allowed)", id, b, a, pid)
			}
		}
		for k, x := range before.sig {
			if strings.HasPrefix(k, pid+"/") {
				continue
			}
			if y := after.sig[k]; x != y {
				t.Errorf("collateral role change: probe %s %s -> %s", k, x, y)
			}
		}
		if pid == "gx" {
			if a := after.tasks["t6"]; a.Assignee == "gus" {
				t.Errorf("invariant violated: gx t6 still assigned to gus after gus became viewer")
			}
		}
	} else if d := zgDiff(before, after); d != "" {
		t.Errorf("state changed despite %d: %s (body=%s)", code, d, body)
	}
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
		{"OpsContributorOwn", "cat", "PATCH", "/projects/ops/tasks/t4", `{"status":"done"}`, 200},
		{"GxManagerCreate", "gus", "POST", "/projects/gx/tasks", `{"title":"x"}`, 200},
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
			"web/ann": "400/400", "web/ben": "403/400", "web/cat": "403/400", "web/dan": "403/403", "web/eve": "403/403",
			"web/hal": "404/404", "web/ivy": "400/400", "web/gus": "404/404",
			"ops/ann": "403/403", "ops/cat": "403/400", "ops/ivy": "400/400", "ops/ben": "404/404",
			"legacy/ann": "409/409", "legacy/ben": "409/409", "legacy/cat": "404/404",
		}
		for k, v := range want {
			if sig[k] != v {
				t.Errorf("probe %s = %s, want %s", k, sig[k], v)
			}
		}
	})

	// ---------- FUNC (assigned tasks are unassigned first, so both policy choices pass) ----------
	t.Run("FUNC_FreezeWeb_200ListsAllViewers", func(t *testing.T) {
		h := newServer()
		zgUnassignWeb(t, h)
		body := zgFreezeOK(t, h, "ivy", "web")
		zgCheckMembers(t, "web", body)
	})
	t.Run("FUNC_FreezeWeb_ByOtherManagerAnn", func(t *testing.T) {
		h := newServer()
		zgUnassignWeb(t, h)
		body := zgFreezeOK(t, h, "ann", "web")
		zgCheckMembers(t, "web", body)
	})
	t.Run("FUNC_FreezeOps_ListsAllViewers", func(t *testing.T) {
		h := newServer()
		zgUnassign(t, h, "ivy", "ops", "t4")
		body := zgFreezeOK(t, h, "ivy", "ops")
		zgCheckMembers(t, "ops", body)
	})
	t.Run("FUNC_FreezeGx_ByGus", func(t *testing.T) {
		h := newServer()
		zgUnassign(t, h, "gus", "gx", "t6")
		body := zgFreezeOK(t, h, "gus", "gx")
		zgCheckMembers(t, "gx", body)
		zgNon2xx(t, h, "gus", "POST", "/projects/gx/tasks", `{"title":"x"}`)
		zgMust(t, h, "gus", "GET", "/projects/gx/tasks", "", 200)
		if s := zgSig(h)["web/ivy"]; s != "400/400" {
			t.Errorf("freezing gx changed web/ivy: %s", s)
		}
	})
	t.Run("FUNC_ManagersBecomeViewers_web", func(t *testing.T) {
		h := newServer()
		zgUnassignWeb(t, h)
		zgFreezeOK(t, h, "ivy", "web")
		sig := zgSig(h)
		for _, u := range []string{"ann", "ivy"} {
			if sig["web/"+u] != "403/403" {
				t.Errorf("manager %s in web probes %s after freeze, want 403/403 (viewer)", u, sig["web/"+u])
			}
		}
	})
	t.Run("FUNC_ContributorsAndViewersBecomeViewers_web", func(t *testing.T) {
		h := newServer()
		zgUnassignWeb(t, h)
		zgFreezeOK(t, h, "ivy", "web")
		sig := zgSig(h)
		for _, u := range []string{"ben", "cat", "dan"} {
			if sig["web/"+u] != "403/403" {
				t.Errorf("%s in web probes %s after freeze, want 403/403 (viewer)", u, sig["web/"+u])
			}
		}
	})
	t.Run("FUNC_NonMembersStayOut_web", func(t *testing.T) {
		h := newServer()
		zgUnassignWeb(t, h)
		zgFreezeOK(t, h, "ivy", "web")
		sig := zgSig(h)
		for _, u := range []string{"hal", "gus"} {
			if sig["web/"+u] != "404/404" {
				t.Errorf("non-member %s in web probes %s after freeze, want 404/404", u, sig["web/"+u])
			}
		}
		if sig["web/eve"] != "403/403" {
			t.Errorf("eve (org admin) probes %s, want 403/403", sig["web/eve"])
		}
	})
	t.Run("FUNC_NobodyCanWrite_web", func(t *testing.T) {
		h := newServer()
		zgUnassignWeb(t, h)
		zgFreezeOK(t, h, "ivy", "web")
		before := zgSnap(t, h)
		for _, u := range []string{"ann", "ben", "cat", "dan", "ivy", "eve"} {
			zgNon2xx(t, h, u, "POST", "/projects/web/tasks", `{"title":"new"}`)
			zgNon2xx(t, h, u, "POST", "/projects/web/tasks", `{"title":"new","assignee":"ben"}`)
			for _, tid := range []string{"t1", "t2", "t3"} {
				zgNon2xx(t, h, u, "PATCH", "/projects/web/tasks/"+tid, `{"title":"hacked"}`)
				zgNon2xx(t, h, u, "PATCH", "/projects/web/tasks/"+tid, `{"status":"done"}`)
				zgNon2xx(t, h, u, "PATCH", "/projects/web/tasks/"+tid, `{"assignee":"ivy"}`)
				zgNon2xx(t, h, u, "DELETE", "/projects/web/tasks/"+tid, "")
			}
		}
		if d := zgDiff(before, zgSnap(t, h)); d != "" {
			t.Errorf("state changed by writes into a frozen project: %s", d)
		}
	})
	t.Run("FUNC_ManagerCannotCreateOrDelete_web", func(t *testing.T) {
		h := newServer()
		zgUnassignWeb(t, h)
		zgFreezeOK(t, h, "ann", "web")
		zgMust(t, h, "ann", "POST", "/projects/web/tasks", `{"title":"after"}`, 403)
		zgMust(t, h, "ivy", "DELETE", "/projects/web/tasks/t3", "", 403)
		zgMust(t, h, "ivy", "PATCH", "/projects/web/tasks/t3", `{"status":"done"}`, 403)
		zgMust(t, h, "eve", "GET", "/projects/web/tasks/t3", "", 200)
	})
	t.Run("FUNC_ContributorCreateBlocked_web", func(t *testing.T) {
		h := newServer()
		zgUnassignWeb(t, h)
		zgFreezeOK(t, h, "ivy", "web")
		zgMust(t, h, "ben", "POST", "/projects/web/tasks", `{"title":"after"}`, 403)
		zgMust(t, h, "cat", "POST", "/projects/web/tasks", `{"title":"after"}`, 403)
		zgMust(t, h, "ben", "PATCH", "/projects/web/tasks/t1", `{"status":"done"}`, 403)
	})
	t.Run("FUNC_ReadsStillWork_web", func(t *testing.T) {
		h := newServer()
		zgUnassignWeb(t, h)
		zgFreezeOK(t, h, "ivy", "web")
		for _, u := range []string{"ann", "ben", "cat", "dan", "ivy", "eve"} {
			zgMust(t, h, u, "GET", "/projects/web/tasks", "", 200)
			zgMust(t, h, u, "GET", "/projects/web/tasks/t1", "", 200)
		}
		zgMust(t, h, "hal", "GET", "/projects/web/tasks", "", 404)
	})
	t.Run("FUNC_TasksPreserved_web", func(t *testing.T) {
		h := newServer()
		zgUnassignWeb(t, h)
		before := zgSnap(t, h)
		zgFreezeOK(t, h, "ivy", "web")
		after := zgSnap(t, h)
		for id, b := range before.tasks {
			if after.tasks[id] != b {
				t.Errorf("task %s changed by freeze: %+v -> %+v", id, b, after.tasks[id])
			}
		}
		if len(after.tasks) != len(before.tasks) {
			t.Errorf("task count %d -> %d", len(before.tasks), len(after.tasks))
		}
	})
	t.Run("FUNC_FreezeWebLeavesOpsAlone", func(t *testing.T) {
		h := newServer()
		zgUnassignWeb(t, h)
		before := zgSnap(t, h)
		zgFreezeOK(t, h, "ivy", "web")
		after := zgSnap(t, h)
		for k, x := range before.sig {
			if !strings.HasPrefix(k, "web/") && after.sig[k] != x {
				t.Errorf("role probe %s changed %s -> %s after freezing web", k, x, after.sig[k])
			}
		}
		zgMust(t, h, "cat", "PATCH", "/projects/ops/tasks/t4", `{"status":"doing"}`, 200)
		zgMust(t, h, "ivy", "POST", "/projects/ops/tasks", `{"title":"ops new","assignee":"cat"}`, 200)
		zgMust(t, h, "ivy", "PATCH", "/projects/ops/tasks/t4", `{"assignee":"ivy"}`, 200)
		zgMust(t, h, "ivy", "PATCH", "/projects/ops/tasks/t4", `{"assignee":"ben"}`, 400)
		zgMust(t, h, "ann", "POST", "/projects/ops/tasks", `{"title":"x"}`, 403)
	})
	t.Run("FUNC_FreezeWebLeavesLegacyAndGxAlone", func(t *testing.T) {
		h := newServer()
		zgUnassignWeb(t, h)
		zgFreezeOK(t, h, "ivy", "web")
		sig := zgSig(h)
		if sig["legacy/ann"] != "409/409" || sig["legacy/ben"] != "409/409" {
			t.Errorf("legacy roles changed: ann=%s ben=%s", sig["legacy/ann"], sig["legacy/ben"])
		}
		zgMust(t, h, "gus", "POST", "/projects/gx/tasks", `{"title":"still works"}`, 200)
		_, b := zgDo(h, "ann", "GET", "/projects/legacy/tasks/t5", "")
		if !strings.Contains(b, `"ben"`) {
			t.Errorf("legacy t5 assignee changed: %s", b)
		}
	})
	t.Run("FUNC_FreezeOpsLeavesWebAlone", func(t *testing.T) {
		h := newServer()
		zgUnassign(t, h, "ivy", "ops", "t4")
		before := zgSnap(t, h)
		zgFreezeOK(t, h, "ivy", "ops")
		after := zgSnap(t, h)
		for k, x := range before.sig {
			if !strings.HasPrefix(k, "ops/") && after.sig[k] != x {
				t.Errorf("role probe %s changed %s -> %s after freezing ops", k, x, after.sig[k])
			}
		}
		for _, id := range []string{"t1", "t2", "t3", "t5", "t6"} {
			if after.tasks[id] != before.tasks[id] {
				t.Errorf("task %s changed by freezing ops: %+v -> %+v", id, before.tasks[id], after.tasks[id])
			}
		}
		zgMust(t, h, "ben", "PATCH", "/projects/web/tasks/t1", `{"status":"doing"}`, 200)
		zgMust(t, h, "ivy", "POST", "/projects/web/tasks", `{"title":"web new","assignee":"cat"}`, 200)
		zgMust(t, h, "ivy", "POST", "/projects/ops/tasks", `{"title":"x"}`, 403)
	})
	t.Run("FUNC_FrozenMemberNotAssignable", func(t *testing.T) {
		h := newServer()
		zgUnassignWeb(t, h)
		zgFreezeOK(t, h, "ivy", "web")
		// nobody can write, and in particular nobody may (re)assign to a frozen member
		zgNon2xx(t, h, "ivy", "PATCH", "/projects/web/tasks/t3", `{"assignee":"ben"}`)
		zgNon2xx(t, h, "ann", "POST", "/projects/web/tasks", `{"title":"x","assignee":"ann"}`)
		if _, b := zgDo(h, "eve", "GET", "/projects/web/tasks/t3", ""); !strings.Contains(b, `"assignee":""`) {
			t.Errorf("t3 acquired an assignee in a frozen project: %s", b)
		}
	})
	t.Run("FUNC_AssignedFreezeIsHandled", func(t *testing.T) {
		// A manager freezing web (t1 ben, t2 cat assigned) must get either 200 or a clear rejection
		// (400/409/422); 404/405/5xx means the endpoint is missing or broken.
		h := newServer()
		code, body := zgFreeze(h, "ivy", "web")
		if code != 200 && code != 400 && code != 409 && code != 422 {
			t.Errorf("POST web/freeze as ivy with assigned tasks -> %d, want 200 or 400/409/422 (body=%s)", code, body)
		}
	})

	// ---------- ATOM: assignee invariant (assignee must be contributor/manager) ----------
	// Accept either rejection (non-2xx, nothing changed) or success with only that project's tasks
	// unassigned. A frozen (viewer) member left as assignee is the failure.
	t.Run("ATOM_FreezeWebWithAssignees", func(t *testing.T) { zgAcceptableFreeze(t, "ivy", "web", nil) })
	t.Run("ATOM_FreezeWebWithAssigneesByAnn", func(t *testing.T) { zgAcceptableFreeze(t, "ann", "web", nil) })
	t.Run("ATOM_FreezeOpsWithAssignee", func(t *testing.T) { zgAcceptableFreeze(t, "ivy", "ops", nil) })
	t.Run("ATOM_FreezeGxWithAssignee", func(t *testing.T) { zgAcceptableFreeze(t, "gus", "gx", nil) })
	t.Run("ATOM_FreezeWebPartiallyUnassigned", func(t *testing.T) {
		zgAcceptableFreeze(t, "ivy", "web", func(h http.Handler) { zgUnassign(t, h, "ivy", "web", "t1") })
	})
	t.Run("ATOM_FreezeWebManagerAssignee", func(t *testing.T) {
		zgAcceptableFreeze(t, "ann", "web", func(h http.Handler) {
			zgUnassignWeb(t, h)
			zgMust(t, h, "ann", "PATCH", "/projects/web/tasks/t3", `{"assignee":"ivy"}`, 200)
		})
	})
	t.Run("ATOM_FreezeWebManagerAssigneeAnn", func(t *testing.T) {
		zgAcceptableFreeze(t, "ivy", "web", func(h http.Handler) {
			zgUnassignWeb(t, h)
			zgMust(t, h, "ivy", "POST", "/projects/web/tasks", `{"title":"Ann task","assignee":"ann"}`, 200)
		})
	})
	t.Run("ATOM_FreezeWebManyNewAssignedTasks", func(t *testing.T) {
		zgAcceptableFreeze(t, "ivy", "web", func(h http.Handler) {
			for _, u := range []string{"ben", "cat", "ann", "ivy"} {
				zgMust(t, h, "ivy", "POST", "/projects/web/tasks", `{"title":"n","assignee":"`+u+`"}`, 200)
			}
		})
	})
	t.Run("ATOM_FreezeWebCatKeepsOpsTask", func(t *testing.T) {
		h := newServer()
		code, _ := zgFreeze(h, "ivy", "web")
		_, b := zgDo(h, "eve", "GET", "/projects/ops/tasks/t4", "")
		if !strings.Contains(b, `"cat"`) {
			t.Errorf("POST web/freeze -> %d but ops t4 assignee changed: %s", code, b)
		}
		if c, _ := zgDo(h, "cat", "PATCH", "/projects/ops/tasks/t4", `{"status":"done"}`); c != 200 {
			t.Errorf("cat can no longer work on her ops task: %d", c)
		}
	})
	t.Run("ATOM_FreezeOpsKeepsWebTasks", func(t *testing.T) {
		h := newServer()
		code, _ := zgFreeze(h, "ivy", "ops")
		for id, want := range map[string]string{"t1": "ben", "t2": "cat"} {
			_, b := zgDo(h, "eve", "GET", "/projects/web/tasks/"+id, "")
			if !strings.Contains(b, `"`+want+`"`) {
				t.Errorf("POST ops/freeze -> %d but web %s assignee changed: %s", code, id, b)
			}
		}
	})
	t.Run("ATOM_FreezeWebLegacyTaskUntouched", func(t *testing.T) {
		h := newServer()
		code, _ := zgFreeze(h, "ivy", "web")
		_, b := zgDo(h, "ann", "GET", "/projects/legacy/tasks/t5", "")
		if !strings.Contains(b, `"ben"`) {
			t.Errorf("POST web/freeze -> %d but legacy t5 assignee = %s (must stay ben)", code, b)
		}
		if c, _ := zgDo(h, "ben", "GET", "/projects/legacy/tasks", ""); c != 200 {
			t.Errorf("ben lost legacy read: %d", c)
		}
	})
	t.Run("ATOM_RejectedFreezeLeavesProjectWritable", func(t *testing.T) {
		h := newServer()
		code, _ := zgFreeze(h, "ivy", "web")
		if code >= 200 && code < 300 {
			return
		}
		zgMust(t, h, "ben", "PATCH", "/projects/web/tasks/t1", `{"status":"doing"}`, 200)
		zgMust(t, h, "ann", "POST", "/projects/web/tasks", `{"title":"x","assignee":"cat"}`, 200)
		zgMust(t, h, "ivy", "DELETE", "/projects/web/tasks/t3", "", 204)
	})
	t.Run("ATOM_FrozenAssigneesNeverViewers", func(t *testing.T) {
		h := newServer()
		zgFreeze(h, "ivy", "web")
		zgFreeze(h, "ivy", "ops")
		zgFreeze(h, "gus", "gx")
		zgInvariant(t, zgSnap(t, h))
	})
	t.Run("ATOM_AfterFreezeNoWritePathToAssign", func(t *testing.T) {
		h := newServer()
		code, _ := zgFreeze(h, "ivy", "web")
		if code < 200 || code >= 300 {
			return
		}
		// accepted: nobody may write to web any more, so the assignee set can no longer change
		before := zgSnap(t, h)
		for _, u := range []string{"ann", "ivy", "ben", "cat"} {
			zgDo(h, u, "PATCH", "/projects/web/tasks/t3", `{"assignee":"`+u+`"}`)
			zgDo(h, u, "POST", "/projects/web/tasks", `{"title":"x","assignee":"`+u+`"}`)
		}
		if d := zgDiff(before, zgSnap(t, h)); d != "" {
			t.Errorf("state changed in frozen project: %s", d)
		}
	})

	// ---------- AUTHZ ----------
	for _, c := range []struct {
		n string
		r zgReq
	}{
		{"ContributorBen_web", zgReq{"ben", "web"}},
		{"ContributorCat_web", zgReq{"cat", "web"}},
		{"ContributorCat_ops", zgReq{"cat", "ops"}},
		{"ViewerDan_web", zgReq{"dan", "web"}},
		{"ViewerAnnInOps", zgReq{"ann", "ops"}},
		{"OrgAdmin_eve_web", zgReq{"eve", "web"}},
		{"OrgAdmin_eve_ops", zgReq{"eve", "ops"}},
		{"OrgAdmin_eve_legacy", zgReq{"eve", "legacy"}},
	} {
		c := c
		t.Run("AUTHZ_"+c.n, func(t *testing.T) { zgDenied(t, c.r, 0) })
	}
	for _, c := range []struct {
		n string
		r zgReq
	}{
		{"NonMemberHal_web", zgReq{"hal", "web"}},
		{"NonMemberHal_ops", zgReq{"hal", "ops"}},
		{"NonMemberBen_ops", zgReq{"ben", "ops"}},
		{"NonMemberDan_ops", zgReq{"dan", "ops"}},
		{"CrossOrgGus_web", zgReq{"gus", "web"}},
		{"CrossOrgGus_ops", zgReq{"gus", "ops"}},
		{"CrossOrgAnn_gx", zgReq{"ann", "gx"}},
		{"CrossOrgIvy_gx", zgReq{"ivy", "gx"}},
		{"CrossOrgEveAdmin_gx", zgReq{"eve", "gx"}},
		{"UnknownProject", zgReq{"ann", "nope"}},
		{"ProjectIdCaseVariant", zgReq{"ivy", "WEB"}},
		{"ManagerOfOtherProjectOnly_ivyLegacy", zgReq{"ivy", "legacy"}},
		{"NonMemberCat_legacy", zgReq{"cat", "legacy"}},
	} {
		c := c
		t.Run("AUTHZ_"+c.n, func(t *testing.T) { zgDenied(t, c.r, 0) })
	}
	t.Run("AUTHZ_Anonymous_web", func(t *testing.T) { zgDenied(t, zgReq{"", "web"}, 0) })
	t.Run("AUTHZ_UnknownUser_web", func(t *testing.T) { zgDenied(t, zgReq{"zed", "web"}, 0) })
	t.Run("AUTHZ_Anonymous_gx", func(t *testing.T) { zgDenied(t, zgReq{"", "gx"}, 0) })
	t.Run("AUTHZ_ArchivedManagerAnn_legacy", func(t *testing.T) { zgDenied(t, zgReq{"ann", "legacy"}, 0) })
	t.Run("AUTHZ_ArchivedContributorBen_legacy", func(t *testing.T) { zgDenied(t, zgReq{"ben", "legacy"}, 0) })
	t.Run("AUTHZ_DeniedNoLeakBodies", func(t *testing.T) {
		h := newServer()
		for _, r := range []zgReq{{"hal", "web"}, {"gus", "web"}, {"", "web"}, {"ann", "gx"}, {"ivy", "gx"}, {"hal", "ops"}} {
			_, body := zgFreeze(h, r.user, r.pid)
			if l := zgLeaks(r.user, body); len(l) > 0 {
				t.Errorf("leak: %v in %s", l, body)
			}
		}
	})
	t.Run("AUTHZ_DeniedFreezeKeepsEveryoneWriting", func(t *testing.T) {
		h := newServer()
		for _, r := range []zgReq{{"ben", "web"}, {"cat", "web"}, {"dan", "web"}, {"eve", "web"}, {"hal", "web"}, {"gus", "web"}, {"ann", "ops"}, {"cat", "ops"}} {
			zgFreeze(h, r.user, r.pid)
		}
		zgMust(t, h, "ben", "PATCH", "/projects/web/tasks/t1", `{"status":"doing"}`, 200)
		zgMust(t, h, "cat", "PATCH", "/projects/web/tasks/t2", `{"status":"done"}`, 200)
		zgMust(t, h, "ann", "POST", "/projects/web/tasks", `{"title":"x"}`, 200)
		zgMust(t, h, "ivy", "POST", "/projects/ops/tasks", `{"title":"x"}`, 200)
		zgMust(t, h, "cat", "PATCH", "/projects/ops/tasks/t4", `{"status":"done"}`, 200)
	})
	t.Run("AUTHZ_ContributorCannotFreezeThenWrite", func(t *testing.T) {
		h := newServer()
		zgFreeze(h, "ben", "web")
		zgMust(t, h, "ivy", "DELETE", "/projects/web/tasks/t3", "", 204)
	})
	t.Run("AUTHZ_OrgAdminNotPromoted", func(t *testing.T) {
		h := newServer()
		zgFreeze(h, "eve", "web")
		if s := zgSig(h)["web/eve"]; s != "403/403" {
			t.Errorf("eve probes %s after attempting freeze, want 403/403", s)
		}
	})
	t.Run("AUTHZ_FrozenManagerCannotFreezeAgain", func(t *testing.T) {
		h := newServer()
		zgUnassignWeb(t, h)
		zgFreezeOK(t, h, "ivy", "web")
		// every member is a viewer now: nobody holds the manager role needed to freeze
		before := zgSnap(t, h)
		for _, u := range []string{"ann", "ben", "cat", "dan", "eve"} {
			if code, body := zgFreeze(h, u, "web"); code >= 200 && code < 300 {
				t.Errorf("unexpected acceptance: %s froze already frozen web -> %d %s", u, code, body)
			}
		}
		if d := zgDiff(before, zgSnap(t, h)); d != "" {
			t.Errorf("state changed: %s", d)
		}
	})

	// ---------- VALID ----------
	t.Run("VALID_WrongMethodsDoNotFreeze", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		for _, m := range []string{"GET", "PUT", "PATCH", "DELETE"} {
			if code, body := zgDo(h, "ivy", m, "/projects/web/freeze", ""); code >= 200 && code < 300 {
				t.Errorf("unexpected acceptance: %s /projects/web/freeze -> %d %s", m, code, body)
			}
		}
		if d := zgDiff(before, zgSnap(t, h)); d != "" {
			t.Errorf("state changed: %s", d)
		}
		if code, _ := zgFreeze(h, "ivy", "web"); code == 404 || code == 405 {
			t.Errorf("POST /projects/web/freeze -> %d (route missing)", code)
		}
	})
	t.Run("VALID_PathVariantsDoNotFreeze", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		for _, p := range []string{"/projects/web/freeze/", "/projects/web/freezes", "/projects//freeze", "/projects/web/tasks/freeze", "/projects/web/freeze/t1"} {
			if code, body := zgDo(h, "ivy", "POST", p, ""); code >= 200 && code < 300 {
				t.Errorf("unexpected acceptance: POST %s -> %d %s", p, code, body)
			}
		}
		if d := zgDiff(before, zgSnap(t, h)); d != "" {
			t.Errorf("state changed: %s", d)
		}
	})
	t.Run("VALID_JunkBodyStillWorks", func(t *testing.T) {
		h := newServer()
		zgUnassignWeb(t, h)
		code, body := zgDo(h, "ivy", "POST", "/projects/web/freeze", `{"junk":true}`)
		if code != 200 {
			t.Fatalf("-> %d body=%s", code, body)
		}
		zgCheckMembers(t, "web", body)
	})
	t.Run("VALID_BodyRoleIgnored", func(t *testing.T) {
		// a body must not let the caller pick another role or an empty member set
		h := newServer()
		zgUnassignWeb(t, h)
		code, body := zgDo(h, "ivy", "POST", "/projects/web/freeze", `{"role":"manager","members":[]}`)
		if code >= 200 && code < 300 {
			zgCheckMembers(t, "web", body)
		}
		if s := zgSig(h)["web/ann"]; code >= 200 && code < 300 && s != "403/403" {
			t.Errorf("ann probes %s, want 403/403", s)
		}
	})

	// ---------- CONTRACT ----------
	for _, c := range []struct {
		n    string
		r    zgReq
		want int
	}{
		{"Contributor403", zgReq{"ben", "web"}, 403},
		{"ContributorOps403", zgReq{"cat", "ops"}, 403},
		{"Viewer403", zgReq{"dan", "web"}, 403},
		{"ViewerAnnOps403", zgReq{"ann", "ops"}, 403},
		{"OrgAdmin403", zgReq{"eve", "web"}, 403},
		{"NonMember404", zgReq{"hal", "web"}, 404},
		{"CrossOrg404", zgReq{"gus", "web"}, 404},
		{"CrossOrgManagerOfOther404", zgReq{"ann", "gx"}, 404},
		{"NotMemberOfThatProject404", zgReq{"ben", "ops"}, 404},
		{"UnknownProject404", zgReq{"ann", "nope"}, 404},
		{"Anonymous401", zgReq{"", "web"}, 401},
		{"UnknownUser401", zgReq{"zed", "web"}, 401},
		{"AnonymousUnknownProject401", zgReq{"", "nope"}, 401},
		{"Archived409", zgReq{"ann", "legacy"}, 409},
		{"ArchivedNonMember404", zgReq{"cat", "legacy"}, 404},
	} {
		c := c
		t.Run("CONTRACT_"+c.n, func(t *testing.T) {
			h := newServer()
			code, body := zgFreeze(h, c.r.user, c.r.pid)
			if code != c.want {
				t.Errorf("POST freeze as %q on %s -> %d, want %d body=%s", c.r.user, c.r.pid, code, c.want, body)
			}
		})
	}
	t.Run("CONTRACT_ArchivedContributor403or409", func(t *testing.T) {
		h := newServer()
		code, body := zgFreeze(h, "ben", "legacy")
		if code != 403 && code != 409 {
			t.Errorf("-> %d body=%s, want 403 or 409", code, body)
		}
	})
	t.Run("CONTRACT_ArchivedOrgAdmin403or409", func(t *testing.T) {
		h := newServer()
		code, body := zgFreeze(h, "eve", "legacy")
		if code != 403 && code != 409 {
			t.Errorf("-> %d body=%s, want 403 or 409", code, body)
		}
	})
	t.Run("CONTRACT_Success200JSONShape", func(t *testing.T) {
		h := newServer()
		zgUnassignWeb(t, h)
		req := httptest.NewRequest("POST", "/projects/web/freeze", nil)
		req.Header.Set("X-User", "ivy")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("-> %d body=%s, want 200", rec.Code, rec.Body.String())
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Errorf("Content-Type %q, want application/json", ct)
		}
		var raw []map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
			t.Fatalf("not a JSON array of objects: %v body=%s", err, rec.Body.String())
		}
		for _, o := range raw {
			if len(o) != 2 || o["userId"] == nil || o["role"] == nil {
				t.Errorf("entry %v, want exactly {userId, role}", o)
			}
		}
		zgCheckMembers(t, "web", rec.Body.String())
	})
	t.Run("CONTRACT_ErrorShape", func(t *testing.T) {
		h := newServer()
		code, body := zgFreeze(h, "dan", "web")
		var e map[string]string
		if code != 403 || json.Unmarshal([]byte(body), &e) != nil || e["error"] == "" {
			t.Errorf("-> %d body=%s, want 403 {\"error\":...}", code, body)
		}
		code, body = zgFreeze(h, "hal", "web")
		e = nil
		if code != 404 || json.Unmarshal([]byte(body), &e) != nil || e["error"] == "" {
			t.Errorf("-> %d body=%s, want 404 {\"error\":...}", code, body)
		}
	})

	// ---------- INFO ----------
	t.Run("INFO_AssignedPolicy_web", func(t *testing.T) {
		h := newServer()
		code, body := zgFreeze(h, "ivy", "web")
		_, ts := zgDo(h, "eve", "GET", "/projects/web/tasks", "")
		t.Logf("INFO ivy freezes web with assigned tasks -> %d body=%s; tasks now %s", code, strings.TrimSpace(body), strings.TrimSpace(ts))
	})
	t.Run("INFO_AssignedPolicy_ops", func(t *testing.T) {
		h := newServer()
		code, body := zgFreeze(h, "ivy", "ops")
		_, ts := zgDo(h, "eve", "GET", "/projects/ops/tasks", "")
		t.Logf("INFO ivy freezes ops (t4 cat) -> %d body=%s; tasks now %s", code, strings.TrimSpace(body), strings.TrimSpace(ts))
	})
	t.Run("INFO_FreezeTwice", func(t *testing.T) {
		h := newServer()
		zgUnassignWeb(t, h)
		c1, _ := zgFreeze(h, "ivy", "web")
		c2, b2 := zgFreeze(h, "ivy", "web")
		t.Logf("INFO freeze web twice by ivy -> %d then %d body=%s", c1, c2, strings.TrimSpace(b2))
	})
	t.Run("INFO_ResponseOrder", func(t *testing.T) {
		h := newServer()
		zgUnassignWeb(t, h)
		_, b := zgFreeze(h, "ivy", "web")
		var ms []zgMember
		json.Unmarshal([]byte(b), &ms)
		var ids []string
		for _, m := range ms {
			ids = append(ids, m.UserID)
		}
		t.Logf("INFO response order: %v (sorted=%v)", ids, sort.StringsAreSorted(ids))
	})
	t.Run("INFO_FreezeOpsCountsAlreadyViewer", func(t *testing.T) {
		h := newServer()
		zgUnassign(t, h, "ivy", "ops", "t4")
		_, b := zgFreeze(h, "ivy", "ops")
		t.Logf("INFO ops freeze includes already-viewer ann: %s", strings.TrimSpace(b))
	})
	t.Run("INFO_OnlyViewersTryFreezeAfter", func(t *testing.T) {
		h := newServer()
		zgUnassignWeb(t, h)
		zgFreeze(h, "ivy", "web")
		c, _ := zgFreeze(h, "ann", "web")
		t.Logf("INFO former manager ann freezes frozen web -> %d", c)
	})
	t.Run("INFO_AssignedRejectionCode", func(t *testing.T) {
		h := newServer()
		code, body := zgFreeze(h, "ann", "web")
		t.Logf("INFO ann freezes web with assigned tasks -> %d body=%s", code, strings.TrimSpace(body))
	})
}
