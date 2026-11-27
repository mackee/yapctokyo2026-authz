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

// Hidden grader for task 019 (DELETE /orgs/{orgID}/users/{uid}: offboard a user).
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

func zgOff(h http.Handler, user, org, uid string) (int, string) {
	return zgDo(h, user, "DELETE", "/orgs/"+org+"/users/"+uid, "")
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

// zgTasks reads every task through existing endpoints (eve reads all acme projects; gus reads gx).
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
//	PATCH <task> {"assignee":"zzz"}: 404 non-member, 403 viewer/org admin/contributor, 400 manager (409 if archived, 403 viewer)
//	POST  tasks  {}               : 404 non-member, 403 viewer/org admin, 400 contributor/manager (409 if archived)
//
// A user whose record was deleted would show 401/401 (also "gone").
func zgSig(h http.Handler) map[string]string {
	out := map[string]string{}
	for _, p := range []struct{ pid, tid string }{{"web", "t3"}, {"ops", "t4"}, {"legacy", "t5"}, {"gx", "t6"}} {
		for _, u := range zgUsers {
			c1, _ := zgDo(h, u, "PATCH", "/projects/"+p.pid+"/tasks/"+p.tid, `{"assignee":"zzz"}`)
			c2, _ := zgDo(h, u, "POST", "/projects/"+p.pid+"/tasks", `{"title":"probe","assignee":"zzz"}`)
			out[p.pid+"/"+u] = fmt.Sprintf("%d/%d", c1, c2)
		}
	}
	return out
}

func zgGone(s string) bool   { return s == "404/404" || s == "401/401" }
func zgWriter(s string) bool { return s == "400/400" || s == "403/400" || s == "409/409" }

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

// zgCheckOff validates a SUCCESSFUL offboarding of uid (acme) against the before/after snapshots:
//   - uid is no member of any acme project (gone in web/ops/legacy)
//   - nobody else's roles changed, gx untouched
//   - tasks: unchanged, except that tasks assigned to uid may have been unassigned ("")
//   - no task keeps uid, or any other non-member/viewer, as assignee
func zgCheckOff(before, after zgState, uid string) []string {
	var p []string
	for k, b := range before.sig {
		a := after.sig[k]
		pid, u, _ := strings.Cut(k, "/")
		if u == uid && pid != "gx" {
			if !zgGone(a) {
				p = append(p, fmt.Sprintf("still has access: %s probe %s (was %s)", k, a, b))
			}
		} else if a != b {
			p = append(p, fmt.Sprintf("collateral role change: %s %s -> %s", k, b, a))
		}
	}
	for id, b := range before.tasks {
		a, ok := after.tasks[id]
		if !ok {
			p = append(p, id+" vanished")
			continue
		}
		if b.ProjectID != "gx" && b.Assignee == uid {
			b.Assignee = ""
			if a.Assignee != "" {
				p = append(p, fmt.Sprintf("invariant violated: %s still assigned to %q after offboarding %s", id, a.Assignee, uid))
				continue
			}
		}
		if a != b {
			p = append(p, fmt.Sprintf("collateral task change: %s %+v -> %+v", id, before.tasks[id], a))
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

type zgReq struct{ user, org, uid string }

// zgDenied: non-2xx, nothing changed (tasks and roles), no leak; want!=0 checks exact status.
func zgDenied(t *testing.T, r zgReq, want int) {
	t.Helper()
	h := newServer()
	before := zgSnap(t, h)
	code, resp := zgOff(h, r.user, r.org, r.uid)
	after := zgSnap(t, h)
	if zg2xx(code) {
		t.Errorf("unexpected acceptance: DELETE /orgs/%s/users/%s as %q -> %d body=%s", r.org, r.uid, r.user, code, resp)
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

// zgOffOK offboards uid as eve, requires 2xx and a fully consistent result.
func zgOffOK(t *testing.T, h http.Handler, uid string) (zgState, zgState) {
	t.Helper()
	before := zgSnap(t, h)
	code, body := zgOff(h, "eve", "acme", uid)
	if !zg2xx(code) {
		t.Fatalf("DELETE /orgs/acme/users/%s as eve -> %d body=%s, want 2xx", uid, code, body)
	}
	after := zgSnap(t, h)
	for _, p := range zgCheckOff(before, after, uid) {
		t.Errorf("%s", p)
	}
	return before, after
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
		{"CreateOK", "ann", "POST", "/projects/web/tasks", `{"title":"x","assignee":"ben"}`, 200},
		{"CreateViewerAssignee400", "ann", "POST", "/projects/web/tasks", `{"title":"x","assignee":"dan"}`, 400},
		{"PatchContributorOthers403", "ben", "PATCH", "/projects/web/tasks/t2", `{"status":"done"}`, 403},
		{"PatchContributorOwn", "ben", "PATCH", "/projects/web/tasks/t1", `{"status":"doing"}`, 200},
		{"PatchContributorReassign403", "ben", "PATCH", "/projects/web/tasks/t1", `{"assignee":"cat"}`, 403},
		{"PatchManagerReassignOK", "ivy", "PATCH", "/projects/web/tasks/t3", `{"assignee":"ben"}`, 200},
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
			"gx/gus": "400/400", "gx/ann": "404/404", "gx/eve": "404/404",
		}
		for k, v := range want {
			if sig[k] != v {
				t.Errorf("probe %s = %s, want %s", k, sig[k], v)
			}
		}
	})

	// ---------- FUNC (users without assigned tasks: dan, hal, ivy, ann) ----------
	t.Run("FUNC_OffboardViewerDan", func(t *testing.T) {
		h := newServer()
		zgOffOK(t, h, "dan")
		if c, b := zgDo(h, "dan", "GET", "/projects/web/tasks", ""); c == 200 {
			t.Errorf("dan can still read web: %s", b)
		}
		if c, b := zgDo(h, "dan", "GET", "/projects/web/tasks/t1", ""); c == 200 {
			t.Errorf("dan can still read web/t1: %s", b)
		}
	})
	t.Run("FUNC_OffboardNoMembershipsHal", func(t *testing.T) {
		h := newServer()
		before, after := zgOffOK(t, h, "hal")
		if d := zgDiff(before, after); d != "" {
			t.Errorf("offboarding a user without memberships changed state: %s", d)
		}
	})
	t.Run("FUNC_OffboardManagerIvyTwoProjects", func(t *testing.T) {
		h := newServer()
		zgOffOK(t, h, "ivy")
		for _, pid := range []string{"web", "ops"} {
			if c, b := zgDo(h, "ivy", "GET", "/projects/"+pid+"/tasks", ""); c == 200 {
				t.Errorf("ivy can still read %s: %s", pid, b)
			}
		}
		if c, b := zgDo(h, "ivy", "POST", "/projects/ops/tasks", `{"title":"after"}`); zg2xx(c) {
			t.Errorf("ivy can still write ops: %d %s", c, b)
		}
		// ann (still manager of web) must still work normally
		zgMust(t, h, "ann", "POST", "/projects/web/tasks", `{"title":"x"}`, 200)
	})
	t.Run("FUNC_OffboardAnnAllThreeProjectsIncludingArchived", func(t *testing.T) {
		h := newServer()
		zgOffOK(t, h, "ann")
		for _, pid := range []string{"web", "ops", "legacy"} {
			if c, b := zgDo(h, "ann", "GET", "/projects/"+pid+"/tasks", ""); c == 200 {
				t.Errorf("ann can still read %s: %s", pid, b)
			}
		}
		if c, b := zgDo(h, "ann", "DELETE", "/projects/web/tasks/t3", ""); zg2xx(c) {
			t.Errorf("ann can still delete in web: %d %s", c, b)
		}
		// the remaining manager and members still work
		zgMust(t, h, "ivy", "PATCH", "/projects/web/tasks/t1", `{"status":"doing"}`, 200)
		zgMust(t, h, "ben", "GET", "/projects/legacy/tasks", "", 200)
	})
	t.Run("FUNC_OffboardedUserNotAssignableAnymore", func(t *testing.T) {
		h := newServer()
		zgOffOK(t, h, "ivy")
		if c, b := zgDo(h, "ann", "POST", "/projects/web/tasks", `{"title":"x","assignee":"ivy"}`); zg2xx(c) {
			t.Errorf("offboarded ivy accepted as assignee on create: %d %s", c, b)
		}
		if c, b := zgDo(h, "ann", "PATCH", "/projects/web/tasks/t3", `{"assignee":"ivy"}`); zg2xx(c) {
			t.Errorf("offboarded ivy accepted as assignee on update: %d %s", c, b)
		}
	})
	t.Run("FUNC_OffboardOnlyAffectsTarget", func(t *testing.T) {
		h := newServer()
		zgOffOK(t, h, "dan")
		// gx and every other user keep working
		zgMust(t, h, "gus", "GET", "/projects/gx/tasks", "", 200)
		zgMust(t, h, "ben", "PATCH", "/projects/web/tasks/t1", `{"status":"doing"}`, 200)
		zgMust(t, h, "cat", "GET", "/projects/ops/tasks", "", 200)
		zgMust(t, h, "eve", "GET", "/projects/legacy/tasks", "", 200)
	})
	t.Run("FUNC_SequentialOffboardDanThenHalThenIvy", func(t *testing.T) {
		h := newServer()
		for _, u := range []string{"dan", "hal", "ivy"} {
			zgOffOK(t, h, u)
		}
	})
	t.Run("FUNC_ResponseIs2xxWithoutLeak", func(t *testing.T) {
		h := newServer()
		code, body := zgOff(h, "eve", "acme", "dan")
		if !zg2xx(code) {
			t.Fatalf("-> %d body=%s", code, body)
		}
		if l := zgLeaks("eve", body); len(l) > 0 {
			t.Errorf("leak: %v", l)
		}
	})

	// ---------- ATOM: assignee invariant ----------
	// Accept either rejection (non-2xx, nothing changed) or success where the user lost every
	// membership and tasks assigned to them were unassigned (nothing else changed). Leaving a
	// non-member as assignee of any task (including the archived project's t5) is the failure.
	atom := func(name string, setup func(h http.Handler), uids ...string) {
		t.Run("ATOM_"+name, func(t *testing.T) {
			h := newServer()
			if setup != nil {
				setup(h)
			}
			for _, uid := range uids {
				before := zgSnap(t, h)
				code, body := zgOff(h, "eve", "acme", uid)
				after := zgSnap(t, h)
				if zg2xx(code) {
					for _, p := range zgCheckOff(before, after, uid) {
						t.Errorf("%s (DELETE %s -> %d)", p, uid, code)
					}
				} else if d := zgDiff(before, after); d != "" {
					t.Errorf("state changed despite %d for %s: %s body=%s", code, uid, d, body)
				}
			}
		})
	}
	atom("OffboardBen_webAndArchivedLegacyTasks", nil, "ben")
	atom("OffboardCat_webAndOpsTasks", nil, "cat")
	atom("OffboardBenThenCat", nil, "ben", "cat")
	atom("OffboardFreshAssigneeIvy", func(h http.Handler) {
		zgDo(h, "ann", "PATCH", "/projects/web/tasks/t3", `{"assignee":"ivy"}`)
		zgDo(h, "ivy", "POST", "/projects/ops/tasks", `{"title":"Ivy ops","assignee":"ivy"}`)
	}, "ivy")
	atom("OffboardFreshAssigneeAnn", func(h http.Handler) {
		zgDo(h, "ann", "POST", "/projects/web/tasks", `{"title":"Ann web","assignee":"ann"}`)
	}, "ann")
	t.Run("ATOM_BenRejectedMeansStillFullMember", func(t *testing.T) {
		h := newServer()
		code, _ := zgOff(h, "eve", "acme", "ben")
		if zg2xx(code) {
			return // covered by ATOM_OffboardBen_*
		}
		sig := zgSig(h)
		if sig["web/ben"] != "403/400" || sig["legacy/ben"] != "409/409" {
			t.Errorf("offboarding rejected (%d) but ben's memberships changed: web=%s legacy=%s", code, sig["web/ben"], sig["legacy/ben"])
		}
	})
	t.Run("ATOM_OffboardBenThenBenCannotKeepWorking", func(t *testing.T) {
		h := newServer()
		code, _ := zgOff(h, "eve", "acme", "ben")
		if !zg2xx(code) {
			return
		}
		if c, b := zgDo(h, "ben", "PATCH", "/projects/web/tasks/t1", `{"status":"done"}`); zg2xx(c) {
			t.Errorf("offboarded ben can still modify his old task: %d %s", c, b)
		}
		if c, b := zgDo(h, "ben", "GET", "/projects/legacy/tasks", ""); c == 200 {
			t.Errorf("offboarded ben can still read legacy: %s", b)
		}
	})

	// ---------- AUTHZ ----------
	for _, c := range []struct {
		n string
		r zgReq
	}{
		{"ManagerAnnOffboardsDan", zgReq{"ann", "acme", "dan"}},
		{"ManagerIvyOffboardsDan", zgReq{"ivy", "acme", "dan"}},
		{"ManagerIvyOffboardsHal", zgReq{"ivy", "acme", "hal"}},
		{"ManagerIvyOffboardsBen", zgReq{"ivy", "acme", "ben"}},
		{"ManagerAnnSelf", zgReq{"ann", "acme", "ann"}},
		{"ManagerIvySelf", zgReq{"ivy", "acme", "ivy"}},
		{"ContributorBenOffboardsCat", zgReq{"ben", "acme", "cat"}},
		{"ContributorBenSelf", zgReq{"ben", "acme", "ben"}},
		{"ContributorCatOffboardsDan", zgReq{"cat", "acme", "dan"}},
		{"ViewerDanSelf", zgReq{"dan", "acme", "dan"}},
		{"ViewerDanOffboardsBen", zgReq{"dan", "acme", "ben"}},
		{"NoMembershipHalOffboardsDan", zgReq{"hal", "acme", "dan"}},
		{"NoMembershipHalSelf", zgReq{"hal", "acme", "hal"}},
		{"CrossOrgGusOffboardsBen", zgReq{"gus", "acme", "ben"}},
		{"CrossOrgGusOffboardsDan", zgReq{"gus", "acme", "dan"}},
		{"CrossOrgGusSelfViaAcme", zgReq{"gus", "acme", "gus"}},
		{"CrossOrgGusSelfViaGlobex", zgReq{"gus", "globex", "gus"}},
		{"CrossOrgGusOffboardsAnnViaGlobex", zgReq{"gus", "globex", "ann"}},
		{"CrossOrgGusOffboardsBenViaGlobex", zgReq{"gus", "globex", "ben"}},
		{"CrossOrgEveOffboardsGusViaAcme", zgReq{"eve", "acme", "gus"}},
		{"CrossOrgEveOffboardsGusViaGlobex", zgReq{"eve", "globex", "gus"}},
		{"CrossOrgEveOffboardsBenViaGlobex", zgReq{"eve", "globex", "ben"}},
		{"CrossOrgEveOffboardsDanViaGlobex", zgReq{"eve", "globex", "dan"}},
		{"EveUnknownOrg", zgReq{"eve", "nope", "ben"}},
		{"EveUnknownTargetZed", zgReq{"eve", "acme", "zed"}},
		{"EveTargetCaseMismatch", zgReq{"eve", "acme", "BEN"}},
		{"EveOrgCaseMismatch", zgReq{"eve", "ACME", "dan"}},
		{"ManagerAnnUnknownOrg", zgReq{"ann", "globex", "dan"}},
		{"Anonymous", zgReq{"", "acme", "dan"}},
		{"UnknownUser", zgReq{"zed", "acme", "dan"}},
		{"AnonymousUnknownOrg", zgReq{"", "nope", "dan"}},
	} {
		c := c
		t.Run("AUTHZ_"+c.n, func(t *testing.T) { zgDenied(t, c.r, 0) })
	}
	t.Run("AUTHZ_CrossOrgGusKeepsGxAccess", func(t *testing.T) {
		h := newServer()
		for _, r := range []zgReq{{"eve", "acme", "gus"}, {"eve", "globex", "gus"}, {"gus", "globex", "gus"}, {"gus", "acme", "gus"}} {
			zgOff(h, r.user, r.org, r.uid)
		}
		zgMust(t, h, "gus", "GET", "/projects/gx/tasks", "", 200)
		if s := zgSig(h)["gx/gus"]; s != "400/400" {
			t.Errorf("gus's gx role changed: probe %s", s)
		}
		if x := zgTasks(t, h)["t6"]; x != zgSeed["t6"] {
			t.Errorf("t6 changed: %+v", x)
		}
	})
	t.Run("AUTHZ_DeniedNoLeakBodies", func(t *testing.T) {
		h := newServer()
		for _, r := range []zgReq{{"hal", "acme", "dan"}, {"gus", "acme", "ben"}, {"", "acme", "dan"}, {"ann", "acme", "dan"}, {"eve", "globex", "gus"}, {"gus", "globex", "ann"}} {
			_, body := zgOff(h, r.user, r.org, r.uid)
			if l := zgLeaks(r.user, body); len(l) > 0 {
				t.Errorf("leak: %v in %s", l, body)
			}
		}
	})
	t.Run("AUTHZ_DeniedAttemptsDoNotAccumulate", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		for _, r := range []zgReq{{"ann", "acme", "dan"}, {"ivy", "acme", "ben"}, {"ben", "acme", "ben"}, {"hal", "acme", "cat"}, {"gus", "acme", "dan"}, {"", "acme", "dan"}, {"eve", "globex", "dan"}} {
			if c, b := zgOff(h, r.user, r.org, r.uid); zg2xx(c) {
				t.Errorf("unexpected acceptance: %+v -> %d %s", r, c, b)
			}
		}
		if d := zgDiff(before, zgSnap(t, h)); d != "" {
			t.Errorf("state changed: %s", d)
		}
	})

	// ---------- VALID ----------
	for _, c := range []struct{ n, path string }{
		{"MissingUID", "/orgs/acme/users/"},
		{"ExtraSegment", "/orgs/acme/users/dan/projects"},
		{"EncodedSlashUID", "/orgs/acme/users/dan%2Fben"},
		{"SpaceUID", "/orgs/acme/users/%20dan"},
		{"DotDotUID", "/orgs/acme/users/.."},
		{"UpperUID", "/orgs/acme/users/DAN"},
		{"EmptyOrg", "/orgs//users/dan"},
		{"WrongCollection", "/orgs/acme/members/dan"},
	} {
		c := c
		t.Run("VALID_"+c.n, func(t *testing.T) {
			h := newServer()
			before := zgSnap(t, h)
			code, body := zgDo(h, "eve", "DELETE", c.path, "")
			if zg2xx(code) {
				t.Errorf("unexpected acceptance: DELETE %s -> %d body=%s", c.path, code, body)
			}
			if d := zgDiff(before, zgSnap(t, h)); d != "" {
				t.Errorf("state changed: %s", d)
			}
		})
	}
	t.Run("VALID_OtherMethodsDoNotOffboard", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		for _, m := range []string{"GET", "POST", "PUT", "PATCH"} {
			zgDo(h, "eve", m, "/orgs/acme/users/dan", `{"x":1}`)
		}
		if d := zgDiff(before, zgSnap(t, h)); d != "" {
			t.Errorf("non-DELETE method changed state: %s", d)
		}
	})
	t.Run("FUNC_BodyIgnored", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		code, body := zgDo(h, "eve", "DELETE", "/orgs/acme/users/dan", `{"uid":"cat","org":"globex"}`)
		if !zg2xx(code) {
			t.Fatalf("-> %d body=%s", code, body)
		}
		after := zgSnap(t, h)
		for _, p := range zgCheckOff(before, after, "dan") {
			t.Errorf("%s", p)
		}
	})
	t.Run("FUNC_SecondOffboardNoCollateral", func(t *testing.T) {
		h := newServer()
		zgOffOK(t, h, "dan")
		before := zgSnap(t, h)
		zgOff(h, "eve", "acme", "dan")
		if d := zgDiff(before, zgSnap(t, h)); d != "" {
			t.Errorf("repeated offboarding changed state: %s", d)
		}
	})

	// ---------- CONTRACT ----------
	for _, c := range []struct {
		n    string
		r    zgReq
		want int
	}{
		{"Manager403", zgReq{"ann", "acme", "dan"}, 403},
		{"ManagerIvy403", zgReq{"ivy", "acme", "dan"}, 403},
		{"Contributor403", zgReq{"ben", "acme", "dan"}, 403},
		{"Viewer403", zgReq{"dan", "acme", "dan"}, 403},
		{"NoMembership403", zgReq{"hal", "acme", "dan"}, 403},
		{"AdminWrongOrg404", zgReq{"eve", "globex", "gus"}, 404},
		{"AdminWrongOrgAcmeUser404", zgReq{"eve", "globex", "ben"}, 404},
		{"AdminUnknownOrg404", zgReq{"eve", "nope", "ben"}, 404},
		{"AdminOtherOrgTarget404", zgReq{"eve", "acme", "gus"}, 404},
		{"AdminUnknownTarget404", zgReq{"eve", "acme", "zed"}, 404},
		{"Anonymous401", zgReq{"", "acme", "dan"}, 401},
		{"UnknownUser401", zgReq{"zed", "acme", "dan"}, 401},
	} {
		c := c
		t.Run("CONTRACT_"+c.n, func(t *testing.T) {
			h := newServer()
			code, body := zgOff(h, c.r.user, c.r.org, c.r.uid)
			if code != c.want {
				t.Errorf("DELETE /orgs/%s/users/%s as %q -> %d, want %d body=%s", c.r.org, c.r.uid, c.r.user, code, c.want, body)
			}
		})
	}
	t.Run("CONTRACT_Success204", func(t *testing.T) {
		h := newServer()
		code, body := zgOff(h, "eve", "acme", "dan")
		if code != 204 {
			t.Errorf("-> %d, want 204 body=%s", code, body)
		}
		if code == 204 && body != "" {
			t.Errorf("204 with body %q", body)
		}
	})
	t.Run("CONTRACT_Success204Hal", func(t *testing.T) {
		code, body := zgOff(newServer(), "eve", "acme", "hal")
		if code != 204 {
			t.Errorf("-> %d, want 204 body=%s", code, body)
		}
	})
	t.Run("CONTRACT_ErrorShape", func(t *testing.T) {
		h := newServer()
		code, body := zgOff(h, "ann", "acme", "dan")
		var e map[string]string
		if code != 403 || json.Unmarshal([]byte(body), &e) != nil || e["error"] == "" {
			t.Errorf("-> %d body=%s, want 403 {\"error\":...}", code, body)
		}
	})
	t.Run("CONTRACT_NotFoundErrorShape", func(t *testing.T) {
		h := newServer()
		code, body := zgOff(h, "eve", "acme", "gus")
		var e map[string]string
		if code != 404 || json.Unmarshal([]byte(body), &e) != nil || e["error"] == "" {
			t.Errorf("-> %d body=%s, want 404 {\"error\":...}", code, body)
		}
	})
	t.Run("CONTRACT_AssigneeRejectionStatus", func(t *testing.T) {
		// only meaningful if the implementation rejects offboarding of an assignee
		h := newServer()
		code, body := zgOff(h, "eve", "acme", "ben")
		if zg2xx(code) {
			return
		}
		if code != 409 && code != 400 {
			t.Errorf("rejected offboarding of assignee ben with %d (want 409 or 400) body=%s", code, body)
		}
	})

	// ---------- INFO ----------
	t.Run("INFO_OffboardBen", func(t *testing.T) {
		h := newServer()
		code, body := zgOff(h, "eve", "acme", "ben")
		tt := zgTasks(t, h)
		sig := zgSig(h)
		t.Logf("INFO eve offboards ben (assigned t1 web, t5 archived legacy): -> %d body=%q; t1=%+v t5=%+v; probes web=%s legacy=%s",
			code, body, tt["t1"], tt["t5"], sig["web/ben"], sig["legacy/ben"])
		if zg2xx(code) && tt["t5"].Assignee == "" {
			t.Logf("INFO archived legacy task t5 was unassigned (accepted, archived project changed)")
		}
	})
	t.Run("INFO_OffboardCat", func(t *testing.T) {
		h := newServer()
		code, body := zgOff(h, "eve", "acme", "cat")
		tt := zgTasks(t, h)
		t.Logf("INFO eve offboards cat (t2 web, t4 ops): -> %d body=%q; t2=%+v t4=%+v", code, body, tt["t2"], tt["t4"])
	})
	t.Run("INFO_AdminOffboardsSelf", func(t *testing.T) {
		h := newServer()
		code, body := zgOff(h, "eve", "acme", "eve")
		c2, _ := zgDo(h, "eve", "GET", "/projects/web/tasks", "")
		t.Logf("INFO eve offboards herself -> %d body=%q; eve GET web afterwards -> %d", code, body, c2)
	})
	t.Run("INFO_AnotherOrgAdmin", func(t *testing.T) {
		// seed has a single org admin and no HTTP way to create another; nothing to exercise.
		t.Logf("INFO no second org admin is constructible through HTTP in the seed; offboarding another admin is not testable")
	})
	t.Run("INFO_UserRecordAfterOffboard", func(t *testing.T) {
		h := newServer()
		zgOff(h, "eve", "acme", "dan")
		c1, _ := zgDo(h, "dan", "GET", "/projects/web/tasks", "")
		c2, _ := zgDo(h, "dan", "GET", "/projects/gx/tasks", "")
		t.Logf("INFO after offboarding dan: dan GET web -> %d (404 = record kept, 401 = record deleted), dan GET gx -> %d", c1, c2)
	})
	t.Run("INFO_SecondOffboard", func(t *testing.T) {
		h := newServer()
		c1, _ := zgOff(h, "eve", "acme", "dan")
		c2, _ := zgOff(h, "eve", "acme", "dan")
		t.Logf("INFO offboard dan twice -> %d then %d", c1, c2)
	})
	t.Run("INFO_OffboardedDanAsCaller", func(t *testing.T) {
		h := newServer()
		zgOff(h, "eve", "acme", "dan")
		c, _ := zgOff(h, "dan", "acme", "hal")
		t.Logf("INFO offboarded dan tries to offboard hal -> %d", c)
	})
}
