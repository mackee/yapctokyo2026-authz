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

// Hidden grader for task 034 (POST /orgs/{orgID}/users/{uid}/out-of-office {"delegate":"x"}).
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

func zgBody(d string) string {
	b, _ := json.Marshal(map[string]any{"delegate": d})
	return string(b)
}

func zgOoo(h http.Handler, user, org, uid, body string) (int, string) {
	return zgDo(h, user, "POST", "/orgs/"+org+"/users/"+uid+"/out-of-office", body)
}

func zg2xx(c int) bool { return c >= 200 && c < 300 }

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

var zgUsers = []string{"ann", "ben", "cat", "dan", "eve", "hal", "ivy", "gus"}

// zgSig observes every user's role per project without mutating anything:
//
//	PATCH <task> {"assignee":"zzz"}: 404 non-member, 403 viewer/org admin/contributor, 400 manager (not used on archived)
//	POST  tasks  {}               : 404 non-member, 403 viewer/org admin, 400 contributor/manager (409 if archived)
func zgSig(h http.Handler) map[string]string {
	out := map[string]string{}
	for _, p := range []struct{ pid, tid string }{{"web", "t3"}, {"ops", "t4"}, {"legacy", "t5"}, {"gx", "t6"}} {
		for _, u := range zgUsers {
			c1 := 0 // the archived project's PATCH probe depends on t5's assignee (contributor check precedes the 409), so only POST is used there
			if p.pid != "legacy" {
				c1, _ = zgDo(h, u, "PATCH", "/projects/"+p.pid+"/tasks/"+p.tid, `{"assignee":"zzz"}`)
			}
			c2, _ := zgDo(h, u, "POST", "/projects/"+p.pid+"/tasks", `{}`)
			out[p.pid+"/"+u] = fmt.Sprintf("%d/%d", c1, c2)
		}
	}
	return out
}

func zgWriter(s string) bool { return s == "400/400" || s == "403/400" || s == "0/409" }

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

// zgCheck validates the state after a request moving uid's tasks to delegate (any outcome):
//   - roles unchanged
//   - tasks not assigned to uid before are unchanged
//   - uid's tasks: assignee stays uid, becomes delegate (only if delegate writes that task's project), or is cleared
//   - no other field changes, no task appears/vanishes
//   - no task ends up assigned to a non-writer of its project
func zgCheck(before, after zgState, uid, delegate string) []string {
	var p []string
	for k, b := range before.sig {
		if a := after.sig[k]; a != b {
			p = append(p, fmt.Sprintf("role change: %s %s -> %s", k, b, a))
		}
	}
	for id, b := range before.tasks {
		a, ok := after.tasks[id]
		if !ok {
			p = append(p, id+" vanished")
			continue
		}
		if a.ID != b.ID || a.ProjectID != b.ProjectID || a.Title != b.Title || a.Status != b.Status {
			p = append(p, fmt.Sprintf("collateral task change: %s %+v -> %+v", id, b, a))
			continue
		}
		if b.Assignee != uid || b.ProjectID == "gx" {
			if a.Assignee != b.Assignee {
				p = append(p, fmt.Sprintf("collateral assignee change: %s %q -> %q (not assigned to %s)", id, b.Assignee, a.Assignee, uid))
			}
			continue
		}
		switch a.Assignee {
		case uid, "":
		case delegate:
			if s := after.sig[a.ProjectID+"/"+delegate]; !zgWriter(s) {
				p = append(p, fmt.Sprintf("invariant violated: task %s in %s reassigned to %s who is not a contributor/manager there (probe %s)", id, a.ProjectID, delegate, s))
			}
		default:
			p = append(p, fmt.Sprintf("task %s moved from %s to unrelated %q", id, uid, a.Assignee))
		}
	}
	for id := range after.tasks {
		if _, ok := before.tasks[id]; !ok {
			p = append(p, id+" appeared")
		}
	}
	for id, x := range after.tasks {
		if x.Assignee == "" {
			continue
		}
		if s := after.sig[x.ProjectID+"/"+x.Assignee]; !zgWriter(s) {
			p = append(p, fmt.Sprintf("invariant violated: task %s in %s has assignee %s who is not a contributor/manager (probe %s)", id, x.ProjectID, x.Assignee, s))
		}
	}
	sort.Strings(p)
	return p
}

type zgReq struct{ user, org, uid, body string }

