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

// Hidden grader for task 015 (PUT /projects/{pid}/members/{uid}). Uses only newServer() and HTTP.
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

func zgPut(h http.Handler, user, pid, uid, role string) (int, string) {
	return zgDo(h, user, "PUT", "/projects/"+pid+"/members/"+uid, `{"role":"`+role+`"}`)
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

type zgReq struct{ user, pid, uid, role string }

// zgDenied: non-2xx, nothing changed (tasks and roles), no leak; want!=0 checks exact status.
func zgDenied(t *testing.T, r zgReq, want int) {
	t.Helper()
	h := newServer()
	before := zgSnap(t, h)
	code, resp := zgPut(h, r.user, r.pid, r.uid, r.role)
	after := zgSnap(t, h)
	if code >= 200 && code < 300 {
		t.Errorf("unexpected acceptance: PUT %s/%s role=%s as %q -> %d body=%s", r.pid, r.uid, r.role, r.user, code, resp)
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

func zgPutOK(t *testing.T, h http.Handler, user, pid, uid, role string) {
	t.Helper()
	code, body := zgPut(h, user, pid, uid, role)
	if code != 200 {
		t.Fatalf("PUT %s/%s %s as %s -> %d body=%s", pid, uid, role, user, code, body)
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
		// grader's own role probe must see the seed roles (guards the probe itself).
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

	// ---------- FUNC ----------
	t.Run("FUNC_PromoteViewerToContributor_ResponseBody", func(t *testing.T) {
		h := newServer()
		code, body := zgPut(h, "ivy", "web", "dan", "contributor")
		if code != 200 {
			t.Fatalf("-> %d body=%s", code, body)
		}
		var r map[string]any
		if err := json.Unmarshal([]byte(body), &r); err != nil {
			t.Fatalf("decode: %v body=%s", err, body)
		}
		if r["userId"] != "dan" || r["role"] != "contributor" {
			t.Errorf("body = %s, want {userId:dan, role:contributor}", body)
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
	t.Run("FUNC_PromotedToManagerCanReassign", func(t *testing.T) {
		h := newServer()
		zgPutOK(t, h, "ann", "web", "cat", "manager")
		zgMust(t, h, "cat", "PATCH", "/projects/web/tasks/t1", `{"assignee":"cat"}`, 200)
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
	t.Run("FUNC_DemoteContributorToContributorNoop", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		zgPutOK(t, h, "ivy", "web", "ben", "contributor")
		if d := zgDiff(before, zgSnap(t, h)); d != "" {
			t.Errorf("no-op role change altered state: %s", d)
		}
	})
	t.Run("FUNC_ManagerOnlyInOneProject", func(t *testing.T) {
		h := newServer()
		// ann demoted in web stays manager in legacy and viewer in ops.
		zgPutOK(t, h, "ivy", "web", "ann", "viewer")
		zgMust(t, h, "ann", "PATCH", "/projects/legacy/tasks/t5", `{"status":"done"}`, 409)
		zgMust(t, h, "ann", "GET", "/projects/ops/tasks", "", 200)
		sig := zgSig(h)
		if sig["legacy/ann"] != "409/409" || sig["ops/ann"] != "403/403" {
			t.Errorf("role change leaked into other projects: legacy/ann=%s ops/ann=%s", sig["legacy/ann"], sig["ops/ann"])
		}
	})
	t.Run("FUNC_DemoteFreshAssigneeManagerRespectsInvariant_ivy", func(t *testing.T) {
		// handled below by ATOM_*; here: ivy (manager) demoted to contributor with a task still fine.
		h := newServer()
		zgMust(t, h, "ivy", "POST", "/projects/web/tasks", `{"title":"Ivy task","assignee":"ivy"}`, 201)
		zgPutOK(t, h, "ann", "web", "ivy", "contributor")
		code, ts := zgTasksCode(h)
		_ = code
		found := false
		for _, x := range ts {
			if x.Title == "Ivy task" {
				found = true
				if x.Assignee != "ivy" {
					t.Errorf("task assignee changed to %q when demoting manager to contributor (still a valid assignee)", x.Assignee)
				}
			}
		}
		if !found {
			t.Errorf("task vanished")
		}
	})
	t.Run("FUNC_ConsecutiveChanges", func(t *testing.T) {
		h := newServer()
		zgPutOK(t, h, "ivy", "web", "dan", "contributor")
		zgPutOK(t, h, "ivy", "web", "dan", "manager")
		zgPutOK(t, h, "dan", "web", "ann", "viewer")
		zgPutOK(t, h, "dan", "web", "dan", "manager")
		sig := zgSig(h)
		if sig["web/dan"] != "400/400" || sig["web/ann"] != "403/403" {
			t.Errorf("sig dan=%s ann=%s", sig["web/dan"], sig["web/ann"])
		}
	})

	// ---------- ATOM: assignee invariant (assignee must be contributor/manager) ----------
	// Accept either rejection (non-2xx, nothing changed) or success with the demoted user
	// no longer assigned any task in that project. Leaving a viewer as assignee is the failure.
	inv := func(name, actor, pid, uid, role string, tids ...string) {
		t.Run("ATOM_"+name, func(t *testing.T) {
			h := newServer()
			before := zgSnap(t, h)
			code, body := zgPut(h, actor, pid, uid, role)
			after := zgSnap(t, h)
			if code >= 200 && code < 300 {
				for _, x := range after.tasks {
					if x.ProjectID == pid && x.Assignee == uid {
						t.Errorf("invariant violated: %s is now %s in %s but still assignee of %s (PUT -> %d)", uid, role, pid, x.ID, code)
					}
				}
				// the role actually changed
				if after.sig[pid+"/"+uid] == before.sig[pid+"/"+uid] {
					t.Errorf("2xx but %s's role did not change (probe %s)", uid, after.sig[pid+"/"+uid])
				}
				// unassigning must be limited to that project/user
				for id, b := range before.tasks {
					a := after.tasks[id]
					if b.ProjectID == pid && b.Assignee == uid {
						b.Assignee = ""
					}
					if a != b {
						t.Errorf("collateral change: %s %+v -> %+v", id, before.tasks[id], a)
					}
				}
			} else {
				if d := zgDiff(before, after); d != "" {
					t.Errorf("state changed despite %d: %s", code, d)
				}
			}
			_ = body
			_ = tids
		})
	}
	inv("DemoteAssigneeToViewer_benWeb", "ivy", "web", "ben", "viewer", "t1")
	inv("DemoteAssigneeToViewer_catOps", "ivy", "ops", "cat", "viewer", "t4")
	inv("DemoteAssigneeToViewer_catWeb", "ann", "web", "cat", "viewer", "t2")
	inv("DemoteAssigneeToViewer_benWebByManagerAnn", "ann", "web", "ben", "viewer", "t1")
	t.Run("ATOM_DemotedManagerAssignee_ivy", func(t *testing.T) {
		h := newServer()
		zgMust(t, h, "ann", "POST", "/projects/web/tasks", `{"title":"Ivy task","assignee":"ivy"}`, 201)
		code, _ := zgPut(h, "ann", "web", "ivy", "viewer")
		if code >= 200 && code < 300 {
			for _, x := range zgTasks(t, h) {
				if x.ProjectID == "web" && x.Assignee == "ivy" {
					t.Errorf("invariant violated: ivy is viewer but still assignee of %s", x.ID)
				}
			}
		}
	})
	t.Run("ATOM_DemoteViaBothPathsNoViewerAssignees", func(t *testing.T) {
		// demote ben and cat in web sequentially; at the end no web task may have a viewer assignee.
		h := newServer()
		zgPut(h, "ivy", "web", "ben", "viewer")
		zgPut(h, "ivy", "web", "cat", "viewer")
		sig := zgSig(h)
		for _, x := range zgTasks(t, h) {
			if x.ProjectID != "web" || x.Assignee == "" {
				continue
			}
			s := sig["web/"+x.Assignee]
			if s == "403/403" {
				t.Errorf("task %s assigned to %s who is viewer/non-writer (probe %s)", x.ID, x.Assignee, s)
			}
		}
	})
	t.Run("ATOM_DemoteToViewerLosesWriteButStillReads", func(t *testing.T) {
		h := newServer()
		code, _ := zgPut(h, "ivy", "web", "ben", "viewer")
		if code < 200 || code >= 300 {
			return // rejected; covered by other ATOM subtests
		}
		// writes: 403 on own (former) task; reads still fine
		if c, b := zgDo(h, "ben", "PATCH", "/projects/web/tasks/t1", `{"status":"doing"}`); c != 403 {
			t.Errorf("demoted viewer ben PATCH t1 -> %d, want 403 body=%s", c, b)
		}
		zgMust(t, h, "ben", "GET", "/projects/web/tasks", "", 200)
	})

	// ---------- AUTHZ ----------
	for _, c := range []struct {
		n string
		r zgReq
	}{
		{"ContributorPromotesOther_cat", zgReq{"cat", "web", "dan", "contributor"}},
		{"ContributorSelfPromote_ben", zgReq{"ben", "web", "ben", "manager"}},
		{"ContributorDemotesManager_ben", zgReq{"ben", "web", "ann", "viewer"}},
		{"ContributorDemotesOther_cat", zgReq{"cat", "ops", "ivy", "viewer"}},
		{"ViewerSelfPromote_dan", zgReq{"dan", "web", "dan", "manager"}},
		{"ViewerPromotesOther_dan", zgReq{"dan", "web", "ben", "manager"}},
		{"ViewerAnnInOpsPromotesSelf", zgReq{"ann", "ops", "ann", "manager"}},
		{"ViewerAnnInOpsDemotesOther", zgReq{"ann", "ops", "cat", "viewer"}},
		{"OrgAdminPromotes_eve", zgReq{"eve", "web", "dan", "contributor"}},
		{"OrgAdminDemotes_eve", zgReq{"eve", "web", "ann", "viewer"}},
		{"OrgAdminOpsSelf", zgReq{"eve", "ops", "ann", "manager"}},
	} {
		c := c
		t.Run("AUTHZ_"+c.n, func(t *testing.T) { zgDenied(t, c.r, 0) })
	}
	for _, c := range []struct {
		n string
		r zgReq
	}{
		{"NonMemberHal", zgReq{"hal", "web", "dan", "contributor"}},
		{"NonMemberHalSelf", zgReq{"hal", "web", "hal", "manager"}},
		{"NonMemberBenInOps", zgReq{"ben", "ops", "cat", "viewer"}},
		{"NonMemberDanInOps", zgReq{"dan", "ops", "dan", "manager"}},
		{"CrossOrgGusInWeb", zgReq{"gus", "web", "dan", "manager"}},
		{"CrossOrgGusSelfInWeb", zgReq{"gus", "web", "gus", "manager"}},
		{"CrossOrgAnnInGx", zgReq{"ann", "gx", "gus", "viewer"}},
		{"CrossOrgIvyInGx", zgReq{"ivy", "gx", "ann", "manager"}},
		{"CrossOrgEveAdminInGx", zgReq{"eve", "gx", "gus", "viewer"}},
		{"UnknownProject", zgReq{"ann", "nope", "dan", "viewer"}},
	} {
		c := c
		t.Run("AUTHZ_"+c.n, func(t *testing.T) { zgDenied(t, c.r, 0) })
	}
	t.Run("AUTHZ_Anonymous", func(t *testing.T) { zgDenied(t, zgReq{"", "web", "dan", "contributor"}, 0) })
	t.Run("AUTHZ_UnknownUser", func(t *testing.T) { zgDenied(t, zgReq{"zed", "web", "dan", "contributor"}, 0) })
	t.Run("AUTHZ_ArchivedProjectManager", func(t *testing.T) {
		zgDenied(t, zgReq{"ann", "legacy", "ben", "viewer"}, 0)
	})
	t.Run("AUTHZ_ArchivedPromote", func(t *testing.T) {
		zgDenied(t, zgReq{"ann", "legacy", "ben", "manager"}, 0)
	})
	t.Run("AUTHZ_ArchivedNoopChange", func(t *testing.T) {
		zgDenied(t, zgReq{"ann", "legacy", "ben", "contributor"}, 0)
	})
	t.Run("AUTHZ_ArchivedByContributor", func(t *testing.T) {
		zgDenied(t, zgReq{"ben", "legacy", "ann", "viewer"}, 0)
	})
	t.Run("AUTHZ_CrossOrgTargetNotAdded_gusToWeb", func(t *testing.T) {
		for _, role := range []string{"viewer", "contributor", "manager"} {
			h := newServer()
			before := zgSnap(t, h)
			code, body := zgPut(h, "ivy", "web", "gus", role)
			if code >= 200 && code < 300 {
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
		code, body := zgPut(h, "gus", "gx", "ann", "manager")
		if code >= 200 && code < 300 {
			t.Errorf("unexpected acceptance: adding other-org ann to gx -> %d body=%s", code, body)
		}
		if c, _ := zgDo(h, "ann", "GET", "/projects/gx/tasks", ""); c != 404 {
			t.Errorf("ann can now read gx: %d (cross-org membership created)", c)
		}
		if c, b := zgDo(h, "ann", "POST", "/projects/gx/tasks", `{"title":"pwn","assignee":"ann"}`); c < 300 && c >= 200 {
			t.Errorf("ann can write in gx: %d %s", c, b)
		}
	})
	t.Run("AUTHZ_UnknownTargetNotAdded", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		for _, uid := range []string{"zed", "ANN", "nobody"} {
			code, body := zgPut(h, "ivy", "web", uid, "manager")
			if code >= 200 && code < 300 {
				t.Errorf("unexpected acceptance: unknown target %q -> %d body=%s", uid, code, body)
			}
			if c, b := zgDo(h, uid, "GET", "/projects/web/tasks", ""); c == 200 {
				t.Errorf("unknown user %q can read web: %s", uid, b)
			}
		}
		if d := zgDiff(before, zgSnap(t, h)); d != "" {
			t.Errorf("state changed: %s", d)
		}
	})
	t.Run("AUTHZ_DeniedNoLeakBodies", func(t *testing.T) {
		h := newServer()
		for _, r := range []zgReq{{"hal", "web", "dan", "viewer"}, {"gus", "web", "dan", "viewer"}, {"", "web", "dan", "viewer"}, {"ann", "gx", "gus", "viewer"}} {
			_, body := zgPut(h, r.user, r.pid, r.uid, r.role)
			if l := zgLeaks(r.user, body); len(l) > 0 {
				t.Errorf("leak: %v in %s", l, body)
			}
		}
	})
	t.Run("AUTHZ_ManagerOfOtherProjectCannotChange", func(t *testing.T) {
		// ann manages web but is only a viewer in ops; ivy manages ops, but legacy is not hers.
		zgDenied(t, zgReq{"ivy", "legacy", "ann", "viewer"}, 0)
		zgDenied(t, zgReq{"ann", "ops", "cat", "manager"}, 0)
	})

	// ---------- VALID ----------
	for _, c := range []struct{ n, role string }{
		{"RoleOwner", "owner"}, {"RoleEmpty", ""}, {"RoleCapitalized", "Manager"}, {"RoleAdmin", "admin"},
		{"RoleOrgAdmin", "orgadmin"}, {"RoleSpace", " viewer"}, {"RoleNone", "none"},
	} {
		c := c
		t.Run("VALID_"+c.n, func(t *testing.T) {
			zgDenied(t, zgReq{"ivy", "web", "dan", c.role}, 0)
		})
	}
	t.Run("VALID_MissingRoleField", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		for _, body := range []string{`{}`, `{"role":null}`, `{"role":5}`, `not json`, `[]`, `{"roles":"viewer"}`} {
			code, resp := zgDo(h, "ivy", "PUT", "/projects/web/members/dan", body)
			if code >= 200 && code < 300 {
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
		code, _ := zgDo(h, "ivy", "PUT", "/projects/web/members/dan", "")
		if code >= 200 && code < 300 {
			t.Errorf("unexpected acceptance: empty body -> %d", code)
		}
		if d := zgDiff(before, zgSnap(t, h)); d != "" {
			t.Errorf("state changed: %s", d)
		}
	})
	t.Run("VALID_InvalidRoleByUnauthorizedStillDenied", func(t *testing.T) {
		zgDenied(t, zgReq{"dan", "web", "dan", "superuser"}, 0)
	})

	// ---------- CONTRACT ----------
	for _, c := range []struct {
		n    string
		r    zgReq
		want int
	}{
		{"Contributor403", zgReq{"ben", "web", "dan", "contributor"}, 403},
		{"Viewer403", zgReq{"dan", "web", "dan", "manager"}, 403},
		{"OrgAdmin403", zgReq{"eve", "web", "dan", "contributor"}, 403},
		{"ViewerAnnOps403", zgReq{"ann", "ops", "cat", "viewer"}, 403},
		{"NonMember404", zgReq{"hal", "web", "dan", "contributor"}, 404},
		{"CrossOrg404", zgReq{"gus", "web", "dan", "contributor"}, 404},
		{"NotMemberOfThatProject404", zgReq{"ben", "ops", "cat", "viewer"}, 404},
		{"UnknownProject404", zgReq{"ann", "nope", "dan", "viewer"}, 404},
		{"Anonymous401", zgReq{"", "web", "dan", "contributor"}, 401},
		{"UnknownUser401", zgReq{"zed", "web", "dan", "contributor"}, 401},
		{"Archived409", zgReq{"ann", "legacy", "ben", "viewer"}, 409},
		{"InvalidRole400", zgReq{"ivy", "web", "dan", "owner"}, 400},
		{"EmptyRole400", zgReq{"ivy", "web", "dan", ""}, 400},
	} {
		c := c
		t.Run("CONTRACT_"+c.n, func(t *testing.T) {
			h := newServer()
			code, body := zgPut(h, c.r.user, c.r.pid, c.r.uid, c.r.role)
			if code != c.want {
				t.Errorf("PUT as %q -> %d, want %d body=%s", c.r.user, code, c.want, body)
			}
		})
	}
	t.Run("CONTRACT_ErrorShape", func(t *testing.T) {
		h := newServer()
		code, body := zgPut(h, "dan", "web", "dan", "manager")
		var e map[string]string
		if code != 403 || json.Unmarshal([]byte(body), &e) != nil || e["error"] == "" {
			t.Errorf("-> %d body=%s, want 403 {\"error\":...}", code, body)
		}
	})
	t.Run("CONTRACT_ContentType", func(t *testing.T) {
		h := newServer()
		req := httptest.NewRequest("PUT", "/projects/web/members/dan", strings.NewReader(`{"role":"contributor"}`))
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
	t.Run("CONTRACT_TargetOtherOrgOrUnknown_400or404", func(t *testing.T) {
		h := newServer()
		for _, uid := range []string{"gus", "zed"} {
			code, _ := zgPut(h, "ivy", "web", uid, "contributor")
			if code != 400 && code != 404 {
				t.Errorf("target %s -> %d, want 400 or 404", uid, code)
			}
		}
	})

	// ---------- INFO ----------
	t.Run("INFO_AddSameOrgNonMemberHal", func(t *testing.T) {
		h := newServer()
		code, body := zgPut(h, "ivy", "web", "hal", "contributor")
		c2, _ := zgDo(h, "hal", "GET", "/projects/web/tasks", "")
		t.Logf("INFO PUT web/hal contributor as manager -> %d body=%s; hal GET web -> %d", code, body, c2)
	})
	t.Run("INFO_AddOrgAdminEve", func(t *testing.T) {
		h := newServer()
		code, body := zgPut(h, "ivy", "web", "eve", "viewer")
		t.Logf("INFO PUT web/eve viewer -> %d body=%s", code, body)
	})
	t.Run("INFO_SelfDemoteManager", func(t *testing.T) {
		h := newServer()
		code, body := zgPut(h, "ivy", "web", "ivy", "viewer")
		t.Logf("INFO ivy self-demote web (ann is also manager) -> %d body=%s", code, body)
	})
	t.Run("INFO_DemoteLastManager", func(t *testing.T) {
		h := newServer()
		zgPut(h, "ivy", "web", "ann", "viewer")
		code, body := zgPut(h, "ivy", "web", "ivy", "viewer")
		t.Logf("INFO demote last manager of web (ivy self) -> %d body=%s", code, body)
	})
	t.Run("INFO_ManagerChangesOwnRoleToManager", func(t *testing.T) {
		h := newServer()
		code, body := zgPut(h, "ivy", "web", "ivy", "manager")
		t.Logf("INFO ivy PUT self manager -> %d body=%s", code, body)
	})
	t.Run("INFO_DemoteAssigneeOutcome", func(t *testing.T) {
		h := newServer()
		code, body := zgPut(h, "ivy", "web", "ben", "viewer")
		_, t1 := zgDo(h, "ann", "GET", "/projects/web/tasks/t1", "")
		t.Logf("INFO demote ben (assigned t1): PUT -> %d body=%s; t1 now %s", code, body, strings.TrimSpace(t1))
	})
	t.Run("INFO_OtherMethods", func(t *testing.T) {
		h := newServer()
		for _, m := range []string{"GET", "POST", "PATCH", "DELETE"} {
			code, _ := zgDo(h, "ivy", m, "/projects/web/members/dan", `{"role":"viewer"}`)
			t.Logf("INFO %s /projects/web/members/dan as ivy -> %d", m, code)
		}
		t.Logf("INFO sig after: dan %s", zgSig(h)["web/dan"])
	})
}

func zgTasksCode(h http.Handler) (int, []zgTask) {
	code, body := zgDo(h, "ann", "GET", "/projects/web/tasks", "")
	var ts []zgTask
	json.Unmarshal([]byte(body), &ts)
	return code, ts
}
