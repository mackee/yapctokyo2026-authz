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

// Hidden grader for task 016 (DELETE /projects/{pid}/members/{uid}). Uses only newServer() and HTTP.
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

func zgRm(h http.Handler, user, pid, uid string) (int, string) {
	return zgDo(h, user, "DELETE", "/projects/"+pid+"/members/"+uid, "")
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

// zgTasks reads every task through existing endpoints.
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


type zgReq struct{ user, pid, uid string }

// zgDenied: non-2xx, nothing changed (tasks and roles), no leak; want!=0 checks exact status.
func zgDenied(t *testing.T, r zgReq, want int) {
	t.Helper()
	h := newServer()
	before := zgSnap(t, h)
	code, resp := zgRm(h, r.user, r.pid, r.uid)
	after := zgSnap(t, h)
	if code >= 200 && code < 300 {
		t.Errorf("unexpected acceptance: DELETE %s/%s as %q -> %d body=%s", r.pid, r.uid, r.user, code, resp)
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

func zgRmOK(t *testing.T, h http.Handler, user, pid, uid string) {
	t.Helper()
	code, body := zgRm(h, user, pid, uid)
	if code != 204 {
		t.Fatalf("DELETE %s/%s as %s -> %d body=%s", pid, uid, user, code, body)
	}
}

// zgInvariant: in web/ops every assignee must currently be a contributor or manager there.
// (legacy is archived so the probe cannot see roles; gx is not probed.)
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

// zgAcceptable: the removal either is rejected with nothing changed, or succeeds, the user has
// lost access to pid, the invariant holds, and the ONLY collateral is unassigning uid's tasks in pid.
func zgAcceptable(t *testing.T, actor, pid, uid string, extra func(h http.Handler)) {
	t.Helper()
	h := newServer()
	if extra != nil {
		extra(h)
	}
	before := zgSnap(t, h)
	code, body := zgRm(h, actor, pid, uid)
	after := zgSnap(t, h)
	if code >= 200 && code < 300 {
		if code != 204 {
			t.Errorf("success status %d, want 204", code)
		}
		if after.sig[pid+"/"+uid] != "404/404" && pid != "gx" {
			t.Errorf("2xx but %s still has access to %s (probe %s)", uid, pid, after.sig[pid+"/"+uid])
		}
		if c, _ := zgDo(h, uid, "GET", "/projects/"+pid+"/tasks", ""); c != 404 {
			t.Errorf("2xx but %s GET %s/tasks -> %d, want 404", uid, pid, c)
		}
		zgInvariant(t, after)
		for id, b := range before.tasks {
			a := after.tasks[id]
			u := b
			if b.ProjectID == pid && b.Assignee == uid {
				u.Assignee = ""
			}
			if a != b && a != u {
				t.Errorf("collateral change: %s %+v -> %+v (only unassigning %s's tasks in %s is allowed)", id, b, a, uid, pid)
			}
		}
		for k, x := range before.sig {
			if k == pid+"/"+uid {
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
			"ops/ann": "403/403", "ops/cat": "403/400", "ops/ivy": "400/400", "ops/ben": "404/404",
			"legacy/ann": "409/409", "legacy/ben": "409/409", "legacy/cat": "404/404",
		}
		for k, v := range want {
			if sig[k] != v {
				t.Errorf("probe %s = %s, want %s", k, sig[k], v)
			}
		}
	})

	// ---------- FUNC (no assigned tasks involved, so both policy choices pass) ----------
	t.Run("FUNC_RemoveViewerDan_204", func(t *testing.T) {
		h := newServer()
		code, body := zgRm(h, "ivy", "web", "dan")
		if code != 204 {
			t.Fatalf("-> %d body=%s", code, body)
		}
		if strings.TrimSpace(body) != "" {
			t.Errorf("204 with body %q", body)
		}
	})
	t.Run("FUNC_RemovedViewerLosesAccess", func(t *testing.T) {
		h := newServer()
		zgMust(t, h, "dan", "GET", "/projects/web/tasks", "", 200)
		zgRmOK(t, h, "ivy", "web", "dan")
		zgMust(t, h, "dan", "GET", "/projects/web/tasks", "", 404)
		zgMust(t, h, "dan", "GET", "/projects/web/tasks/t1", "", 404)
	})
	t.Run("FUNC_RemoveByOtherManagerAnn", func(t *testing.T) {
		h := newServer()
		zgRmOK(t, h, "ann", "web", "dan")
		zgMust(t, h, "dan", "GET", "/projects/web/tasks", "", 404)
	})
	t.Run("FUNC_RemoveViewerDoesNotTouchOthers", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		zgRmOK(t, h, "ivy", "web", "dan")
		after := zgSnap(t, h)
		for id, b := range before.tasks {
			if after.tasks[id] != b {
				t.Errorf("task %s changed: %+v -> %+v", id, b, after.tasks[id])
			}
		}
		for k, x := range before.sig {
			if k != "web/dan" && after.sig[k] != x {
				t.Errorf("probe %s %s -> %s", k, x, after.sig[k])
			}
		}
		if after.sig["web/dan"] != "404/404" {
			t.Errorf("dan probe = %s, want 404/404", after.sig["web/dan"])
		}
		zgMust(t, h, "eve", "GET", "/projects/web/tasks", "", 200)
	})
	t.Run("FUNC_RemovedUserCannotWrite", func(t *testing.T) {
		h := newServer()
		zgMust(t, h, "ivy", "PATCH", "/projects/web/tasks/t1", `{"assignee":""}`, 200)
		zgRmOK(t, h, "ivy", "web", "ben")
		zgMust(t, h, "ben", "POST", "/projects/web/tasks", `{"title":"x"}`, 404)
		zgMust(t, h, "ben", "PATCH", "/projects/web/tasks/t1", `{"status":"done"}`, 404)
		zgMust(t, h, "ben", "GET", "/projects/web/tasks", "", 404)
	})
	t.Run("FUNC_RemovalIsPerProject", func(t *testing.T) {
		h := newServer()
		zgMust(t, h, "ivy", "PATCH", "/projects/web/tasks/t1", `{"assignee":""}`, 200)
		zgRmOK(t, h, "ivy", "web", "ben")
		zgMust(t, h, "ben", "GET", "/projects/legacy/tasks", "", 200)
		if s := zgSig(h)["legacy/ben"]; s != "409/409" {
			t.Errorf("ben lost legacy membership: probe %s", s)
		}
		if _, b := zgDo(h, "ann", "GET", "/projects/legacy/tasks/t5", ""); !strings.Contains(b, `"ben"`) {
			t.Errorf("legacy t5 assignee changed: %s", b)
		}
	})
	t.Run("FUNC_RemoveManagerAnnFromWeb", func(t *testing.T) {
		h := newServer()
		zgRmOK(t, h, "ivy", "web", "ann")
		zgMust(t, h, "ann", "GET", "/projects/web/tasks", "", 404)
		zgMust(t, h, "ann", "DELETE", "/projects/web/tasks/t3", "", 404)
		zgMust(t, h, "ann", "GET", "/projects/ops/tasks", "", 200)
		sig := zgSig(h)
		if sig["legacy/ann"] != "409/409" || sig["ops/ann"] != "403/403" || sig["web/ann"] != "404/404" {
			t.Errorf("ann probes web=%s ops=%s legacy=%s", sig["web/ann"], sig["ops/ann"], sig["legacy/ann"])
		}
	})
	t.Run("FUNC_RemoveManagerIvyByAnn", func(t *testing.T) {
		h := newServer()
		zgRmOK(t, h, "ann", "web", "ivy")
		zgMust(t, h, "ivy", "GET", "/projects/web/tasks", "", 404)
		zgMust(t, h, "ivy", "POST", "/projects/ops/tasks", `{"title":"x"}`, 200)
	})
	t.Run("FUNC_RemoveOpsViewerAnn", func(t *testing.T) {
		h := newServer()
		zgRmOK(t, h, "ivy", "ops", "ann")
		zgMust(t, h, "ann", "GET", "/projects/ops/tasks", "", 404)
		zgMust(t, h, "ann", "GET", "/projects/web/tasks", "", 200)
	})
	t.Run("FUNC_RemovedManagerCannotRemoveOthers", func(t *testing.T) {
		h := newServer()
		zgRmOK(t, h, "ann", "web", "ivy")
		zgMust(t, h, "ivy", "DELETE", "/projects/web/members/dan", "", 404)
		zgMust(t, h, "dan", "GET", "/projects/web/tasks", "", 200)
	})
	t.Run("FUNC_RemoveTwiceSecond404", func(t *testing.T) {
		h := newServer()
		zgRmOK(t, h, "ivy", "web", "dan")
		zgMust(t, h, "ivy", "DELETE", "/projects/web/members/dan", "", 404)
	})
	t.Run("FUNC_RemovedUserNotAssignable", func(t *testing.T) {
		h := newServer()
		zgRmOK(t, h, "ivy", "web", "ann")
		zgMust(t, h, "ivy", "PATCH", "/projects/web/tasks/t3", `{"assignee":"ann"}`, 400)
		zgMust(t, h, "ivy", "POST", "/projects/web/tasks", `{"title":"x","assignee":"ann"}`, 400)
	})
	t.Run("FUNC_RemovedAssigneeAfterUnassign_cat", func(t *testing.T) {
		h := newServer()
		zgMust(t, h, "ivy", "PATCH", "/projects/ops/tasks/t4", `{"assignee":""}`, 200)
		zgRmOK(t, h, "ivy", "ops", "cat")
		zgMust(t, h, "cat", "GET", "/projects/ops/tasks", "", 404)
		zgMust(t, h, "cat", "GET", "/projects/web/tasks", "", 200)
	})

	// ---------- ATOM: assignee invariant (assignee must be contributor/manager) ----------
	// Accept either rejection (non-2xx, nothing changed) or success with only that user's tasks
	// in that project unassigned. A removed user left as assignee is the failure.
	for _, c := range []struct{ n, actor, pid, uid string }{
		{"RemoveAssigneeBenFromWeb", "ivy", "web", "ben"},
		{"RemoveAssigneeCatFromOps", "ivy", "ops", "cat"},
		{"RemoveAssigneeCatFromWeb", "ann", "web", "cat"},
		{"RemoveAssigneeBenFromWebByAnn", "ann", "web", "ben"},
	} {
		c := c
		t.Run("ATOM_"+c.n, func(t *testing.T) { zgAcceptable(t, c.actor, c.pid, c.uid, nil) })
	}
	t.Run("ATOM_RemoveManagerAssignee_ivy", func(t *testing.T) {
		zgAcceptable(t, "ann", "web", "ivy", func(h http.Handler) {
			zgMust(t, h, "ann", "POST", "/projects/web/tasks", `{"title":"Ivy task","assignee":"ivy"}`, 200)
		})
	})
	t.Run("ATOM_RemoveManagerAssignee_annInWeb", func(t *testing.T) {
		zgAcceptable(t, "ivy", "web", "ann", func(h http.Handler) {
			zgMust(t, h, "ivy", "PATCH", "/projects/web/tasks/t3", `{"assignee":"ann"}`, 200)
		})
	})
	t.Run("ATOM_LegacyTaskOfBenUntouched", func(t *testing.T) {
		h := newServer()
		code, _ := zgRm(h, "ivy", "web", "ben")
		_, b := zgDo(h, "ann", "GET", "/projects/legacy/tasks/t5", "")
		var x zgTask
		json.Unmarshal([]byte(b), &x)
		if x.Assignee != "ben" {
			t.Errorf("DELETE web/ben -> %d but legacy t5 assignee = %q (must stay ben)", code, x.Assignee)
		}
		if c, _ := zgDo(h, "ben", "GET", "/projects/legacy/tasks", ""); c != 200 {
			t.Errorf("ben lost legacy read: %d", c)
		}
	})
	t.Run("ATOM_OtherProjectTaskOfCatUntouched", func(t *testing.T) {
		h := newServer()
		code, _ := zgRm(h, "ivy", "ops", "cat")
		_, b := zgDo(h, "eve", "GET", "/projects/web/tasks/t2", "")
		var x zgTask
		json.Unmarshal([]byte(b), &x)
		if x.Assignee != "cat" {
			t.Errorf("DELETE ops/cat -> %d but web t2 assignee = %q (must stay cat)", code, x.Assignee)
		}
		if c, _ := zgDo(h, "cat", "PATCH", "/projects/web/tasks/t2", `{"status":"done"}`); c != 200 {
			t.Errorf("cat can no longer work on her web task: %d", c)
		}
	})
	t.Run("ATOM_RemoveBothBenAndCat_NoStaleAssignees", func(t *testing.T) {
		h := newServer()
		zgRm(h, "ivy", "web", "ben")
		zgRm(h, "ivy", "web", "cat")
		zgInvariant(t, zgSnap(t, h))
	})
	t.Run("ATOM_RemoveThenRejectedStillConsistent", func(t *testing.T) {
		// after an attempt on ben, a follow-up reassign by a manager must still behave per the rules.
		h := newServer()
		code, _ := zgRm(h, "ivy", "web", "ben")
		c2, _ := zgDo(h, "ivy", "PATCH", "/projects/web/tasks/t1", `{"assignee":"ben"}`)
		if code >= 200 && code < 300 && c2 != 400 {
			t.Errorf("removed ben can be assigned again: PATCH -> %d", c2)
		}
		if code >= 300 && c2 != 200 {
			t.Errorf("rejected removal but ben (still member) cannot be assigned: PATCH -> %d", c2)
		}
		zgInvariant(t, zgSnap(t, h))
	})
	t.Run("ATOM_RemovedAssigneeCannotStillModifyTask", func(t *testing.T) {
		h := newServer()
		code, _ := zgRm(h, "ivy", "web", "ben")
		if code < 200 || code >= 300 {
			return
		}
		if c, b := zgDo(h, "ben", "PATCH", "/projects/web/tasks/t1", `{"status":"done"}`); c >= 200 && c < 300 {
			t.Errorf("unexpected acceptance: removed ben PATCH t1 -> %d %s", c, b)
		}
		_, tb := zgDo(h, "eve", "GET", "/projects/web/tasks/t1", "")
		if strings.Contains(tb, `"done"`) {
			t.Errorf("state changed by removed user: %s", tb)
		}
	})

	// ---------- AUTHZ ----------
	for _, c := range []struct {
		n string
		r zgReq
	}{
		{"ContributorRemovesOther_cat", zgReq{"cat", "web", "dan"}},
		{"ContributorRemovesSelf_ben", zgReq{"ben", "web", "ben"}},
		{"ContributorRemovesManager_ben", zgReq{"ben", "web", "ann"}},
		{"ContributorRemovesInOps_cat", zgReq{"cat", "ops", "ivy"}},
		{"ViewerRemovesSelf_dan", zgReq{"dan", "web", "dan"}},
		{"ViewerRemovesOther_dan", zgReq{"dan", "web", "ben"}},
		{"ViewerAnnInOpsRemovesOther", zgReq{"ann", "ops", "cat"}},
		{"ViewerAnnInOpsRemovesManager", zgReq{"ann", "ops", "ivy"}},
		{"ViewerAnnInOpsRemovesSelf", zgReq{"ann", "ops", "ann"}},
		{"OrgAdminRemoves_eve", zgReq{"eve", "web", "dan"}},
		{"OrgAdminRemovesManager_eve", zgReq{"eve", "web", "ann"}},
		{"OrgAdminOps_eve", zgReq{"eve", "ops", "cat"}},
	} {
		c := c
		t.Run("AUTHZ_"+c.n, func(t *testing.T) { zgDenied(t, c.r, 0) })
	}
	for _, c := range []struct {
		n string
		r zgReq
	}{
		{"NonMemberHal", zgReq{"hal", "web", "dan"}},
		{"NonMemberBenInOps", zgReq{"ben", "ops", "cat"}},
		{"NonMemberDanInOps", zgReq{"dan", "ops", "ivy"}},
		{"CrossOrgGusInWeb", zgReq{"gus", "web", "dan"}},
		{"CrossOrgGusInWebManager", zgReq{"gus", "web", "ann"}},
		{"CrossOrgAnnInGx", zgReq{"ann", "gx", "gus"}},
		{"CrossOrgIvyInGx", zgReq{"ivy", "gx", "gus"}},
		{"CrossOrgEveAdminInGx", zgReq{"eve", "gx", "gus"}},
		{"UnknownProject", zgReq{"ann", "nope", "dan"}},
	} {
		c := c
		t.Run("AUTHZ_"+c.n, func(t *testing.T) { zgDenied(t, c.r, 0) })
	}
	t.Run("AUTHZ_Anonymous", func(t *testing.T) { zgDenied(t, zgReq{"", "web", "dan"}, 0) })
	t.Run("AUTHZ_UnknownUser", func(t *testing.T) { zgDenied(t, zgReq{"zed", "web", "dan"}, 0) })
	t.Run("AUTHZ_ArchivedManagerRemovesContributor", func(t *testing.T) { zgDenied(t, zgReq{"ann", "legacy", "ben"}, 0) })
	t.Run("AUTHZ_ArchivedManagerRemovesSelf", func(t *testing.T) { zgDenied(t, zgReq{"ann", "legacy", "ann"}, 0) })
	t.Run("AUTHZ_ArchivedContributorRemovesManager", func(t *testing.T) { zgDenied(t, zgReq{"ben", "legacy", "ann"}, 0) })
	t.Run("AUTHZ_ArchivedContributorRemovesSelf", func(t *testing.T) { zgDenied(t, zgReq{"ben", "legacy", "ben"}, 0) })
	t.Run("AUTHZ_ManagerOfOtherProjectCannotRemove", func(t *testing.T) {
		zgDenied(t, zgReq{"ivy", "legacy", "ben"}, 0)
		zgDenied(t, zgReq{"ann", "ops", "cat"}, 0)
	})
	t.Run("AUTHZ_TargetNotMember_NoChange", func(t *testing.T) {
		for _, r := range []zgReq{
			{"ivy", "web", "hal"}, {"ivy", "web", "eve"}, {"ivy", "web", "gus"}, {"ivy", "web", "zed"},
			{"ivy", "ops", "dan"}, {"ivy", "ops", "ben"}, {"ann", "web", "gus"},
		} {
			zgDenied(t, r, 0)
		}
	})
	t.Run("AUTHZ_ForeignManagerCannotRemoveGus", func(t *testing.T) {
		h := newServer()
		for _, u := range []string{"ann", "ivy", "eve"} {
			if code, body := zgRm(h, u, "gx", "gus"); code >= 200 && code < 300 {
				t.Errorf("unexpected acceptance: %s removed gus from gx -> %d %s", u, code, body)
			}
		}
		if c, _ := zgDo(h, "gus", "GET", "/projects/gx/tasks", ""); c != 200 {
			t.Errorf("gus lost gx access: %d", c)
		}
	})
	t.Run("AUTHZ_DeniedNoLeakBodies", func(t *testing.T) {
		h := newServer()
		for _, r := range []zgReq{{"hal", "web", "dan"}, {"gus", "web", "dan"}, {"", "web", "dan"}, {"ann", "gx", "gus"}, {"ivy", "web", "gus"}} {
			_, body := zgRm(h, r.user, r.pid, r.uid)
			if l := zgLeaks(r.user, body); len(l) > 0 {
				t.Errorf("leak: %v in %s", l, body)
			}
		}
	})
	t.Run("AUTHZ_DeniedRemovalKeepsAccess", func(t *testing.T) {
		h := newServer()
		zgRm(h, "cat", "web", "dan")
		zgRm(h, "dan", "web", "dan")
		zgRm(h, "eve", "web", "dan")
		zgMust(t, h, "dan", "GET", "/projects/web/tasks", "", 200)
		zgRm(h, "hal", "web", "ivy")
		zgMust(t, h, "ivy", "GET", "/projects/web/tasks", "", 200)
	})

	// ---------- VALID ----------
	t.Run("VALID_TargetCaseVariant", func(t *testing.T) { zgDenied(t, zgReq{"ivy", "web", "DAN"}, 0) })
	t.Run("VALID_TargetEmptyOrSlash", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		for _, p := range []string{"/projects/web/members/", "/projects/web/members", "/projects/web/members/dan/"} {
			code, body := zgDo(h, "ivy", "DELETE", p, "")
			if code >= 200 && code < 300 {
				t.Errorf("unexpected acceptance: DELETE %s -> %d %s", p, code, body)
			}
		}
		if d := zgDiff(before, zgSnap(t, h)); d != "" {
			t.Errorf("state changed: %s", d)
		}
		zgMust(t, h, "dan", "GET", "/projects/web/tasks", "", 200)
	})
	t.Run("VALID_TargetWithOddChars", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		for _, u := range []string{"dan%20", "%2e%2e", "dan%2Fx", "ann,dan"} {
			if code, body := zgDo(h, "ivy", "DELETE", "/projects/web/members/"+u, ""); code >= 200 && code < 300 {
				t.Errorf("unexpected acceptance: target %q -> %d %s", u, code, body)
			}
		}
		if d := zgDiff(before, zgSnap(t, h)); d != "" {
			t.Errorf("state changed: %s", d)
		}
	})
	t.Run("VALID_DeleteWithJunkBodyStillWorks", func(t *testing.T) {
		h := newServer()
		code, body := zgDo(h, "ivy", "DELETE", "/projects/web/members/dan", `{"junk":true}`)
		if code != 204 {
			t.Errorf("-> %d body=%s", code, body)
		}
	})

	// ---------- CONTRACT ----------
	for _, c := range []struct {
		n    string
		r    zgReq
		want int
	}{
		{"Contributor403", zgReq{"ben", "web", "dan"}, 403},
		{"Viewer403", zgReq{"dan", "web", "dan"}, 403},
		{"OrgAdmin403", zgReq{"eve", "web", "dan"}, 403},
		{"ViewerAnnOps403", zgReq{"ann", "ops", "cat"}, 403},
		{"NonMember404", zgReq{"hal", "web", "dan"}, 404},
		{"CrossOrg404", zgReq{"gus", "web", "dan"}, 404},
		{"NotMemberOfThatProject404", zgReq{"ben", "ops", "cat"}, 404},
		{"UnknownProject404", zgReq{"ann", "nope", "dan"}, 404},
		{"Anonymous401", zgReq{"", "web", "dan"}, 401},
		{"UnknownUser401", zgReq{"zed", "web", "dan"}, 401},
		{"Archived409", zgReq{"ann", "legacy", "ben"}, 409},
		{"TargetNotMemberHal404", zgReq{"ivy", "web", "hal"}, 404},
		{"TargetOrgAdminNotMember404", zgReq{"ivy", "web", "eve"}, 404},
		{"TargetOtherOrg404", zgReq{"ivy", "web", "gus"}, 404},
		{"TargetUnknown404", zgReq{"ivy", "web", "zed"}, 404},
		{"TargetMemberElsewhere404", zgReq{"ivy", "ops", "dan"}, 404},
	} {
		c := c
		t.Run("CONTRACT_"+c.n, func(t *testing.T) {
			h := newServer()
			code, body := zgRm(h, c.r.user, c.r.pid, c.r.uid)
			if code != c.want {
				t.Errorf("DELETE as %q -> %d, want %d body=%s", c.r.user, code, c.want, body)
			}
		})
	}
	t.Run("CONTRACT_Success204", func(t *testing.T) {
		h := newServer()
		if code, body := zgRm(h, "ivy", "web", "dan"); code != 204 {
			t.Errorf("-> %d body=%s, want 204", code, body)
		}
	})
	t.Run("CONTRACT_ErrorShape", func(t *testing.T) {
		h := newServer()
		code, body := zgRm(h, "dan", "web", "dan")
		var e map[string]string
		if code != 403 || json.Unmarshal([]byte(body), &e) != nil || e["error"] == "" {
			t.Errorf("-> %d body=%s, want 403 {\"error\":...}", code, body)
		}
		code, body = zgRm(h, "ivy", "web", "hal")
		e = nil
		if code != 404 || json.Unmarshal([]byte(body), &e) != nil || e["error"] == "" {
			t.Errorf("-> %d body=%s, want 404 {\"error\":...}", code, body)
		}
	})
	t.Run("CONTRACT_ArchivedWithNonMemberTarget", func(t *testing.T) {
		h := newServer()
		code, body := zgRm(h, "ann", "legacy", "hal")
		if code != 404 && code != 409 {
			t.Errorf("-> %d body=%s, want 404 or 409", code, body)
		}
	})

	// ---------- INFO ----------
	t.Run("INFO_RemoveSelfManager", func(t *testing.T) {
		h := newServer()
		code, body := zgRm(h, "ivy", "web", "ivy")
		c2, _ := zgDo(h, "ivy", "GET", "/projects/web/tasks", "")
		t.Logf("INFO ivy removes self from web (ann also manager) -> %d body=%s; ivy GET web -> %d", code, body, c2)
	})
	t.Run("INFO_RemoveSelfContributor", func(t *testing.T) {
		h := newServer()
		code, body := zgRm(h, "ben", "web", "ben")
		t.Logf("INFO ben (contributor) removes self -> %d body=%s", code, body)
	})
	t.Run("INFO_RemoveLastManager", func(t *testing.T) {
		h := newServer()
		c1, _ := zgRm(h, "ivy", "web", "ann")
		code, body := zgRm(h, "ivy", "web", "ivy")
		c3, _ := zgDo(h, "dan", "GET", "/projects/web/tasks", "")
		t.Logf("INFO remove ann (%d) then last manager ivy self -> %d body=%s; web still readable by dan -> %d; probes %v", c1, code, body, c3, zgSig(h)["web/ivy"])
	})
	t.Run("INFO_RemoveLastManagerOtherManager", func(t *testing.T) {
		h := newServer()
		code, body := zgRm(h, "ann", "legacy", "ann")
		t.Logf("INFO archived legacy last-manager self removal -> %d body=%s", code, body)
	})
	t.Run("INFO_RemoveNonMemberOrgAdminEve", func(t *testing.T) {
		h := newServer()
		code, body := zgRm(h, "ivy", "web", "eve")
		t.Logf("INFO remove eve (org admin, not member) from web -> %d body=%s", code, body)
	})
	t.Run("INFO_OrgAdminCanStillReadAfter", func(t *testing.T) {
		h := newServer()
		zgRm(h, "ivy", "web", "dan")
		c, _ := zgDo(h, "eve", "GET", "/projects/web/tasks", "")
		t.Logf("INFO eve reads web after removal -> %d", c)
	})
	t.Run("INFO_RemoveAssigneeOutcome", func(t *testing.T) {
		h := newServer()
		code, body := zgRm(h, "ivy", "web", "ben")
		_, t1 := zgDo(h, "eve", "GET", "/projects/web/tasks/t1", "")
		t.Logf("INFO remove ben (assigned t1): DELETE -> %d body=%s; t1 now %s", code, body, strings.TrimSpace(t1))
	})
	t.Run("INFO_RemovedUserReaddViaOtherEndpoints", func(t *testing.T) {
		h := newServer()
		zgRm(h, "ivy", "web", "dan")
		for _, m := range []string{"GET", "PUT", "POST", "PATCH"} {
			code, _ := zgDo(h, "ivy", m, "/projects/web/members/dan", `{"role":"viewer"}`)
			t.Logf("INFO %s /projects/web/members/dan as ivy -> %d", m, code)
		}
	})
	t.Run("INFO_ArchivedContributorStatus", func(t *testing.T) {
		h := newServer()
		code, body := zgRm(h, "ben", "legacy", "ann")
		t.Logf("INFO ben (contributor) in archived legacy removing ann -> %d body=%s", code, body)
	})
}
