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

// Hidden grader for task 025 (PUT /projects/{pid}/members with {"roles":{uid:role,...}}).
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

// zgRoles builds {"roles":{k:v,...}} keeping the given order: zgRoles("ben","viewer","dan","contributor").
func zgRoles(kv ...string) string {
	var parts []string
	for i := 0; i+1 < len(kv); i += 2 {
		k, _ := json.Marshal(kv[i])
		v, _ := json.Marshal(kv[i+1])
		parts = append(parts, string(k)+":"+string(v))
	}
	return `{"roles":{` + strings.Join(parts, ",") + `}}`
}

func zgPutRaw(h http.Handler, user, pid, body string) (int, string) {
	return zgDo(h, user, "PUT", "/projects/"+pid+"/members", body)
}

func zgPutRoles(h http.Handler, user, pid string, kv ...string) (int, string) {
	return zgPutRaw(h, user, pid, zgRoles(kv...))
}

func zgPutOK(t *testing.T, h http.Handler, user, pid string, kv ...string) string {
	t.Helper()
	code, body := zgPutRoles(h, user, pid, kv...)
	if code != 200 {
		t.Fatalf("PUT %s %v as %s -> %d body=%s", pid, kv, user, code, body)
	}
	return body
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

var zgWebBase = map[string]string{"ann": "manager", "ben": "contributor", "cat": "contributor", "dan": "viewer", "ivy": "manager"}

// signature of each role under zgSig (web t3 / ops t4 probes; both behave the same).
var zgRoleSig = map[string]string{"manager": "400/400", "contributor": "403/400", "viewer": "403/403"}

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

// zgTasks reads every task through existing endpoints.
func zgTasks(t *testing.T, h http.Handler) map[string]zgTask {
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

// zgSig observes every user's role in web/ops/legacy without mutating anything:
//
//	PATCH <task> {"assignee":"zzz"}: 404 non-member, 403 viewer/org admin/contributor, 400 manager (409 if archived, 403 viewer)
//	POST  tasks  {}               : 404 non-member, 403 viewer/org admin, 400 contributor/manager (409 if archived)
func zgSig(h http.Handler) map[string]string {
	out := map[string]string{}
	for _, p := range []struct{ pid, tid string }{{"web", "t3"}, {"ops", "t4"}, {"legacy", "t5"}} {
		for _, u := range []string{"ann", "ben", "cat", "dan", "eve", "hal", "ivy", "gus"} {
			c1, _ := zgDo(h, u, "PATCH", "/projects/"+p.pid+"/tasks/"+p.tid, `{"assignee":"zzz"}`)
			c2, _ := zgDo(h, u, "POST", "/projects/"+p.pid+"/tasks", `{}`)
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

type zgReq struct{ user, pid, body string }

func zgOK(code int) bool { return code >= 200 && code < 300 }

// zgDenied: non-2xx, nothing changed (tasks and roles), no leak; want!=0 checks exact status.
func zgDenied(t *testing.T, r zgReq, want int) {
	t.Helper()
	h := newServer()
	before := zgSnap(t, h)
	code, resp := zgPutRaw(h, r.user, r.pid, r.body)
	after := zgSnap(t, h)
	if zgOK(code) {
		t.Errorf("unexpected acceptance: PUT %s %s as %q -> %d body=%s", r.pid, r.body, r.user, code, resp)
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

// zgMembers decodes a member-list response into userId -> role.
func zgMembers(body string) (map[string]string, error) {
	var l []map[string]any
	if err := json.Unmarshal([]byte(body), &l); err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, m := range l {
		u, _ := m["userId"].(string)
		r, _ := m["role"].(string)
		if u == "" || r == "" {
			return nil, fmt.Errorf("entry without userId/role: %v", m)
		}
		out[u] = r
	}
	return out, nil
}

func zgSameMap(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// zgInv checks the "assignee must be contributor/manager" invariant for a bulk change.
// Accepted outcomes: (a) non-2xx with NOTHING changed (including the other entries), or
// (b) 2xx where every entry took effect, no task in the project has a viewer/non-writer
// assignee, and the only task changes are unassigning users demoted to viewer in that project.
func zgInv(t *testing.T, actor, pid string, kv ...string) {
	t.Helper()
	h := newServer()
	before := zgSnap(t, h)
	code, body := zgPutRoles(h, actor, pid, kv...)
	after := zgSnap(t, h)
	if !zgOK(code) {
		if d := zgDiff(before, after); d != "" {
			t.Errorf("state changed despite %d (body=%s): %s", code, body, d)
		}
		return
	}
	entries := map[string]string{}
	for i := 0; i+1 < len(kv); i += 2 {
		entries[kv[i]] = kv[i+1]
	}
	for uid, role := range entries {
		if got := after.sig[pid+"/"+uid]; got != zgRoleSig[role] {
			t.Errorf("2xx but %s is not %s in %s (probe %s, want %s)", uid, role, pid, got, zgRoleSig[role])
		}
	}
	for id, b := range before.tasks {
		exp := b
		if b.ProjectID == pid && entries[b.Assignee] == "viewer" {
			exp.Assignee = ""
		}
		if a := after.tasks[id]; a != exp {
			t.Errorf("task %s: %+v -> %+v, want %+v (collateral or missing unassign)", id, b, a, exp)
		}
	}
	for _, x := range after.tasks {
		if x.ProjectID != pid || x.Assignee == "" {
			continue
		}
		if s := after.sig[pid+"/"+x.Assignee]; s != "400/400" && s != "403/400" {
			t.Errorf("invariant violated: task %s in %s still assigned to %s who is a non-writer (probe %s)", x.ID, pid, x.Assignee, s)
		}
	}
	// roles of users not named must not change anywhere
	for k, b := range before.sig {
		parts := strings.SplitN(k, "/", 2)
		if parts[0] == pid {
			if _, named := entries[parts[1]]; named {
				continue
			}
		}
		if after.sig[k] != b {
			t.Errorf("role-probe %s changed %s -> %s", k, b, after.sig[k])
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
		{"ListAnon401", "", "GET", "/projects/web/tasks", "", 401},
		{"GetTask", "dan", "GET", "/projects/web/tasks/t1", "", 200},
		{"CreateViewer403", "dan", "POST", "/projects/web/tasks", `{"title":"x"}`, 403},
		{"CreateArchived409", "ann", "POST", "/projects/legacy/tasks", `{"title":"x"}`, 409},
		{"CreateOK", "ann", "POST", "/projects/web/tasks", `{"title":"x","assignee":"ben"}`, 201},
		{"CreateViewerAssignee400", "ann", "POST", "/projects/web/tasks", `{"title":"x","assignee":"dan"}`, 400},
		{"PatchContributorOthers403", "ben", "PATCH", "/projects/web/tasks/t2", `{"status":"done"}`, 403},
		{"PatchContributorOwn", "ben", "PATCH", "/projects/web/tasks/t1", `{"status":"doing"}`, 200},
		{"PatchContributorReassign403", "ben", "PATCH", "/projects/web/tasks/t1", `{"assignee":"cat"}`, 403},
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
			"ops/ann": "403/403", "ops/cat": "403/400", "ops/ivy": "400/400", "ops/ben": "404/404",
			"legacy/ann": "409/409", "legacy/ben": "409/409", "legacy/cat": "404/404",
		}
		for k, v := range want {
			if sig[k] != v {
				t.Errorf("probe %s = %s, want %s", k, sig[k], v)
			}
		}
	})

	// ---------- FUNC ----------
	t.Run("FUNC_PromoteViewer_ResponseList", func(t *testing.T) {
		h := newServer()
		body := zgPutOK(t, h, "ivy", "web", "dan", "contributor")
		m, err := zgMembers(body)
		if err != nil {
			t.Fatalf("decode: %v body=%s", err, body)
		}
		if m["dan"] != "contributor" {
			t.Errorf("response dan=%q, want contributor (body=%s)", m["dan"], body)
		}
	})
	t.Run("FUNC_PromotedViewerCanCreate", func(t *testing.T) {
		h := newServer()
		zgMust(t, h, "dan", "POST", "/projects/web/tasks", `{"title":"x"}`, 403)
		zgPutOK(t, h, "ivy", "web", "dan", "contributor")
		zgMust(t, h, "dan", "POST", "/projects/web/tasks", `{"title":"x","assignee":"dan"}`, 201)
	})
	t.Run("FUNC_PromotedToManagerCanDelete", func(t *testing.T) {
		h := newServer()
		zgMust(t, h, "ben", "DELETE", "/projects/web/tasks/t3", "", 403)
		zgPutOK(t, h, "ann", "web", "ben", "manager")
		zgMust(t, h, "ben", "DELETE", "/projects/web/tasks/t3", "", 204)
	})
	t.Run("FUNC_PromoteOpsViewerAnn", func(t *testing.T) {
		h := newServer()
		zgMust(t, h, "ann", "POST", "/projects/ops/tasks", `{"title":"x"}`, 403)
		zgPutOK(t, h, "ivy", "ops", "ann", "contributor")
		zgMust(t, h, "ann", "POST", "/projects/ops/tasks", `{"title":"x"}`, 201)
	})
	t.Run("FUNC_DemoteManagerToViewer_NoTasks", func(t *testing.T) {
		h := newServer()
		zgPutOK(t, h, "ivy", "web", "ann", "viewer")
		zgMust(t, h, "ann", "POST", "/projects/web/tasks", `{"title":"x"}`, 403)
		zgMust(t, h, "ann", "GET", "/projects/web/tasks", "", 200)
		zgMust(t, h, "ann", "DELETE", "/projects/web/tasks/t3", "", 403)
	})
	t.Run("FUNC_DemoteManagerToContributor", func(t *testing.T) {
		h := newServer()
		zgPutOK(t, h, "ivy", "web", "ann", "contributor")
		zgMust(t, h, "ann", "DELETE", "/projects/web/tasks/t3", "", 403)
		zgMust(t, h, "ann", "POST", "/projects/web/tasks", `{"title":"x"}`, 201)
		zgMust(t, h, "ann", "PATCH", "/projects/web/tasks/t1", `{"status":"done"}`, 403)
	})
	t.Run("FUNC_MultipleEntriesAppliedTogether", func(t *testing.T) {
		h := newServer()
		body := zgPutOK(t, h, "ivy", "web", "dan", "contributor", "ann", "viewer", "cat", "manager")
		if m, err := zgMembers(body); err != nil {
			t.Errorf("decode: %v body=%s", err, body)
		} else if m["dan"] != "contributor" || m["ann"] != "viewer" || m["cat"] != "manager" {
			t.Errorf("response roles dan=%s ann=%s cat=%s (body=%s)", m["dan"], m["ann"], m["cat"], body)
		}
		sig := zgSig(h)
		if sig["web/dan"] != "403/400" || sig["web/ann"] != "403/403" || sig["web/cat"] != "400/400" || sig["web/ben"] != "403/400" || sig["web/ivy"] != "400/400" {
			t.Errorf("web roles after bulk: dan=%s ann=%s cat=%s ben=%s ivy=%s", sig["web/dan"], sig["web/ann"], sig["web/cat"], sig["web/ben"], sig["web/ivy"])
		}
	})
	t.Run("FUNC_MultipleEntriesTakeEffect_Behavior", func(t *testing.T) {
		h := newServer()
		zgPutOK(t, h, "ivy", "web", "dan", "contributor", "ben", "manager")
		zgMust(t, h, "dan", "POST", "/projects/web/tasks", `{"title":"x"}`, 201)
		zgMust(t, h, "ben", "DELETE", "/projects/web/tasks/t3", "", 204)
	})
	t.Run("FUNC_KeyOrderIrrelevant", func(t *testing.T) {
		for _, body := range []string{
			`{"roles":{"dan":"contributor","ann":"viewer"}}`,
			`{"roles":{"ann":"viewer","dan":"contributor"}}`,
		} {
			h := newServer()
			code, resp := zgPutRaw(h, "ivy", "web", body)
			if code != 200 {
				t.Errorf("%s -> %d %s", body, code, resp)
				continue
			}
			sig := zgSig(h)
			if sig["web/dan"] != "403/400" || sig["web/ann"] != "403/403" {
				t.Errorf("%s: dan=%s ann=%s", body, sig["web/dan"], sig["web/ann"])
			}
		}
	})
	t.Run("FUNC_NoopEntriesAllowed", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		zgPutOK(t, h, "ivy", "web", "ben", "contributor", "dan", "viewer")
		if d := zgDiff(before, zgSnap(t, h)); d != "" {
			t.Errorf("no-op role change altered state: %s", d)
		}
	})
	t.Run("FUNC_OtherProjectsUntouched", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		zgPutOK(t, h, "ivy", "web", "ann", "viewer", "cat", "manager", "dan", "contributor")
		after := zgSnap(t, h)
		for _, pid := range []string{"ops", "legacy"} {
			for k, v := range before.sig {
				if strings.HasPrefix(k, pid+"/") && after.sig[k] != v {
					t.Errorf("role change in web leaked into %s: %s %s -> %s", pid, k, v, after.sig[k])
				}
			}
		}
		for _, id := range []string{"t4", "t5", "t6"} {
			if before.tasks[id] != after.tasks[id] {
				t.Errorf("task %s in another project changed: %+v -> %+v", id, before.tasks[id], after.tasks[id])
			}
		}
	})
	t.Run("FUNC_OpsBulk", func(t *testing.T) {
		h := newServer()
		zgPutOK(t, h, "ivy", "ops", "ann", "contributor", "ivy", "manager")
		zgMust(t, h, "ann", "POST", "/projects/ops/tasks", `{"title":"x"}`, 201)
		if sig := zgSig(h); sig["web/ann"] != "400/400" {
			t.Errorf("ann's web role changed: %s", sig["web/ann"])
		}
	})
	t.Run("FUNC_ManagerAssigneeDemotedToContributorKeepsTask", func(t *testing.T) {
		h := newServer()
		zgMust(t, h, "ann", "POST", "/projects/web/tasks", `{"title":"Ivy task","assignee":"ivy"}`, 201)
		zgPutOK(t, h, "ann", "web", "ivy", "contributor")
		for _, x := range zgTasks(t, h) {
			if x.Title == "Ivy task" && x.Assignee != "ivy" {
				t.Errorf("assignee changed to %q although ivy is still a valid assignee (contributor)", x.Assignee)
			}
		}
	})
	t.Run("FUNC_PromotedAssigneeKeepsTask", func(t *testing.T) {
		h := newServer()
		before := zgTasks(t, h)
		zgPutOK(t, h, "ivy", "web", "ben", "manager", "cat", "manager")
		after := zgTasks(t, h)
		for id, b := range before {
			if after[id] != b {
				t.Errorf("task %s changed: %+v -> %+v", id, b, after[id])
			}
		}
	})
	t.Run("FUNC_PromotedManagerCanBulkChange", func(t *testing.T) {
		h := newServer()
		zgPutOK(t, h, "ivy", "web", "ben", "manager")
		zgPutOK(t, h, "ben", "web", "dan", "contributor")
		if sig := zgSig(h); sig["web/dan"] != "403/400" {
			t.Errorf("dan=%s", sig["web/dan"])
		}
	})
	t.Run("FUNC_DemotedManagerCannotBulkChange", func(t *testing.T) {
		h := newServer()
		zgPutOK(t, h, "ivy", "web", "ann", "viewer")
		code, body := zgPutRoles(h, "ann", "web", "dan", "manager")
		if zgOK(code) {
			t.Errorf("unexpected acceptance: demoted ann changed roles -> %d %s", code, body)
		}
		if sig := zgSig(h); sig["web/dan"] != "403/403" {
			t.Errorf("state changed: dan=%s", sig["web/dan"])
		}
	})
	t.Run("FUNC_ConsecutiveBulkChanges", func(t *testing.T) {
		h := newServer()
		zgPutOK(t, h, "ivy", "web", "dan", "contributor")
		zgPutOK(t, h, "ivy", "web", "dan", "manager", "ann", "contributor")
		zgPutOK(t, h, "dan", "web", "ann", "viewer")
		sig := zgSig(h)
		if sig["web/dan"] != "400/400" || sig["web/ann"] != "403/403" || sig["web/ben"] != "403/400" {
			t.Errorf("dan=%s ann=%s ben=%s", sig["web/dan"], sig["web/ann"], sig["web/ben"])
		}
	})

	// ---------- ATOM: assignee invariant + all-or-nothing ----------
	// Rejection with nothing changed (including the other entries) or unassigning the demoted
	// user's tasks in that project only are both accepted. A viewer left as assignee is the failure.
	t.Run("ATOM_DemoteAssigneeToViewer_benWeb", func(t *testing.T) { zgInv(t, "ivy", "web", "ben", "viewer") })
	t.Run("ATOM_DemoteAssigneeToViewer_benWebByAnn", func(t *testing.T) { zgInv(t, "ann", "web", "ben", "viewer") })
	t.Run("ATOM_DemoteAssigneeMixedWithPromote", func(t *testing.T) {
		zgInv(t, "ivy", "web", "ben", "viewer", "dan", "contributor")
	})
	t.Run("ATOM_DemoteAssigneeMixedWithPromote_PromoteFirstInOrder", func(t *testing.T) {
		zgInv(t, "ivy", "web", "ann", "manager", "ben", "viewer")
	})
	t.Run("ATOM_DemoteAssigneeToViewer_catOps", func(t *testing.T) { zgInv(t, "ivy", "ops", "cat", "viewer") })
	t.Run("ATOM_DemoteAssigneeInOpsMixed", func(t *testing.T) {
		zgInv(t, "ivy", "ops", "ann", "contributor", "cat", "viewer")
	})
	t.Run("ATOM_DemoteCatInWebKeepsOps", func(t *testing.T) { zgInv(t, "ivy", "web", "cat", "viewer") })
	t.Run("ATOM_DemoteTwoAssignees", func(t *testing.T) { zgInv(t, "ivy", "web", "ben", "viewer", "cat", "viewer") })
	t.Run("ATOM_DemoteTwoAssigneesAndOthers", func(t *testing.T) {
		zgInv(t, "ivy", "web", "ben", "viewer", "cat", "viewer", "dan", "contributor", "ann", "contributor")
	})
	t.Run("ATOM_DemoteNewManagerAssignee_ivy", func(t *testing.T) {
		h := newServer()
		zgMust(t, h, "ann", "POST", "/projects/web/tasks", `{"title":"Ivy task","assignee":"ivy"}`, 201)
		before := zgSnap(t, h)
		code, _ := zgPutRoles(h, "ann", "web", "ivy", "viewer", "dan", "contributor")
		after := zgSnap(t, h)
		if !zgOK(code) {
			if d := zgDiff(before, after); d != "" {
				t.Errorf("state changed despite %d: %s", code, d)
			}
			return
		}
		for _, x := range after.tasks {
			if x.ProjectID == "web" && x.Assignee == "ivy" {
				t.Errorf("invariant violated: ivy is viewer but still assignee of %s", x.ID)
			}
		}
		if after.sig["web/dan"] != "403/400" || after.sig["web/ivy"] != "403/403" {
			t.Errorf("2xx but roles not applied: dan=%s ivy=%s", after.sig["web/dan"], after.sig["web/ivy"])
		}
	})
	t.Run("ATOM_DemotedViewerStillReadsButCannotWrite", func(t *testing.T) {
		h := newServer()
		code, _ := zgPutRoles(h, "ivy", "web", "ben", "viewer", "dan", "contributor")
		if !zgOK(code) {
			return // rejected; covered by the other ATOM subtests
		}
		if c, b := zgDo(h, "ben", "PATCH", "/projects/web/tasks/t1", `{"status":"doing"}`); c != 403 {
			t.Errorf("demoted viewer ben PATCH t1 -> %d, want 403 body=%s", c, b)
		}
		zgMust(t, h, "ben", "GET", "/projects/web/tasks", "", 200)
	})
	// all-or-nothing across entries (order of entries chosen so that the bad one is first, last, or middle)
	for _, c := range []struct {
		n, body string
	}{
		{"InvalidRoleLast", `{"roles":{"ben":"manager","dan":"owner"}}`},
		{"InvalidRoleFirst", `{"roles":{"dan":"owner","ben":"manager"}}`},
		{"InvalidRoleMiddle", `{"roles":{"ann":"viewer","ben":"admin","dan":"contributor"}}`},
		{"EmptyRoleValueLast", `{"roles":{"dan":"contributor","ben":""}}`},
		{"NonMemberLast", `{"roles":{"dan":"contributor","hal":"viewer"}}`},
		{"NonMemberFirst", `{"roles":{"hal":"viewer","dan":"contributor"}}`},
		{"OtherOrgTarget", `{"roles":{"dan":"contributor","gus":"viewer"}}`},
		{"UnknownTarget", `{"roles":{"dan":"contributor","zed":"viewer"}}`},
		{"UnknownTargetFirst", `{"roles":{"aaa":"viewer","dan":"contributor"}}`},
		{"OrgAdminNonMemberTarget", `{"roles":{"dan":"contributor","eve":"viewer"}}`},
		{"NonStringRoleLast", `{"roles":{"dan":"contributor","ben":5}}`},
		{"NullRoleLast", `{"roles":{"dan":"contributor","ben":null}}`},
		{"BadEntryWithAssigneeDemotion", `{"roles":{"ben":"viewer","hal":"viewer"}}`},
	} {
		c := c
		t.Run("ATOM_AllOrNothing_"+c.n, func(t *testing.T) {
			h := newServer()
			before := zgSnap(t, h)
			code, resp := zgPutRaw(h, "ivy", "web", c.body)
			if zgOK(code) {
				t.Errorf("unexpected acceptance: %s -> %d %s", c.body, code, resp)
			}
			if d := zgDiff(before, zgSnap(t, h)); d != "" {
				t.Errorf("state changed (partial write): %s", d)
			}
		})
	}
	t.Run("ATOM_AllOrNothing_UnauthorizedMixedSelfPromote", func(t *testing.T) {
		zgDenied(t, zgReq{"ben", "web", zgRoles("ben", "manager", "dan", "contributor")}, 0)
	})

	// ---------- AUTHZ ----------
	valid := zgRoles("dan", "contributor")
	for _, c := range []struct {
		n string
		r zgReq
	}{
		{"ContributorPromotesOther_cat", zgReq{"cat", "web", valid}},
		{"ContributorSelfPromote_ben", zgReq{"ben", "web", zgRoles("ben", "manager")}},
		{"ContributorDemotesManager_ben", zgReq{"ben", "web", zgRoles("ann", "viewer")}},
		{"ContributorBulk_cat", zgReq{"cat", "ops", zgRoles("ivy", "viewer", "ann", "manager")}},
		{"ViewerSelfPromote_dan", zgReq{"dan", "web", zgRoles("dan", "manager")}},
		{"ViewerPromotesOther_dan", zgReq{"dan", "web", zgRoles("ben", "manager", "cat", "manager")}},
		{"ViewerAnnInOpsPromotesSelf", zgReq{"ann", "ops", zgRoles("ann", "manager")}},
		{"ViewerAnnInOpsDemotesOther", zgReq{"ann", "ops", zgRoles("cat", "viewer")}},
		{"OrgAdminPromotes_eve", zgReq{"eve", "web", valid}},
		{"OrgAdminDemotes_eve", zgReq{"eve", "web", zgRoles("ann", "viewer", "ivy", "viewer")}},
		{"OrgAdminOpsSelf", zgReq{"eve", "ops", zgRoles("ann", "manager")}},
	} {
		c := c
		t.Run("AUTHZ_"+c.n, func(t *testing.T) { zgDenied(t, c.r, 0) })
	}
	for _, c := range []struct {
		n string
		r zgReq
	}{
		{"NonMemberHal", zgReq{"hal", "web", valid}},
		{"NonMemberHalSelf", zgReq{"hal", "web", zgRoles("hal", "manager")}},
		{"NonMemberBenInOps", zgReq{"ben", "ops", zgRoles("cat", "viewer")}},
		{"NonMemberDanInOps", zgReq{"dan", "ops", zgRoles("dan", "manager")}},
		{"CrossOrgGusInWeb", zgReq{"gus", "web", zgRoles("dan", "manager")}},
		{"CrossOrgGusSelfInWeb", zgReq{"gus", "web", zgRoles("gus", "manager")}},
		{"CrossOrgAnnInGx", zgReq{"ann", "gx", zgRoles("gus", "viewer")}},
		{"CrossOrgIvyInGx", zgReq{"ivy", "gx", zgRoles("gus", "viewer")}},
		{"CrossOrgEveAdminInGx", zgReq{"eve", "gx", zgRoles("gus", "viewer")}},
		{"UnknownProject", zgReq{"ann", "nope", valid}},
		{"Anonymous", zgReq{"", "web", valid}},
		{"UnknownUser", zgReq{"zed", "web", valid}},
		{"ArchivedManager_demote", zgReq{"ann", "legacy", zgRoles("ben", "viewer")}},
		{"ArchivedManager_promote", zgReq{"ann", "legacy", zgRoles("ben", "manager")}},
		{"ArchivedManager_noop", zgReq{"ann", "legacy", zgRoles("ben", "contributor")}},
		{"ArchivedByContributor", zgReq{"ben", "legacy", zgRoles("ann", "viewer")}},
		{"ManagerOfOtherProject_ivyLegacy", zgReq{"ivy", "legacy", zgRoles("ann", "viewer")}},
		{"ManagerOfOtherProject_annOps", zgReq{"ann", "ops", zgRoles("cat", "manager")}},
	} {
		c := c
		t.Run("AUTHZ_"+c.n, func(t *testing.T) { zgDenied(t, c.r, 0) })
	}
	t.Run("AUTHZ_CrossOrgTargetNotAdded_gusToWeb", func(t *testing.T) {
		for _, role := range []string{"viewer", "contributor", "manager"} {
			h := newServer()
			before := zgSnap(t, h)
			code, body := zgPutRoles(h, "ivy", "web", "gus", role)
			if zgOK(code) {
				t.Errorf("unexpected acceptance: adding other-org gus to web as %s -> %d body=%s", role, code, body)
			}
			if c, _ := zgDo(h, "gus", "GET", "/projects/web/tasks", ""); c != 404 {
				t.Errorf("gus can now read web: %d (cross-org membership created)", c)
			}
			if c, b := zgDo(h, "gus", "GET", "/projects/web/tasks/t1", ""); c == 200 {
				t.Errorf("leak: gus reads web task: %s", b)
			}
			if d := zgDiff(before, zgSnap(t, h)); d != "" {
				t.Errorf("state changed: %s", d)
			}
		}
	})
	t.Run("AUTHZ_CrossOrgTargetNotAdded_annToGx", func(t *testing.T) {
		h := newServer()
		code, body := zgPutRoles(h, "gus", "gx", "ann", "manager")
		if zgOK(code) {
			t.Errorf("unexpected acceptance: adding other-org ann to gx -> %d body=%s", code, body)
		}
		if c, _ := zgDo(h, "ann", "GET", "/projects/gx/tasks", ""); c != 404 {
			t.Errorf("ann can now read gx: %d (cross-org membership created)", c)
		}
		if c, b := zgDo(h, "ann", "POST", "/projects/gx/tasks", `{"title":"pwn","assignee":"ann"}`); zgOK(c) {
			t.Errorf("ann can write in gx: %d %s", c, b)
		}
	})
	t.Run("AUTHZ_NonMemberTargetsNotAdded", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		for _, uid := range []string{"hal", "zed", "ANN", "nobody"} {
			code, body := zgPutRoles(h, "ivy", "web", uid, "manager")
			if zgOK(code) {
				t.Errorf("unexpected acceptance: non-member target %q -> %d body=%s", uid, code, body)
			}
			if c, b := zgDo(h, uid, "GET", "/projects/web/tasks", ""); c == 200 {
				t.Errorf("%q can read web after the request: %s", uid, b)
			}
		}
		if d := zgDiff(before, zgSnap(t, h)); d != "" {
			t.Errorf("state changed: %s", d)
		}
	})
	t.Run("AUTHZ_DeniedNoLeakBodies", func(t *testing.T) {
		h := newServer()
		for _, r := range []zgReq{{"hal", "web", valid}, {"gus", "web", valid}, {"", "web", valid}, {"ann", "gx", zgRoles("gus", "viewer")}} {
			_, body := zgPutRaw(h, r.user, r.pid, r.body)
			if l := zgLeaks(r.user, body); len(l) > 0 {
				t.Errorf("leak: %v in %s", l, body)
			}
		}
	})

	// ---------- VALID ----------
	for _, c := range []struct{ n, role string }{
		{"RoleOwner", "owner"}, {"RoleEmpty", ""}, {"RoleCapitalized", "Manager"}, {"RoleAdmin", "admin"},
		{"RoleOrgAdmin", "orgadmin"}, {"RoleSpace", " viewer"}, {"RoleNone", "none"},
	} {
		c := c
		t.Run("VALID_"+c.n, func(t *testing.T) {
			zgDenied(t, zgReq{"ivy", "web", zgRoles("dan", c.role)}, 0)
		})
	}
	t.Run("VALID_BadBodies", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		for _, body := range []string{
			`{}`, `{"roles":null}`, `{"roles":"viewer"}`, `{"roles":[]}`, `{"roles":["dan"]}`, `not json`, `[]`,
			`{"dan":"viewer"}`, `{"roles":{"dan":5}}`, `{"roles":{"dan":null}}`, `{"roles":{"dan":["viewer"]}}`,
			`{"role":"viewer"}`, `{"roles":{"dan":"viewer"}`,
		} {
			code, resp := zgPutRaw(h, "ivy", "web", body)
			if zgOK(code) {
				t.Errorf("unexpected acceptance: body %q -> %d %s", body, code, resp)
			}
		}
		if d := zgDiff(before, zgSnap(t, h)); d != "" {
			t.Errorf("state changed: %s", d)
		}
	})
	t.Run("VALID_NoBody", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		code, _ := zgPutRaw(h, "ivy", "web", "")
		if zgOK(code) {
			t.Errorf("unexpected acceptance: empty body -> %d", code)
		}
		if d := zgDiff(before, zgSnap(t, h)); d != "" {
			t.Errorf("state changed: %s", d)
		}
	})
	t.Run("VALID_InvalidRoleByUnauthorizedStillDenied", func(t *testing.T) {
		zgDenied(t, zgReq{"dan", "web", zgRoles("dan", "superuser")}, 0)
	})

	// ---------- CONTRACT ----------
	for _, c := range []struct {
		n    string
		r    zgReq
		want int
	}{
		{"Contributor403", zgReq{"ben", "web", valid}, 403},
		{"Viewer403", zgReq{"dan", "web", zgRoles("dan", "manager")}, 403},
		{"OrgAdmin403", zgReq{"eve", "web", valid}, 403},
		{"ViewerAnnOps403", zgReq{"ann", "ops", zgRoles("cat", "viewer")}, 403},
		{"NonMember404", zgReq{"hal", "web", valid}, 404},
		{"CrossOrg404", zgReq{"gus", "web", valid}, 404},
		{"NotMemberOfThatProject404", zgReq{"ben", "ops", zgRoles("cat", "viewer")}, 404},
		{"UnknownProject404", zgReq{"ann", "nope", valid}, 404},
		{"Anonymous401", zgReq{"", "web", valid}, 401},
		{"UnknownUser401", zgReq{"zed", "web", valid}, 401},
		{"Archived409", zgReq{"ann", "legacy", zgRoles("ben", "viewer")}, 409},
		{"InvalidRole400", zgReq{"ivy", "web", zgRoles("dan", "owner")}, 400},
		{"EmptyRole400", zgReq{"ivy", "web", zgRoles("dan", "")}, 400},
		{"InvalidRoleInMixed400", zgReq{"ivy", "web", zgRoles("ben", "manager", "dan", "owner")}, 400},
		{"MissingRolesKey400", zgReq{"ivy", "web", `{}`}, 400},
		{"MalformedJSON400", zgReq{"ivy", "web", `not json`}, 400},
	} {
		c := c
		t.Run("CONTRACT_"+c.n, func(t *testing.T) {
			h := newServer()
			code, body := zgPutRaw(h, c.r.user, c.r.pid, c.r.body)
			if code != c.want {
				t.Errorf("PUT as %q -> %d, want %d body=%s", c.r.user, code, c.want, body)
			}
		})
	}
	t.Run("CONTRACT_ErrorShape", func(t *testing.T) {
		h := newServer()
		code, body := zgPutRoles(h, "dan", "web", "dan", "manager")
		var e map[string]string
		if code != 403 || json.Unmarshal([]byte(body), &e) != nil || e["error"] == "" {
			t.Errorf("-> %d body=%s, want 403 {\"error\":...}", code, body)
		}
	})
	t.Run("CONTRACT_ContentType", func(t *testing.T) {
		h := newServer()
		req := httptest.NewRequest("PUT", "/projects/web/members", strings.NewReader(valid))
		req.Header.Set("X-User", "ivy")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("-> %d", rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Errorf("Content-Type = %q", ct)
		}
	})
	t.Run("CONTRACT_ResponseIsFullMemberList", func(t *testing.T) {
		h := newServer()
		body := zgPutOK(t, h, "ivy", "web", "dan", "contributor", "ann", "viewer")
		m, err := zgMembers(body)
		if err != nil {
			t.Fatalf("decode: %v body=%s", err, body)
		}
		want := map[string]string{}
		for k, v := range zgWebBase {
			want[k] = v
		}
		want["dan"], want["ann"] = "contributor", "viewer"
		if !zgSameMap(m, want) {
			t.Errorf("member list = %v, want %v (body=%s)", m, want, body)
		}
	})
	t.Run("CONTRACT_ResponseListOps", func(t *testing.T) {
		h := newServer()
		body := zgPutOK(t, h, "ivy", "ops", "ann", "contributor")
		m, err := zgMembers(body)
		if err != nil {
			t.Fatalf("decode: %v body=%s", err, body)
		}
		want := map[string]string{"ann": "contributor", "cat": "contributor", "ivy": "manager"}
		if !zgSameMap(m, want) {
			t.Errorf("member list = %v, want %v (body=%s)", m, want, body)
		}
	})
	t.Run("CONTRACT_NonMemberOrUnknownTarget_400or404", func(t *testing.T) {
		h := newServer()
		for _, uid := range []string{"hal", "gus", "zed"} {
			code, _ := zgPutRoles(h, "ivy", "web", uid, "contributor")
			if code != 400 && code != 404 {
				t.Errorf("target %s -> %d, want 400 or 404", uid, code)
			}
		}
	})

	// ---------- INFO ----------
	t.Run("INFO_EmptyRolesMap", func(t *testing.T) {
		h := newServer()
		code, body := zgPutRaw(h, "ivy", "web", `{"roles":{}}`)
		t.Logf("INFO empty roles map as manager -> %d body=%s", code, body)
		code, body = zgPutRaw(h, "dan", "web", `{"roles":{}}`)
		t.Logf("INFO empty roles map as viewer -> %d body=%s", code, body)
	})
	t.Run("INFO_SelfDemoteManager", func(t *testing.T) {
		h := newServer()
		code, body := zgPutRoles(h, "ivy", "web", "ivy", "viewer")
		t.Logf("INFO ivy self-demote web (ann is also manager) -> %d body=%s; sig ivy=%s", code, body, zgSig(h)["web/ivy"])
	})
	t.Run("INFO_DemoteLastManager", func(t *testing.T) {
		h := newServer()
		code, body := zgPutRoles(h, "ivy", "web", "ann", "viewer", "ivy", "viewer")
		t.Logf("INFO demote both managers of web in one request -> %d body=%s; sig ann=%s ivy=%s", code, body, zgSig(h)["web/ann"], zgSig(h)["web/ivy"])
	})
	t.Run("INFO_SelfDemoteAndPromoteOther", func(t *testing.T) {
		h := newServer()
		code, body := zgPutRoles(h, "ivy", "web", "ivy", "contributor", "dan", "manager")
		sig := zgSig(h)
		t.Logf("INFO ivy self-demote + promote dan -> %d body=%s; ivy=%s dan=%s", code, body, sig["web/ivy"], sig["web/dan"])
	})
	t.Run("INFO_AddOrgAdminEve", func(t *testing.T) {
		h := newServer()
		code, body := zgPutRoles(h, "ivy", "web", "eve", "viewer")
		t.Logf("INFO PUT web eve viewer -> %d body=%s", code, body)
	})
	t.Run("INFO_AddSameOrgNonMemberHal", func(t *testing.T) {
		h := newServer()
		code, body := zgPutRoles(h, "ivy", "web", "hal", "contributor")
		c2, _ := zgDo(h, "hal", "GET", "/projects/web/tasks", "")
		t.Logf("INFO PUT web hal contributor -> %d body=%s; hal GET web -> %d", code, body, c2)
	})
	t.Run("INFO_DemoteAssigneeOutcome", func(t *testing.T) {
		h := newServer()
		code, body := zgPutRoles(h, "ivy", "web", "ben", "viewer")
		_, t1 := zgDo(h, "ann", "GET", "/projects/web/tasks/t1", "")
		t.Logf("INFO demote ben (assigned t1): PUT -> %d body=%s; t1 now %s", code, body, strings.TrimSpace(t1))
	})
	t.Run("INFO_OtherMethods", func(t *testing.T) {
		h := newServer()
		for _, m := range []string{"GET", "POST", "PATCH", "DELETE"} {
			code, _ := zgDo(h, "ivy", m, "/projects/web/members", valid)
			t.Logf("INFO %s /projects/web/members as ivy -> %d", m, code)
		}
	})
}