func zgDenied(t *testing.T, r zgReq, want int) {
	t.Helper()
	h := newServer()
	before := zgSnap(t, h)
	code, resp := zgOoo(h, r.user, r.org, r.uid, r.body)
	after := zgSnap(t, h)
	if zg2xx(code) {
		t.Errorf("unexpected acceptance: POST /orgs/%s/users/%s/out-of-office %s as %q -> %d body=%s", r.org, r.uid, r.body, r.user, code, resp)
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

// zgMk creates a task through the existing endpoint and returns its id.
func zgMk(t *testing.T, h http.Handler, user, pid, title, as string) string {
	t.Helper()
	b, _ := json.Marshal(map[string]string{"title": title, "assignee": as})
	resp := zgMust(t, h, user, "POST", "/projects/"+pid+"/tasks", string(b), 201)
	var x zgTask
	if err := json.Unmarshal([]byte(resp), &x); err != nil || x.ID == "" {
		t.Fatalf("grader setup: bad create response %s", resp)
	}
	return x.ID
}

// setups: extra tasks for ivy (writes web+ops) so that fully valid delegations exist.
func zgIvyBoth(t *testing.T, h http.Handler) {
	zgMk(t, h, "ann", "web", "Ivy web A", "ivy")
	zgMk(t, h, "ivy", "web", "Ivy web C", "ivy")
	zgMk(t, h, "ivy", "ops", "Ivy ops B", "ivy")
}
func zgIvyWebOnly(t *testing.T, h http.Handler) {
	zgMk(t, h, "ann", "web", "Ivy web A", "ivy")
	zgMk(t, h, "ivy", "web", "Ivy web C", "ivy")
}

// zgOK performs the call as eve and requires a complete, valid delegation: 2xx, every task of uid
// now belongs to delegate, nothing else changed, response lists exactly those tasks.
func zgOK(t *testing.T, h http.Handler, uid, delegate string) {
	t.Helper()
	before := zgSnap(t, h)
	code, body := zgOoo(h, "eve", "acme", uid, zgBody(delegate))
	after := zgSnap(t, h)
	if !zg2xx(code) {
		t.Errorf("POST out-of-office %s -> %s as eve -> %d body=%s, want 2xx", uid, delegate, code, body)
		return
	}
	for _, p := range zgCheck(before, after, uid, delegate) {
		t.Errorf("%s", p)
	}
	want := map[string]bool{}
	for id, b := range before.tasks {
		if b.Assignee == uid && b.ProjectID != "gx" {
			want[id] = true
			if a := after.tasks[id]; a.Assignee != delegate {
				t.Errorf("task %s still assigned to %q, want %s", id, a.Assignee, delegate)
			}
		}
	}
	var resp []zgTask
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Errorf("response is not a JSON array of tasks: %v body=%s", err, body)
		return
	}
	got := map[string]bool{}
	for _, x := range resp {
		got[x.ID] = true
		if a, ok := after.tasks[x.ID]; ok && a != x {
			t.Errorf("response task %+v differs from stored %+v", x, a)
		}
	}
	for id := range want {
		if !got[id] {
			t.Errorf("response is missing reassigned task %s: %s", id, body)
		}
	}
	for id := range got {
		if !want[id] {
			t.Errorf("response contains task %s that was not assigned to %s", id, uid)
		}
	}
	for id, x := range after.tasks {
		if x.Assignee == uid && x.ProjectID != "gx" {
			t.Errorf("task %s still assigned to %s", id, uid)
		}
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
		{"ListAdminReads", "eve", "GET", "/projects/ops/tasks", "", 200},
		{"ListAnon401", "", "GET", "/projects/web/tasks", "", 401},
		{"GetTask", "dan", "GET", "/projects/web/tasks/t1", "", 200},
		{"CreateViewer403", "dan", "POST", "/projects/web/tasks", `{"title":"x"}`, 403},
		{"CreateArchived409", "ann", "POST", "/projects/legacy/tasks", `{"title":"x"}`, 409},
		{"CreateOK", "ann", "POST", "/projects/web/tasks", `{"title":"x","assignee":"ben"}`, 201},
		{"CreateViewerAssignee400", "ann", "POST", "/projects/web/tasks", `{"title":"x","assignee":"dan"}`, 400},
		{"PatchContributorOthers403", "ben", "PATCH", "/projects/web/tasks/t2", `{"status":"done"}`, 403},
		{"PatchContributorOwn", "ben", "PATCH", "/projects/web/tasks/t1", `{"status":"doing"}`, 200},
		{"PatchContributorReassign403", "ben", "PATCH", "/projects/web/tasks/t1", `{"assignee":"cat"}`, 403},
		{"PatchManagerReassignOK", "ivy", "PATCH", "/projects/web/tasks/t3", `{"assignee":"ben"}`, 200},
		{"PatchManagerReassignNonWriter400", "ivy", "PATCH", "/projects/web/tasks/t3", `{"assignee":"dan"}`, 400},
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
			"web/ann": "400/400", "web/ben": "403/400", "web/cat": "403/400", "web/dan": "403/403", "web/eve": "403/403",
			"web/hal": "404/404", "web/ivy": "400/400", "web/gus": "404/404",
			"ops/ann": "403/403", "ops/cat": "403/400", "ops/ivy": "400/400", "ops/ben": "404/404", "ops/dan": "404/404",
			"legacy/ann": "0/409", "legacy/ben": "0/409", "legacy/cat": "0/404", "legacy/ivy": "0/404", "legacy/dan": "0/404",
			"gx/gus": "400/400", "gx/ann": "404/404", "gx/eve": "404/404",
		}
		for k, v := range want {
			if sig[k] != v {
				t.Errorf("probe %s = %s, want %s", k, sig[k], v)
			}
		}
	})

	// ---------- FUNC (every affected task is valid for the delegate) ----------
	t.Run("FUNC_CatToIvyWebAndOps", func(t *testing.T) {
		h := newServer()
		zgOK(t, h, "cat", "ivy") // t2 (web), t4 (ops): ivy manages both
		tt := zgTasks(t, h)
		if tt["t2"].Assignee != "ivy" || tt["t4"].Assignee != "ivy" {
			t.Errorf("t2=%+v t4=%+v, want both assigned to ivy", tt["t2"], tt["t4"])
		}
		if tt["t2"].Status != "doing" {
			t.Errorf("t2 status changed to %q", tt["t2"].Status)
		}
	})
	t.Run("FUNC_IvyToCatThreeTasks", func(t *testing.T) {
		h := newServer()
		zgIvyBoth(t, h)
		zgOK(t, h, "ivy", "cat") // cat writes web and ops
	})
	t.Run("FUNC_IvyToBenWebOnly", func(t *testing.T) {
		h := newServer()
		zgIvyWebOnly(t, h)
		zgOK(t, h, "ivy", "ben")
	})
	t.Run("FUNC_IvyToAnnWebOnly", func(t *testing.T) {
		h := newServer()
		zgIvyWebOnly(t, h)
		zgOK(t, h, "ivy", "ann")
	})
	t.Run("FUNC_ResponseListsReassignedTasks", func(t *testing.T) {
		h := newServer()
		code, body := zgOoo(h, "eve", "acme", "cat", zgBody("ivy"))
		if !zg2xx(code) {
			t.Fatalf("-> %d body=%s", code, body)
		}
		var resp []zgTask
		if err := json.Unmarshal([]byte(body), &resp); err != nil {
			t.Fatalf("not a JSON array: %v body=%s", err, body)
		}
		ids := []string{}
		for _, x := range resp {
			ids = append(ids, x.ID)
			if x.Assignee != "ivy" {
				t.Errorf("response task %+v not assigned to ivy", x)
			}
			if s, ok := zgSeed[x.ID]; ok && (s.Title != x.Title || s.ProjectID != x.ProjectID) {
				t.Errorf("response task %+v does not match seed %+v", x, s)
			}
		}
		sort.Strings(ids)
		if strings.Join(ids, ",") != "t2,t4" {
			t.Errorf("response ids %v, want [t2 t4]", ids)
		}
	})
	t.Run("FUNC_DelegateCanWorkOnTask", func(t *testing.T) {
		h := newServer()
		id := zgMk(t, h, "ann", "web", "Ivy handover", "ivy")
		zgOK(t, h, "ivy", "cat")
		zgMust(t, h, "cat", "PATCH", "/projects/web/tasks/"+id, `{"status":"doing"}`, 200)
		// ivy is no longer the assignee; the contributor-only-own rule applies to ben, not ivy (manager) so just read
		zgMust(t, h, "ben", "PATCH", "/projects/web/tasks/"+id, `{"status":"done"}`, 403)
	})
	t.Run("FUNC_OriginalHasNoTasksLeft", func(t *testing.T) {
		h := newServer()
		zgIvyBoth(t, h)
		zgOK(t, h, "ivy", "cat")
		for id, x := range zgTasks(t, h) {
			if x.Assignee == "ivy" {
				t.Errorf("task %s still assigned to ivy", id)
			}
		}
	})
	t.Run("FUNC_ThereAndBack", func(t *testing.T) {
		h := newServer()
		zgOK(t, h, "cat", "ivy")
		zgOK(t, h, "ivy", "cat")
		tt := zgTasks(t, h)
		if tt["t2"].Assignee != "cat" || tt["t4"].Assignee != "cat" {
			t.Errorf("t2=%+v t4=%+v, want both back at cat", tt["t2"], tt["t4"])
		}
	})
	t.Run("FUNC_GlobexTaskAndOthersUntouched", func(t *testing.T) {
		h := newServer()
		zgOK(t, h, "cat", "ivy")
		tt := zgTasks(t, h)
		if tt["t6"] != zgSeed["t6"] || tt["t1"] != zgSeed["t1"] || tt["t5"] != zgSeed["t5"] || tt["t3"] != zgSeed["t3"] {
			t.Errorf("unrelated tasks changed: t1=%+v t3=%+v t5=%+v t6=%+v", tt["t1"], tt["t3"], tt["t5"], tt["t6"])
		}
	})

	// ---------- ATOM: delegate must write the task's project ----------
	// Accept rejection (non-2xx, nothing changed) or partial success where only valid tasks moved
	// and the rest were left alone/unassigned. A task assigned to a non-writer fails.
	atom := func(name string, setup func(t *testing.T, h http.Handler), calls ...[2]string) {
		t.Run("ATOM_"+name, func(t *testing.T) {
			h := newServer()
			if setup != nil {
				setup(t, h)
			}
			for _, c := range calls {
				before := zgSnap(t, h)
				code, body := zgOoo(h, "eve", "acme", c[0], zgBody(c[1]))
				after := zgSnap(t, h)
				if zg2xx(code) {
					for _, p := range zgCheck(before, after, c[0], c[1]) {
						t.Errorf("%s (%s -> %s, %d)", p, c[0], c[1], code)
					}
					var resp []zgTask
					if json.Unmarshal([]byte(body), &resp) == nil {
						for _, x := range resp {
							if a := after.tasks[x.ID]; a.Assignee != c[1] {
								t.Errorf("response claims %s reassigned to %s but stored assignee is %q", x.ID, c[1], a.Assignee)
							}
						}
					}
				} else if d := zgDiff(before, after); d != "" {
					t.Errorf("state changed despite %d (%s -> %s): %s body=%s", code, c[0], c[1], d, body)
				}
			}
		})
	}
	atom("BenToCat_cannotHoldArchivedLegacy", nil, [2]string{"ben", "cat"})
	atom("BenToIvy_cannotHoldArchivedLegacy", nil, [2]string{"ben", "ivy"})
	atom("BenToDan_viewer", nil, [2]string{"ben", "dan"})
	atom("BenToHal_noMemberships", nil, [2]string{"ben", "hal"})
	atom("BenToEve_orgAdminNoRole", nil, [2]string{"ben", "eve"})
	atom("BenToAnn_managerOfBoth", nil, [2]string{"ben", "ann"})
	atom("CatToDan_viewerNoOps", nil, [2]string{"cat", "dan"})
	atom("CatToAnn_opsViewer", nil, [2]string{"cat", "ann"})
	atom("CatToBen_notInOps", nil, [2]string{"cat", "ben"})
	atom("CatToHal", nil, [2]string{"cat", "hal"})
	atom("CatToEve", nil, [2]string{"cat", "eve"})
	atom("CatToIvy", nil, [2]string{"cat", "ivy"})
	atom("BenToCatThenCatToIvy", nil, [2]string{"ben", "cat"}, [2]string{"cat", "ivy"})
	atom("BenToCatThenCatToAnn", nil, [2]string{"ben", "cat"}, [2]string{"cat", "ann"})
	atom("IvyBothToBen_opsTaskNotAllowed", zgIvyBoth, [2]string{"ivy", "ben"})
	atom("IvyBothToAnn_opsViewer", zgIvyBoth, [2]string{"ivy", "ann"})
	atom("IvyBothToDan", zgIvyBoth, [2]string{"ivy", "dan"})
	atom("IvyWebOnlyToDan_viewer", zgIvyWebOnly, [2]string{"ivy", "dan"})
	atom("IvyWebOnlyToHal", zgIvyWebOnly, [2]string{"ivy", "hal"})
	atom("AnnFreshLegacyAndOps", func(t *testing.T, h http.Handler) {
		zgMk(t, h, "ann", "web", "Ann web", "ann")
	}, [2]string{"ann", "ben"}, [2]string{"ann", "cat"})
	t.Run("ATOM_RejectionLeavesMembershipsAndTasks", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		for _, c := range [][2]string{{"ben", "dan"}, {"cat", "hal"}, {"ben", "gus"}, {"cat", "gus"}} {
			code, _ := zgOoo(h, "eve", "acme", c[0], zgBody(c[1]))
			if !zg2xx(code) {
				if d := zgDiff(before, zgSnap(t, h)); d != "" {
					t.Errorf("%s -> %s rejected (%d) but state changed: %s", c[0], c[1], code, d)
				}
			} else {
				after := zgSnap(t, h)
				for _, p := range zgCheck(before, after, c[0], c[1]) {
					t.Errorf("%s (%s -> %s)", p, c[0], c[1])
				}
				before = after
			}
		}
	})

	// ---------- AUTHZ ----------
	cb := zgBody("ivy")
	for _, c := range []struct {
		n string
		r zgReq
	}{
		{"ManagerAnn", zgReq{"ann", "acme", "cat", cb}},
		{"ManagerIvy", zgReq{"ivy", "acme", "cat", cb}},
		{"ManagerAnnForBen", zgReq{"ann", "acme", "ben", zgBody("ann")}},
		{"ManagerIvySelf", zgReq{"ivy", "acme", "ivy", zgBody("cat")}},
		{"ManagerAnnSelf", zgReq{"ann", "acme", "ann", zgBody("ben")}},
		{"ContributorBenForCat", zgReq{"ben", "acme", "cat", cb}},
		{"ContributorCatSelf", zgReq{"cat", "acme", "cat", cb}},
		{"ContributorBenSelf", zgReq{"ben", "acme", "ben", zgBody("ann")}},
		{"ViewerDanForCat", zgReq{"dan", "acme", "cat", cb}},
		{"ViewerDanForBen", zgReq{"dan", "acme", "ben", zgBody("ann")}},
		{"NoMembershipHalForCat", zgReq{"hal", "acme", "cat", cb}},
		{"CrossOrgGusViaAcme", zgReq{"gus", "acme", "cat", cb}},
		{"CrossOrgGusViaAcmeBen", zgReq{"gus", "acme", "ben", zgBody("ann")}},
		{"CrossOrgGusViaGlobexCat", zgReq{"gus", "globex", "cat", cb}},
		{"CrossOrgGusViaGlobexSelf", zgReq{"gus", "globex", "gus", zgBody("ann")}},
		{"CrossOrgGusViaAcmeSelf", zgReq{"gus", "acme", "gus", zgBody("ann")}},
		{"CrossOrgEveViaGlobexCat", zgReq{"eve", "globex", "cat", cb}},
		{"CrossOrgEveViaGlobexGus", zgReq{"eve", "globex", "gus", zgBody("gus")}},
		{"CrossOrgEveViaGlobexBen", zgReq{"eve", "globex", "ben", zgBody("ann")}},
		{"CrossOrgEveUidGusViaAcme", zgReq{"eve", "acme", "gus", cb}},
		{"CrossOrgEveDelegateGusViaAcmeCat", zgReq{"eve", "acme", "cat", zgBody("gus")}},
		{"CrossOrgEveDelegateGusViaAcmeBen", zgReq{"eve", "acme", "ben", zgBody("gus")}},
		{"EveUnknownOrg", zgReq{"eve", "nope", "cat", cb}},
		{"EveUnknownUid", zgReq{"eve", "acme", "zed", cb}},
		{"EveUnknownDelegate", zgReq{"eve", "acme", "cat", zgBody("zed")}},
		{"EveDelegateCaseMismatch", zgReq{"eve", "acme", "cat", zgBody("IVY")}},
		{"EveUidCaseMismatch", zgReq{"eve", "acme", "CAT", cb}},
		{"EveOrgCaseMismatch", zgReq{"eve", "ACME", "cat", cb}},
		{"ManagerAnnWrongOrg", zgReq{"ann", "globex", "cat", cb}},
		{"Anonymous", zgReq{"", "acme", "cat", cb}},
		{"UnknownUser", zgReq{"zed", "acme", "cat", cb}},
		{"AnonymousUnknownOrg", zgReq{"", "nope", "cat", cb}},
	} {
		c := c
		t.Run("AUTHZ_"+c.n, func(t *testing.T) { zgDenied(t, c.r, 0) })
	}
	t.Run("AUTHZ_DeniedAttemptsDoNotAccumulate", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		for _, r := range []zgReq{{"ann", "acme", "cat", cb}, {"ivy", "acme", "ben", zgBody("ivy")}, {"ben", "acme", "ben", cb}, {"hal", "acme", "cat", cb}, {"gus", "acme", "cat", cb}, {"", "acme", "cat", cb}, {"eve", "globex", "cat", cb}, {"cat", "acme", "cat", cb}, {"dan", "acme", "cat", cb}} {
			if c, b := zgOoo(h, r.user, r.org, r.uid, r.body); zg2xx(c) {
				t.Errorf("unexpected acceptance: %+v -> %d %s", r, c, b)
			}
		}
		if d := zgDiff(before, zgSnap(t, h)); d != "" {
			t.Errorf("state changed: %s", d)
		}
	})
	t.Run("AUTHZ_DeniedNoLeakBodies", func(t *testing.T) {
		h := newServer()
		for _, r := range []zgReq{{"hal", "acme", "cat", cb}, {"gus", "acme", "cat", cb}, {"", "acme", "cat", cb}, {"ann", "acme", "cat", cb}, {"eve", "globex", "gus", zgBody("gus")}, {"gus", "globex", "cat", cb}, {"dan", "acme", "cat", cb}} {
			_, body := zgOoo(h, r.user, r.org, r.uid, r.body)
			if l := zgLeaks(r.user, body); len(l) > 0 {
				t.Errorf("leak: %v in %s", l, body)
			}
		}
	})
	t.Run("AUTHZ_GusKeepsGxTask", func(t *testing.T) {
		h := newServer()
		for _, r := range []zgReq{{"eve", "acme", "gus", zgBody("ann")}, {"eve", "globex", "gus", zgBody("gus")}, {"gus", "globex", "gus", zgBody("ann")}, {"gus", "acme", "gus", zgBody("ann")}, {"eve", "acme", "cat", zgBody("gus")}} {
			zgOoo(h, r.user, r.org, r.uid, r.body)
		}
		if x := zgTasks(t, h)["t6"]; x != zgSeed["t6"] {
			t.Errorf("t6 changed: %+v", x)
		}
		for _, id := range []string{"t2", "t4"} {
			if a := zgTasks(t, h)[id].Assignee; a == "gus" {
				t.Errorf("%s assigned to gus (other org)", id)
			}
		}
	})

	// ---------- VALID ----------
	for _, c := range []struct{ n, path, body string }{
		{"EmptyDelegate", "/orgs/acme/users/cat/out-of-office", `{"delegate":""}`},
		{"MissingDelegate", "/orgs/acme/users/cat/out-of-office", `{}`},
		{"EmptyBody", "/orgs/acme/users/cat/out-of-office", ``},
		{"MalformedJSON", "/orgs/acme/users/cat/out-of-office", `{"delegate":`},
		{"DelegateNotString", "/orgs/acme/users/cat/out-of-office", `{"delegate":42}`},
		{"DelegateArray", "/orgs/acme/users/cat/out-of-office", `{"delegate":["ivy"]}`},
		{"WrongFieldName", "/orgs/acme/users/cat/out-of-office", `{"to":"ivy"}`},
		{"WrongFieldNameAssignee", "/orgs/acme/users/cat/out-of-office", `{"assignee":"ivy"}`},
		{"SpaceDelegate", "/orgs/acme/users/cat/out-of-office", `{"delegate":" ivy"}`},
		{"DelegateUnknown", "/orgs/acme/users/cat/out-of-office", `{"delegate":"nobody"}`},
		{"MissingUID", "/orgs/acme/users//out-of-office", `{"delegate":"ivy"}`},
		{"EmptyOrg", "/orgs//users/cat/out-of-office", `{"delegate":"ivy"}`},
		{"ExtraSegment", "/orgs/acme/users/cat/out-of-office/x", `{"delegate":"ivy"}`},
		{"EncodedSlashUID", "/orgs/acme/users/cat%2Fben/out-of-office", `{"delegate":"ivy"}`},
		{"DotDotUID", "/orgs/acme/users/../out-of-office", `{"delegate":"ivy"}`},
		{"WrongSuffix", "/orgs/acme/users/cat/out_of_office", `{"delegate":"ivy"}`},
		{"UpperUID", "/orgs/acme/users/CAT/out-of-office", `{"delegate":"ivy"}`},
	} {
		c := c
		t.Run("VALID_"+c.n, func(t *testing.T) {
			h := newServer()
			before := zgSnap(t, h)
			code, body := zgDo(h, "eve", "POST", c.path, c.body)
			if zg2xx(code) {
				t.Errorf("unexpected acceptance: POST %s %s -> %d body=%s", c.path, c.body, code, body)
			}
			if d := zgDiff(before, zgSnap(t, h)); d != "" {
				t.Errorf("state changed: %s", d)
			}
		})
	}
	t.Run("VALID_OtherMethodsDoNothing", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		for _, m := range []string{"GET", "PUT", "PATCH", "DELETE"} {
			zgDo(h, "eve", m, "/orgs/acme/users/cat/out-of-office", zgBody("ivy"))
		}
		if d := zgDiff(before, zgSnap(t, h)); d != "" {
			t.Errorf("non-POST method changed state: %s", d)
		}
	})
	t.Run("FUNC_UnknownExtraFieldsIgnored", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		code, body := zgOoo(h, "eve", "acme", "cat", `{"delegate":"ivy","uid":"ben","org":"globex"}`)
		after := zgSnap(t, h)
		if !zg2xx(code) {
			t.Fatalf("-> %d body=%s", code, body)
		}
		for _, p := range zgCheck(before, after, "cat", "ivy") {
			t.Errorf("%s", p)
		}
		if a := after.tasks["t2"].Assignee; a != "ivy" {
			t.Errorf("t2 assignee %q, want ivy", a)
		}
	})

	// ---------- CONTRACT ----------
	for _, c := range []struct {
		n    string
		r    zgReq
		want int
	}{
		{"Manager403", zgReq{"ann", "acme", "cat", cb}, 403},
		{"ManagerIvy403", zgReq{"ivy", "acme", "cat", cb}, 403},
		{"Contributor403", zgReq{"ben", "acme", "cat", cb}, 403},
		{"ContributorSelf403", zgReq{"cat", "acme", "cat", cb}, 403},
		{"Viewer403", zgReq{"dan", "acme", "cat", cb}, 403},
		{"NoMembership403", zgReq{"hal", "acme", "cat", cb}, 403},
		{"AdminWrongOrg404", zgReq{"eve", "globex", "gus", zgBody("gus")}, 404},
		{"AdminWrongOrgAcmeUser404", zgReq{"eve", "globex", "cat", cb}, 404},
		{"AdminUnknownOrg404", zgReq{"eve", "nope", "cat", cb}, 404},
		{"AdminOtherOrgTarget404", zgReq{"eve", "acme", "gus", cb}, 404},
		{"AdminUnknownTarget404", zgReq{"eve", "acme", "zed", cb}, 404},
		{"Anonymous401", zgReq{"", "acme", "cat", cb}, 401},
		{"UnknownUser401", zgReq{"zed", "acme", "cat", cb}, 401},
	} {
		c := c
		t.Run("CONTRACT_"+c.n, func(t *testing.T) {
			h := newServer()
			code, body := zgOoo(h, c.r.user, c.r.org, c.r.uid, c.r.body)
			if code != c.want {
				t.Errorf("POST /orgs/%s/users/%s/out-of-office as %q -> %d, want %d body=%s", c.r.org, c.r.uid, c.r.user, code, c.want, body)
			}
		})
	}
	t.Run("CONTRACT_Success200", func(t *testing.T) {
		code, body := zgOoo(newServer(), "eve", "acme", "cat", cb)
		if code != 200 {
			t.Errorf("-> %d, want 200 body=%s", code, body)
		}
	})
	t.Run("CONTRACT_SuccessIsJSONArray", func(t *testing.T) {
		code, body := zgOoo(newServer(), "eve", "acme", "cat", cb)
		var resp []zgTask
		if code != 200 || json.Unmarshal([]byte(body), &resp) != nil || len(resp) != 2 {
			t.Errorf("-> %d body=%s, want 200 array of 2 tasks", code, body)
		}
	})
	t.Run("CONTRACT_ErrorShape", func(t *testing.T) {
		code, body := zgOoo(newServer(), "ann", "acme", "cat", cb)
		var e map[string]string
		if code != 403 || json.Unmarshal([]byte(body), &e) != nil || e["error"] == "" {
			t.Errorf("-> %d body=%s, want 403 {\"error\":...}", code, body)
		}
	})
	t.Run("CONTRACT_NotFoundErrorShape", func(t *testing.T) {
		code, body := zgOoo(newServer(), "eve", "acme", "gus", cb)
		var e map[string]string
		if code != 404 || json.Unmarshal([]byte(body), &e) != nil || e["error"] == "" {
			t.Errorf("-> %d body=%s, want 404 {\"error\":...}", code, body)
		}
	})
	t.Run("CONTRACT_NonWriterDelegateRejectionStatus", func(t *testing.T) {
		// only meaningful if the implementation rejects instead of partially applying
		for _, c := range [][2]string{{"cat", "dan"}, {"ben", "cat"}, {"cat", "hal"}} {
			code, body := zgOoo(newServer(), "eve", "acme", c[0], zgBody(c[1]))
			if zg2xx(code) {
				continue
			}
			if code != 400 && code != 409 && code != 422 {
				t.Errorf("%s -> %s rejected with %d (want 400, 409 or 422) body=%s", c[0], c[1], code, body)
			}
		}
	})
	t.Run("CONTRACT_BadBodyStatus", func(t *testing.T) {
		for _, b := range []string{`{}`, `{"delegate":""}`, `{"delegate":`} {
			code, body := zgOoo(newServer(), "eve", "acme", "cat", b)
			if code != 400 && code != 422 {
				t.Errorf("body %s -> %d, want 400 or 422 body=%s", b, code, body)
			}
		}
	})
	t.Run("CONTRACT_UnknownDelegateStatus", func(t *testing.T) {
		code, body := zgOoo(newServer(), "eve", "acme", "cat", zgBody("zed"))
		if code != 400 && code != 404 && code != 422 {
			t.Errorf("-> %d, want 400, 404 or 422 body=%s", code, body)
		}
	})

	// ---------- INFO ----------
	t.Run("INFO_BenToCat", func(t *testing.T) {
		h := newServer()
		code, body := zgOoo(h, "eve", "acme", "ben", zgBody("cat"))
		tt := zgTasks(t, h)
		t.Logf("INFO ben->cat (t1 web ok, t5 legacy: cat not a member): %d body=%q; t1=%+v t5=%+v", code, body, tt["t1"], tt["t5"])
	})
	t.Run("INFO_BenToAnn", func(t *testing.T) {
		h := newServer()
		code, body := zgOoo(h, "eve", "acme", "ben", zgBody("ann"))
		tt := zgTasks(t, h)
		t.Logf("INFO ben->ann (ann manages web and archived legacy): %d body=%q; t1=%+v t5=%+v", code, body, tt["t1"], tt["t5"])
		if tt["t5"].Assignee != "ben" {
			t.Logf("INFO archived legacy t5 changed (now %q)", tt["t5"].Assignee)
		}
	})
	t.Run("INFO_CatToDan", func(t *testing.T) {
		h := newServer()
		code, body := zgOoo(h, "eve", "acme", "cat", zgBody("dan"))
		tt := zgTasks(t, h)
		t.Logf("INFO cat->dan (viewer): %d body=%q; t2=%+v t4=%+v", code, body, tt["t2"], tt["t4"])
	})
	t.Run("INFO_UserWithoutTasks", func(t *testing.T) {
		for _, u := range []string{"dan", "hal", "ivy", "eve"} {
			h := newServer()
			before := zgSnap(t, h)
			code, body := zgOoo(h, "eve", "acme", u, zgBody("cat"))
			t.Logf("INFO %s (no tasks) -> cat: %d body=%q, state changed=%v", u, code, body, zgDiff(before, zgSnap(t, h)) != "")
		}
	})
	t.Run("INFO_DelegateIsSelf", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		code, body := zgOoo(h, "eve", "acme", "cat", zgBody("cat"))
		t.Logf("INFO cat->cat: %d body=%q, state changed=%q", code, body, zgDiff(before, zgSnap(t, h)))
	})
	t.Run("INFO_ResponseOrder", func(t *testing.T) {
		h := newServer()
		zgIvyBoth(t, h)
		_, body := zgOoo(h, "eve", "acme", "ivy", zgBody("cat"))
		var resp []zgTask
		_ = json.Unmarshal([]byte(body), &resp)
		ids := []string{}
		for _, x := range resp {
			ids = append(ids, x.ID)
		}
		t.Logf("INFO response order for ivy->cat: %v", ids)
	})
	t.Run("INFO_AdminDelegateToSelf", func(t *testing.T) {
		h := newServer()
		code, body := zgOoo(h, "eve", "acme", "cat", zgBody("eve"))
		tt := zgTasks(t, h)
		t.Logf("INFO cat->eve (org admin, no project roles): %d body=%q; t2=%+v t4=%+v", code, body, tt["t2"], tt["t4"])
	})
	t.Run("INFO_AdminAsUid", func(t *testing.T) {
		h := newServer()
		code, body := zgOoo(h, "eve", "acme", "eve", zgBody("cat"))
		t.Logf("INFO uid=eve (admin) -> cat: %d body=%q", code, body)
	})
	t.Run("INFO_SecondCall", func(t *testing.T) {
		h := newServer()
		c1, _ := zgOoo(h, "eve", "acme", "cat", zgBody("ivy"))
		c2, b2 := zgOoo(h, "eve", "acme", "cat", zgBody("ivy"))
		t.Logf("INFO cat->ivy twice: %d then %d body=%q", c1, c2, b2)
	})
}
