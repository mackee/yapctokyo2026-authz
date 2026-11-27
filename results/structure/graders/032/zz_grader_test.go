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

// Hidden grader for task 032 (POST /projects/{pid}/members). Uses only newServer() and HTTP.
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

func zgAdd(h http.Handler, user, pid, uid, role string) (int, string) {
	return zgDo(h, user, "POST", "/projects/"+pid+"/members", `{"userId":"`+uid+`","role":"`+role+`"}`)
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
	code, resp := zgAdd(h, r.user, r.pid, r.uid, r.role)
	after := zgSnap(t, h)
	if code >= 200 && code < 300 {
		t.Errorf("unexpected acceptance: POST members %s/%s role=%s as %q -> %d body=%s", r.pid, r.uid, r.role, r.user, code, resp)
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

func zgAddOK(t *testing.T, h http.Handler, user, pid, uid, role string) {
	t.Helper()
	code, body := zgAdd(h, user, pid, uid, role)
	if code != 201 {
		t.Fatalf("POST members %s/%s %s as %s -> %d body=%s", pid, uid, role, user, code, body)
	}
}

// zgNoAssignee fails if any task in pid is assigned to uid.
func zgNoAssignee(t *testing.T, h http.Handler, pid, uid string) {
	t.Helper()
	for _, x := range zgTasks(t, h) {
		if x.ProjectID == pid && x.Assignee == uid {
			t.Errorf("task %s in %s is assigned to %s (must not be a member)", x.ID, pid, uid)
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
	t.Run("FUNC_AddHalContributor_ResponseBody", func(t *testing.T) {
		h := newServer()
		code, body := zgAdd(h, "ivy", "web", "hal", "contributor")
		if code != 201 {
			t.Fatalf("-> %d body=%s", code, body)
		}
		var r map[string]any
		if err := json.Unmarshal([]byte(body), &r); err != nil {
			t.Fatalf("decode: %v body=%s", err, body)
		}
		if r["userId"] != "hal" || r["role"] != "contributor" {
			t.Errorf("body = %s, want {userId:hal, role:contributor}", body)
		}
	})
	t.Run("FUNC_AddedContributorCanReadAndCreateSelfAssigned", func(t *testing.T) {
		h := newServer()
		zgMust(t, h, "hal", "GET", "/projects/web/tasks", "", 404)
		zgMust(t, h, "ivy", "POST", "/projects/web/tasks", `{"title":"pre","assignee":"hal"}`, 400)
		zgAddOK(t, h, "ivy", "web", "hal", "contributor")
		zgMust(t, h, "hal", "GET", "/projects/web/tasks", "", 200)
		zgMust(t, h, "hal", "POST", "/projects/web/tasks", `{"title":"mine","assignee":"hal"}`, 201)
		zgMust(t, h, "hal", "PATCH", "/projects/web/tasks/t1", `{"status":"done"}`, 403)
	})
	t.Run("FUNC_AddedMemberBecomesAssignable", func(t *testing.T) {
		h := newServer()
		zgAddOK(t, h, "ann", "web", "hal", "contributor")
		zgMust(t, h, "ivy", "POST", "/projects/web/tasks", `{"title":"for hal","assignee":"hal"}`, 201)
		zgMust(t, h, "ann", "PATCH", "/projects/web/tasks/t3", `{"assignee":"hal"}`, 200)
	})
	t.Run("FUNC_AddViewerToOps_ReadsButCannotWrite", func(t *testing.T) {
		h := newServer()
		zgMust(t, h, "hal", "GET", "/projects/ops/tasks", "", 404)
		zgAddOK(t, h, "ivy", "ops", "hal", "viewer")
		zgMust(t, h, "hal", "GET", "/projects/ops/tasks", "", 200)
		zgMust(t, h, "hal", "POST", "/projects/ops/tasks", `{"title":"x"}`, 403)
		zgMust(t, h, "hal", "PATCH", "/projects/ops/tasks/t4", `{"status":"done"}`, 403)
		zgMust(t, h, "hal", "DELETE", "/projects/ops/tasks/t4", "", 403)
		// a viewer is not a valid assignee
		code, body := zgDo(h, "ivy", "POST", "/projects/ops/tasks", `{"title":"x","assignee":"hal"}`)
		if code >= 200 && code < 300 {
			t.Errorf("viewer hal accepted as assignee: %d %s", code, body)
		}
		zgNoAssignee(t, h, "ops", "hal")
	})
	t.Run("FUNC_AddManagerCanDelete", func(t *testing.T) {
		h := newServer()
		zgMust(t, h, "hal", "DELETE", "/projects/web/tasks/t3", "", 404)
		zgAddOK(t, h, "ivy", "web", "hal", "manager")
		zgMust(t, h, "hal", "DELETE", "/projects/web/tasks/t3", "", 204)
	})
	t.Run("FUNC_NewManagerCanAddOthers", func(t *testing.T) {
		h := newServer()
		zgAddOK(t, h, "ivy", "ops", "hal", "manager")
		zgAddOK(t, h, "hal", "ops", "ben", "viewer")
		zgMust(t, h, "ben", "GET", "/projects/ops/tasks", "", 200)
	})
	t.Run("FUNC_AddExistingOrgMembersToOps", func(t *testing.T) {
		h := newServer()
		zgAddOK(t, h, "ivy", "ops", "ben", "contributor")
		zgAddOK(t, h, "ivy", "ops", "dan", "viewer")
		zgMust(t, h, "ben", "POST", "/projects/ops/tasks", `{"title":"b","assignee":"ben"}`, 201)
		zgMust(t, h, "dan", "GET", "/projects/ops/tasks", "", 200)
		zgMust(t, h, "dan", "POST", "/projects/ops/tasks", `{"title":"d"}`, 403)
	})
	t.Run("FUNC_AddOnlyAffectsThatProject", func(t *testing.T) {
		h := newServer()
		before := zgSig(h)
		zgAddOK(t, h, "ivy", "web", "hal", "manager")
		after := zgSig(h)
		for k, v := range before {
			if k == "web/hal" {
				if after[k] == v {
					t.Errorf("web/hal probe unchanged: %s", v)
				}
				continue
			}
			if after[k] != v {
				t.Errorf("probe %s %s -> %s (collateral change)", k, v, after[k])
			}
		}
		zgMust(t, h, "hal", "GET", "/projects/ops/tasks", "", 404)
		zgMust(t, h, "hal", "GET", "/projects/legacy/tasks", "", 404)
	})
	t.Run("FUNC_ProbeSeesAddedRole", func(t *testing.T) {
		h := newServer()
		zgAddOK(t, h, "ivy", "web", "hal", "viewer")
		zgAddOK(t, h, "ivy", "ops", "ben", "contributor")
		sig := zgSig(h)
		if sig["web/hal"] != "403/403" || sig["ops/ben"] != "403/400" {
			t.Errorf("web/hal=%s ops/ben=%s", sig["web/hal"], sig["ops/ben"])
		}
	})

	// ---------- AUTHZ ----------
	for _, c := range []struct {
		n string
		r zgReq
	}{
		{"ContributorAdds_ben", zgReq{"ben", "web", "hal", "contributor"}},
		{"ContributorAddsSelfEscalate_ben", zgReq{"ben", "web", "ben", "manager"}},
		{"ContributorCatOps", zgReq{"cat", "ops", "hal", "viewer"}},
		{"ViewerAdds_dan", zgReq{"dan", "web", "hal", "viewer"}},
		{"ViewerAnnInOps", zgReq{"ann", "ops", "hal", "contributor"}},
		{"ViewerAnnInOpsAddsSelf", zgReq{"ann", "ops", "ann", "manager"}},
		{"OrgAdminAdds_eve", zgReq{"eve", "web", "hal", "contributor"}},
		{"OrgAdminAddsSelf_eve", zgReq{"eve", "web", "eve", "manager"}},
		{"NonMemberHal", zgReq{"hal", "web", "hal", "manager"}},
		{"NonMemberBenInOps", zgReq{"ben", "ops", "hal", "viewer"}},
		{"NonMemberBenInOpsSelf", zgReq{"ben", "ops", "ben", "manager"}},
		{"CrossOrgGusInWeb", zgReq{"gus", "web", "hal", "manager"}},
		{"CrossOrgGusSelfInWeb", zgReq{"gus", "web", "gus", "manager"}},
		{"CrossOrgAnnInGx", zgReq{"ann", "gx", "ann", "manager"}},
		{"CrossOrgIvyInGx", zgReq{"ivy", "gx", "hal", "viewer"}},
		{"CrossOrgEveAdminInGx", zgReq{"eve", "gx", "eve", "manager"}},
		{"UnknownProject", zgReq{"ann", "nope", "hal", "viewer"}},
		{"Anonymous", zgReq{"", "web", "hal", "contributor"}},
		{"UnknownUser", zgReq{"zed", "web", "hal", "contributor"}},
		{"ArchivedManager", zgReq{"ann", "legacy", "hal", "viewer"}},
		{"ArchivedManagerExistingUserNotMember", zgReq{"ann", "legacy", "cat", "contributor"}},
		{"ArchivedByContributor", zgReq{"ben", "legacy", "hal", "viewer"}},
		{"ManagerOfOtherProject_annInOps", zgReq{"ann", "ops", "ben", "contributor"}},
		{"ManagerOfOtherProject_ivyInLegacy", zgReq{"ivy", "legacy", "hal", "viewer"}},
	} {
		c := c
		t.Run("AUTHZ_"+c.n, func(t *testing.T) { zgDenied(t, c.r, 0) })
	}
	t.Run("AUTHZ_CrossOrgTargetNotAdded_gusToWeb", func(t *testing.T) {
		for _, role := range []string{"viewer", "contributor", "manager"} {
			h := newServer()
			before := zgSnap(t, h)
			code, body := zgAdd(h, "ivy", "web", "gus", role)
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
	t.Run("AUTHZ_CrossOrgTargetNotAssignable_gusInWeb", func(t *testing.T) {
		for _, role := range []string{"contributor", "manager"} {
			h := newServer()
			zgAdd(h, "ivy", "web", "gus", role)
			code, body := zgDo(h, "ivy", "POST", "/projects/web/tasks", `{"title":"for gus","assignee":"gus"}`)
			if code >= 200 && code < 300 {
				t.Errorf("task assigned to other-org gus created after add as %s: %d %s", role, code, body)
			}
			zgNoAssignee(t, h, "web", "gus")
			code, body = zgDo(h, "ivy", "PATCH", "/projects/web/tasks/t3", `{"assignee":"gus"}`)
			if code >= 200 && code < 300 {
				t.Errorf("reassign to gus accepted after add as %s: %d %s", role, code, body)
			}
			zgNoAssignee(t, h, "web", "gus")
			if c, b := zgDo(h, "gus", "GET", "/projects/web/tasks", ""); c == 200 {
				t.Errorf("gus reads web: %s", b)
			}
		}
	})
	t.Run("AUTHZ_CrossOrgTargetNotAdded_gusToOps", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		code, body := zgAdd(h, "ivy", "ops", "gus", "viewer")
		if code >= 200 && code < 300 {
			t.Errorf("unexpected acceptance: gus to ops -> %d %s", code, body)
		}
		if c, b := zgDo(h, "gus", "GET", "/projects/ops/tasks", ""); c == 200 {
			t.Errorf("gus reads ops: %s", b)
		}
		if d := zgDiff(before, zgSnap(t, h)); d != "" {
			t.Errorf("state changed: %s", d)
		}
	})
	t.Run("AUTHZ_CrossOrgTargetNotAdded_annToGx", func(t *testing.T) {
		h := newServer()
		code, body := zgAdd(h, "gus", "gx", "ann", "manager")
		if code >= 200 && code < 300 {
			t.Errorf("unexpected acceptance: adding other-org ann to gx -> %d body=%s", code, body)
		}
		if c, _ := zgDo(h, "ann", "GET", "/projects/gx/tasks", ""); c != 404 {
			t.Errorf("ann can now read gx: %d", c)
		}
		if c, b := zgDo(h, "gus", "POST", "/projects/gx/tasks", `{"title":"x","assignee":"ann"}`); c >= 200 && c < 300 {
			t.Errorf("gx task assigned to other-org ann: %d %s", c, b)
		}
	})
	t.Run("AUTHZ_UnknownTargetNotAdded", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		for _, uid := range []string{"zed", "HAL", "nobody", "hal ", ""} {
			code, body := zgAdd(h, "ivy", "web", uid, "manager")
			if code >= 200 && code < 300 {
				t.Errorf("unexpected acceptance: unknown target %q -> %d body=%s", uid, code, body)
			}
			if c, b := zgDo(h, uid, "GET", "/projects/web/tasks", ""); c == 200 {
				t.Errorf("unknown user %q can read web: %s", uid, b)
			}
		}
		zgNoAssignee(t, h, "web", "zed")
		if d := zgDiff(before, zgSnap(t, h)); d != "" {
			t.Errorf("state changed: %s", d)
		}
	})
	t.Run("AUTHZ_DeniedNoLeakBodies", func(t *testing.T) {
		h := newServer()
		for _, r := range []zgReq{{"hal", "web", "hal", "viewer"}, {"gus", "web", "hal", "viewer"}, {"", "web", "hal", "viewer"}, {"ann", "gx", "ann", "viewer"}, {"ben", "ops", "ben", "viewer"}} {
			_, body := zgAdd(h, r.user, r.pid, r.uid, r.role)
			if l := zgLeaks(r.user, body); len(l) > 0 {
				t.Errorf("leak: %v in %s", l, body)
			}
		}
	})
	t.Run("ATOM_ReAddExistingMemberOtherProjectsUntouched", func(t *testing.T) {
		// Outcome for the target project is ambiguous (INFO); other projects must not change.
		h := newServer()
		before := zgSnap(t, h)
		zgAdd(h, "ivy", "web", "dan", "manager")
		zgAdd(h, "ivy", "web", "ben", "viewer")
		zgAdd(h, "ivy", "web", "ann", "viewer")
		after := zgSnap(t, h)
		for k, v := range before.sig {
			if !strings.HasPrefix(k, "web/") && after.sig[k] != v {
				t.Errorf("probe %s %s -> %s", k, v, after.sig[k])
			}
		}
		for id, x := range before.tasks {
			if x.ProjectID != "web" && after.tasks[id] != x {
				t.Errorf("task %s changed: %+v -> %+v", id, x, after.tasks[id])
			}
		}
	})
	t.Run("ATOM_ReAddDoesNotDemoteAssigneeIntoViewerAssignee", func(t *testing.T) {
		// If a re-add (ben viewer) is accepted, ben must not remain assignee of t1 as a viewer.
		h := newServer()
		code, _ := zgAdd(h, "ivy", "web", "ben", "viewer")
		if code >= 200 && code < 300 {
			sig := zgSig(h)
			if sig["web/ben"] == "403/403" {
				for _, x := range zgTasks(t, h) {
					if x.ProjectID == "web" && x.Assignee == "ben" {
						t.Errorf("ben is viewer but still assignee of %s", x.ID)
					}
				}
			}
		}
	})
	t.Run("ATOM_DeniedAddLeavesNoPartialMembership", func(t *testing.T) {
		// invalid role for a valid target must not add the target with some default role.
		h := newServer()
		for _, role := range []string{"owner", "", "admin"} {
			zgAdd(h, "ivy", "web", "hal", role)
		}
		if c, b := zgDo(h, "hal", "GET", "/projects/web/tasks", ""); c != 404 {
			t.Errorf("hal readable after invalid-role adds: %d %s", c, b)
		}
	})

	// ---------- VALID ----------
	for _, c := range []struct{ n, role string }{
		{"RoleOwner", "owner"}, {"RoleEmpty", ""}, {"RoleCapitalized", "Manager"}, {"RoleAdmin", "admin"},
		{"RoleOrgAdmin", "orgadmin"}, {"RoleSpace", " viewer"}, {"RoleNone", "none"},
	} {
		c := c
		t.Run("VALID_"+c.n, func(t *testing.T) {
			zgDenied(t, zgReq{"ivy", "web", "hal", c.role}, 0)
			if c, _ := zgDo(newServer(), "ivy", "GET", "/projects/web/tasks", ""); c != 200 {
				t.Errorf("sanity")
			}
		})
	}
	t.Run("VALID_MalformedBodies", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		for _, body := range []string{`{}`, `{"role":"viewer"}`, `{"userId":"hal"}`, `{"userId":null,"role":"viewer"}`,
			`{"userId":5,"role":"viewer"}`, `{"userId":"hal","role":null}`, `{"userId":"hal","role":5}`, `not json`, `[]`,
			`{"user":"hal","role":"viewer"}`, `{"userId":["hal"],"role":"viewer"}`} {
			code, resp := zgDo(h, "ivy", "POST", "/projects/web/members", body)
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
		code, _ := zgDo(h, "ivy", "POST", "/projects/web/members", "")
		if code >= 200 && code < 300 {
			t.Errorf("unexpected acceptance: empty body -> %d", code)
		}
		if d := zgDiff(before, zgSnap(t, h)); d != "" {
			t.Errorf("state changed: %s", d)
		}
	})
	t.Run("VALID_InvalidRoleByUnauthorizedStillDenied", func(t *testing.T) {
		zgDenied(t, zgReq{"dan", "web", "hal", "superuser"}, 0)
		zgDenied(t, zgReq{"hal", "web", "hal", "superuser"}, 0)
	})

	// ---------- CONTRACT ----------
	for _, c := range []struct {
		n    string
		r    zgReq
		want int
	}{
		{"Created201", zgReq{"ivy", "web", "hal", "contributor"}, 201},
		{"Contributor403", zgReq{"ben", "web", "hal", "contributor"}, 403},
		{"Viewer403", zgReq{"dan", "web", "hal", "viewer"}, 403},
		{"OrgAdmin403", zgReq{"eve", "web", "hal", "contributor"}, 403},
		{"ViewerAnnOps403", zgReq{"ann", "ops", "hal", "viewer"}, 403},
		{"NonMember404", zgReq{"hal", "web", "hal", "contributor"}, 404},
		{"CrossOrg404", zgReq{"gus", "web", "hal", "contributor"}, 404},
		{"NotMemberOfThatProject404", zgReq{"ben", "ops", "hal", "viewer"}, 404},
		{"UnknownProject404", zgReq{"ann", "nope", "hal", "viewer"}, 404},
		{"Anonymous401", zgReq{"", "web", "hal", "contributor"}, 401},
		{"UnknownUser401", zgReq{"zed", "web", "hal", "contributor"}, 401},
		{"Archived409", zgReq{"ann", "legacy", "hal", "viewer"}, 409},
		{"InvalidRole400", zgReq{"ivy", "web", "hal", "owner"}, 400},
		{"TrailingSpaceRole400", zgReq{"ivy", "web", "hal", "viewer "}, 400},
	} {
		c := c
		t.Run("CONTRACT_"+c.n, func(t *testing.T) {
			h := newServer()
			code, body := zgAdd(h, c.r.user, c.r.pid, c.r.uid, c.r.role)
			if code != c.want {
				t.Errorf("POST as %q -> %d, want %d body=%s", c.r.user, code, c.want, body)
			}
		})
	}
	t.Run("CONTRACT_MissingUserId400", func(t *testing.T) {
		code, body := zgDo(newServer(), "ivy", "POST", "/projects/web/members", `{"role":"viewer"}`)
		if code != 400 {
			t.Errorf("-> %d body=%s, want 400", code, body)
		}
	})
	t.Run("CONTRACT_ErrorShape", func(t *testing.T) {
		h := newServer()
		code, body := zgAdd(h, "dan", "web", "hal", "viewer")
		var e map[string]string
		if code != 403 || json.Unmarshal([]byte(body), &e) != nil || e["error"] == "" {
			t.Errorf("-> %d body=%s, want 403 {\"error\":...}", code, body)
		}
	})
	t.Run("CONTRACT_ContentType", func(t *testing.T) {
		h := newServer()
		req := httptest.NewRequest("POST", "/projects/web/members", strings.NewReader(`{"userId":"hal","role":"contributor"}`))
		req.Header.Set("X-User", "ivy")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 201 {
			t.Fatalf("-> %d", rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Errorf("Content-Type = %q", ct)
		}
	})
	t.Run("CONTRACT_TargetOtherOrgOrUnknown_400or404or403", func(t *testing.T) {
		h := newServer()
		for _, uid := range []string{"gus", "zed"} {
			code, _ := zgAdd(h, "ivy", "web", uid, "contributor")
			if code != 400 && code != 404 && code != 403 && code != 422 {
				t.Errorf("target %s -> %d, want a 4xx rejection (400/403/404/422)", uid, code)
			}
		}
	})

	// ---------- INFO ----------
	t.Run("INFO_AddOrgAdminEve", func(t *testing.T) {
		h := newServer()
		code, body := zgAdd(h, "ivy", "web", "eve", "contributor")
		t.Logf("INFO add eve (org admin) contributor -> %d body=%s; probe web/eve=%s", code, body, zgSig(h)["web/eve"])
	})
	t.Run("INFO_AddSelfManager", func(t *testing.T) {
		h := newServer()
		code, body := zgAdd(h, "ivy", "web", "ivy", "manager")
		t.Logf("INFO ivy adds self (already manager) -> %d body=%s", code, body)
	})
	t.Run("INFO_ReAddDanAsManager", func(t *testing.T) {
		h := newServer()
		code, body := zgAdd(h, "ivy", "web", "dan", "manager")
		t.Logf("INFO re-add dan viewer->manager -> %d body=%s; probe web/dan=%s (viewer=403/403)", code, body, zgSig(h)["web/dan"])
	})
	t.Run("INFO_ReAddSameRole", func(t *testing.T) {
		h := newServer()
		code, body := zgAdd(h, "ivy", "web", "ben", "contributor")
		t.Logf("INFO re-add ben same role -> %d body=%s", code, body)
	})
	t.Run("INFO_AddHalTwice", func(t *testing.T) {
		h := newServer()
		c1, _ := zgAdd(h, "ivy", "web", "hal", "contributor")
		c2, b2 := zgAdd(h, "ivy", "web", "hal", "viewer")
		t.Logf("INFO add hal twice -> %d then %d body=%s; probe web/hal=%s", c1, c2, b2, zgSig(h)["web/hal"])
	})
	t.Run("INFO_AddEveToOpsViewerReadsAlready", func(t *testing.T) {
		h := newServer()
		code, _ := zgAdd(h, "ivy", "ops", "eve", "viewer")
		t.Logf("INFO add eve viewer to ops -> %d", code)
	})
	t.Run("INFO_OtherMethods", func(t *testing.T) {
		h := newServer()
		for _, m := range []string{"GET", "PUT", "PATCH", "DELETE"} {
			code, _ := zgDo(h, "ivy", m, "/projects/web/members", `{"userId":"hal","role":"viewer"}`)
			t.Logf("INFO %s /projects/web/members as ivy -> %d", m, code)
		}
		t.Logf("INFO sig web/hal after: %s", zgSig(h)["web/hal"])
	})
	t.Run("INFO_ResponseShape", func(t *testing.T) {
		code, body := zgAdd(newServer(), "ivy", "web", "hal", "viewer")
		t.Logf("INFO response %d %s", code, strings.TrimSpace(body))
	})
}
