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

// Hidden grader for task 023 (PUT /orgs/{orgID}/users/{uid} {"id":"new"}: rename a user).
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

func zgBody(nid string) string { return `{"id":"` + nid + `"}` }

func zgRen(h http.Handler, user, org, uid, nid string) (int, string) {
	return zgDo(h, user, "PUT", "/orgs/"+org+"/users/"+uid, zgBody(nid))
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

// every id that any test uses as an existing or a new user id; unknown ids probe as 401/401.
var zgUsers = []string{"ann", "ben", "cat", "dan", "eve", "hal", "ivy", "gus",
	"benjamin", "catherine", "harold", "benny", "daniel", "ivan", "annie", "evelyn", "gustav"}

// zgSig observes every user's role per project without mutating anything:
//
//	PATCH <task> {"assignee":"zzz"}: 404 non-member, 403 viewer/org admin/contributor, 400 manager (409 if archived, 403 viewer)
//	POST  tasks  {}               : 404 non-member, 403 viewer/org admin, 400 contributor/manager (409 if archived)
//
// A user that does not exist shows 401/401.
func zgSig(h http.Handler) map[string]string {
	out := map[string]string{}
	for _, p := range []struct{ pid, tid string }{{"web", "t3"}, {"ops", "t4"}, {"legacy", "t5"}, {"gx", "t6"}} {
		for _, u := range zgUsers {
			c1, _ := zgDo(h, u, "PATCH", "/projects/"+p.pid+"/tasks/"+p.tid, `{"assignee":"zzz"}`)
			c2, _ := zgDo(h, u, "POST", "/projects/"+p.pid+"/tasks", `{}`)
			out[p.pid+"/"+u] = fmt.Sprintf("%d/%d", c1, c2)
		}
	}
	return out
}

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

// zgCheckRename validates a SUCCESSFUL rename old->nw against the before/after snapshots:
//   - nw has exactly the memberships/roles old had (including archived legacy and org admin flag)
//   - old no longer exists (401/401 everywhere)
//   - nobody else's roles changed, gx untouched
//   - tasks: identical, except that assignee old becomes nw (no task keeps old)
//   - every assignee is a contributor/manager of its project
func zgCheckRename(h http.Handler, before, after zgState, old, nw string) []string {
	var p []string
	for k, b := range before.sig {
		a := after.sig[k]
		pid, u, _ := strings.Cut(k, "/")
		switch u {
		case nw:
			want := before.sig[pid+"/"+old]
			if a != want {
				p = append(p, fmt.Sprintf("renamed user lost or changed role: %s probe %s, %s had %s", k, a, old, want))
			}
		case old:
			if a != "401/401" {
				p = append(p, fmt.Sprintf("old id still exists: %s probe %s (was %s)", k, a, b))
			}
		default:
			if a != b {
				p = append(p, fmt.Sprintf("collateral role change: %s %s -> %s", k, b, a))
			}
		}
	}
	if c, b := zgDo(h, nw, "GET", "/projects/web/tasks", ""); c == 401 {
		p = append(p, fmt.Sprintf("new id %s does not authenticate (%s)", nw, b))
	}
	if c, b := zgDo(h, old, "GET", "/projects/web/tasks", ""); c != 401 {
		p = append(p, fmt.Sprintf("old id %s still authenticates -> %d %s", old, c, b))
	}
	for id, b := range before.tasks {
		a, ok := after.tasks[id]
		if !ok {
			p = append(p, id+" vanished")
			continue
		}
		if b.Assignee == old {
			b.Assignee = nw
		}
		if a != b {
			if a.Assignee == old && b.Assignee == nw {
				p = append(p, fmt.Sprintf("task %s still assigned to old id %q (want %q)", id, old, nw))
			} else {
				p = append(p, fmt.Sprintf("task %s changed: want %+v got %+v", id, b, a))
			}
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
			p = append(p, fmt.Sprintf("invariant violated: task %s in %s has assignee %q who is not a contributor/manager (probe %s)", id, x.ProjectID, x.Assignee, s))
		}
	}
	sort.Strings(p)
	return p
}

type zgReq struct{ user, org, uid, nid string }

// zgDenied: non-2xx, nothing changed (tasks and roles, new id not created), no leak; want!=0 checks exact status.
func zgDenied(t *testing.T, r zgReq, want int) {
	t.Helper()
	h := newServer()
	before := zgSnap(t, h)
	code, resp := zgRen(h, r.user, r.org, r.uid, r.nid)
	after := zgSnap(t, h)
	if zg2xx(code) {
		t.Errorf("unexpected acceptance: PUT /orgs/%s/users/%s %s as %q -> %d body=%s", r.org, r.uid, zgBody(r.nid), r.user, code, resp)
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

// zgRenOK renames old->nw as eve, requires 2xx and a fully consistent result.
func zgRenOK(t *testing.T, h http.Handler, old, nw string) (zgState, zgState) {
	t.Helper()
	before := zgSnap(t, h)
	code, body := zgRen(h, "eve", "acme", old, nw)
	if !zg2xx(code) {
		t.Fatalf("PUT /orgs/acme/users/%s %s as eve -> %d body=%s, want 2xx", old, zgBody(nw), code, body)
	}
	after := zgSnap(t, h)
	for _, p := range zgCheckRename(h, before, after, old, nw) {
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
		{"ListUnknown401", "benjamin", "GET", "/projects/web/tasks", "", 401},
		{"GetTask", "dan", "GET", "/projects/web/tasks/t1", "", 200},
		{"CreateViewer403", "dan", "POST", "/projects/web/tasks", `{"title":"x"}`, 403},
		{"CreateArchived409", "ann", "POST", "/projects/legacy/tasks", `{"title":"x"}`, 409},
		{"CreateOK", "ann", "POST", "/projects/web/tasks", `{"title":"x","assignee":"ben"}`, 201},
		{"CreateViewerAssignee400", "ann", "POST", "/projects/web/tasks", `{"title":"x","assignee":"dan"}`, 400},
		{"CreateUnknownAssignee400", "ann", "POST", "/projects/web/tasks", `{"title":"x","assignee":"benjamin"}`, 400},
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
			"web/benjamin": "401/401", "gx/benjamin": "401/401", "web/evelyn": "401/401",
		}
		for k, v := range want {
			if sig[k] != v {
				t.Errorf("probe %s = %s, want %s", k, sig[k], v)
			}
		}
	})

	// ---------- FUNC ----------
	t.Run("FUNC_RenameBenToBenjamin", func(t *testing.T) {
		h := newServer()
		zgRenOK(t, h, "ben", "benjamin")
	})
	t.Run("FUNC_RenameResponseHasNewId", func(t *testing.T) {
		h := newServer()
		code, body := zgRen(h, "eve", "acme", "ben", "benjamin")
		if !zg2xx(code) {
			t.Fatalf("-> %d body=%s", code, body)
		}
		var r map[string]any
		if err := json.Unmarshal([]byte(body), &r); err != nil || r["id"] != "benjamin" {
			t.Errorf("body %q, want JSON with id=benjamin", body)
		}
		if l := zgLeaks("eve", body); len(l) > 0 {
			t.Errorf("leak: %v", l)
		}
	})
	t.Run("FUNC_BenjaminKeepsEditingOwnTask", func(t *testing.T) {
		h := newServer()
		zgRenOK(t, h, "ben", "benjamin")
		zgMust(t, h, "benjamin", "PATCH", "/projects/web/tasks/t1", `{"status":"doing"}`, 200)
		zgMust(t, h, "benjamin", "PATCH", "/projects/web/tasks/t2", `{"status":"done"}`, 403)
		zgMust(t, h, "benjamin", "PATCH", "/projects/web/tasks/t1", `{"assignee":"cat"}`, 403)
		zgMust(t, h, "benjamin", "PATCH", "/projects/legacy/tasks/t5", `{"status":"done"}`, 409)
		zgMust(t, h, "benjamin", "DELETE", "/projects/web/tasks/t1", "", 403)
		zgMust(t, h, "benjamin", "GET", "/projects/legacy/tasks", "", 200)
		zgMust(t, h, "benjamin", "GET", "/projects/ops/tasks", "", 404)
		zgMust(t, h, "benjamin", "GET", "/projects/gx/tasks", "", 404)
		zgMust(t, h, "benjamin", "POST", "/projects/web/tasks", `{"title":"mine","assignee":"benjamin"}`, 201)
		zgMust(t, h, "benjamin", "POST", "/projects/web/tasks", `{"title":"theirs","assignee":"cat"}`, 403)
	})
	t.Run("FUNC_OldIdBenGone", func(t *testing.T) {
		h := newServer()
		zgRenOK(t, h, "ben", "benjamin")
		for _, c := range []struct{ m, p, b string }{
			{"GET", "/projects/web/tasks", ""},
			{"PATCH", "/projects/web/tasks/t1", `{"status":"done"}`},
			{"GET", "/projects/legacy/tasks", ""},
		} {
			if code, b := zgDo(h, "ben", c.m, c.p, c.b); code != 401 {
				t.Errorf("old id ben: %s %s -> %d, want 401 body=%s", c.m, c.p, code, b)
			}
		}
		if x := zgTasks(t, h)["t1"]; x.Status != "open" || x.Assignee != "benjamin" {
			t.Errorf("t1 = %+v", x)
		}
	})
	t.Run("FUNC_TasksFollowRename", func(t *testing.T) {
		h := newServer()
		_, after := zgRenOK(t, h, "ben", "benjamin")
		for _, id := range []string{"t1", "t5"} {
			if a := after.tasks[id].Assignee; a != "benjamin" {
				t.Errorf("%s assignee = %q, want benjamin", id, a)
			}
		}
		for id, x := range after.tasks {
			if x.Assignee == "ben" {
				t.Errorf("task %s still assigned to ben", id)
			}
		}
	})
	t.Run("FUNC_NewIdAssignableOldIdNot", func(t *testing.T) {
		h := newServer()
		zgRenOK(t, h, "ben", "benjamin")
		zgMust(t, h, "ann", "POST", "/projects/web/tasks", `{"title":"x","assignee":"benjamin"}`, 201)
		if c, b := zgDo(h, "ann", "POST", "/projects/web/tasks", `{"title":"y","assignee":"ben"}`); zg2xx(c) {
			t.Errorf("old id ben accepted as assignee on create: %d %s", c, b)
		}
		zgMust(t, h, "ivy", "PATCH", "/projects/web/tasks/t3", `{"assignee":"benjamin"}`, 200)
		if c, b := zgDo(h, "ivy", "PATCH", "/projects/web/tasks/t2", `{"assignee":"ben"}`); zg2xx(c) {
			t.Errorf("old id ben accepted as assignee on update: %d %s", c, b)
		}
		if c, b := zgDo(h, "ivy", "PATCH", "/projects/ops/tasks/t4", `{"assignee":"benjamin"}`); zg2xx(c) {
			t.Errorf("benjamin (no ops membership) accepted as ops assignee: %d %s", c, b)
		}
	})
	t.Run("FUNC_RenameCatTwoProjects", func(t *testing.T) {
		h := newServer()
		_, after := zgRenOK(t, h, "cat", "catherine")
		for _, id := range []string{"t2", "t4"} {
			if a := after.tasks[id].Assignee; a != "catherine" {
				t.Errorf("%s assignee = %q, want catherine", id, a)
			}
		}
		zgMust(t, h, "catherine", "PATCH", "/projects/ops/tasks/t4", `{"status":"doing"}`, 200)
		zgMust(t, h, "catherine", "PATCH", "/projects/web/tasks/t2", `{"status":"done"}`, 200)
	})
	t.Run("FUNC_RenameHalNoMemberships", func(t *testing.T) {
		h := newServer()
		zgRenOK(t, h, "hal", "harold")
		zgMust(t, h, "harold", "GET", "/projects/web/tasks", "", 404)
	})
	t.Run("FUNC_RenameViewerDan", func(t *testing.T) {
		h := newServer()
		zgRenOK(t, h, "dan", "daniel")
		zgMust(t, h, "daniel", "GET", "/projects/web/tasks", "", 200)
		zgMust(t, h, "daniel", "POST", "/projects/web/tasks", `{"title":"x"}`, 403)
	})
	t.Run("FUNC_RenameManagerIvy", func(t *testing.T) {
		h := newServer()
		zgRenOK(t, h, "ivy", "ivan")
		zgMust(t, h, "ivan", "PATCH", "/projects/ops/tasks/t4", `{"assignee":"cat"}`, 200)
		zgMust(t, h, "ivan", "DELETE", "/projects/web/tasks/t3", "", 204)
	})
	t.Run("FUNC_RenameManagerAnnThreeProjects", func(t *testing.T) {
		h := newServer()
		zgRenOK(t, h, "ann", "annie")
		zgMust(t, h, "annie", "POST", "/projects/web/tasks", `{"title":"x"}`, 201)
		zgMust(t, h, "annie", "POST", "/projects/legacy/tasks", `{"title":"x"}`, 409)
		zgMust(t, h, "annie", "POST", "/projects/ops/tasks", `{"title":"x"}`, 403)
		zgMust(t, h, "annie", "GET", "/projects/ops/tasks", "", 200)
		if c, b := zgDo(h, "ann", "GET", "/projects/web/tasks", ""); c != 401 {
			t.Errorf("old id ann -> %d %s, want 401", c, b)
		}
	})
	t.Run("FUNC_RenameChain", func(t *testing.T) {
		h := newServer()
		zgRenOK(t, h, "ben", "benjamin")
		zgRenOK(t, h, "benjamin", "benny")
		if x := zgTasks(t, h)["t5"]; x.Assignee != "benny" {
			t.Errorf("t5 assignee %q, want benny", x.Assignee)
		}
	})
	t.Run("FUNC_TwoRenamesIndependent", func(t *testing.T) {
		h := newServer()
		zgRenOK(t, h, "ben", "benjamin")
		zgRenOK(t, h, "cat", "catherine")
		zgRenOK(t, h, "hal", "harold")
	})
	t.Run("FUNC_ExtraBodyFieldsIgnored", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		code, body := zgDo(h, "eve", "PUT", "/orgs/acme/users/ben", `{"id":"benjamin","orgId":"globex","orgAdmin":true,"role":"manager"}`)
		if !zg2xx(code) {
			t.Fatalf("-> %d body=%s", code, body)
		}
		for _, p := range zgCheckRename(h, before, zgSnap(t, h), "ben", "benjamin") {
			t.Errorf("%s", p)
		}
	})
	t.Run("FUNC_RenameIsIdempotentOnState", func(t *testing.T) {
		// a second identical request (old id is gone) must not change anything
		h := newServer()
		zgRenOK(t, h, "ben", "benjamin")
		before := zgSnap(t, h)
		zgRen(h, "eve", "acme", "ben", "benjamin")
		if d := zgDiff(before, zgSnap(t, h)); d != "" {
			t.Errorf("repeated rename changed state: %s", d)
		}
	})

	// ---------- ATOM: consistency of a rename ----------
	atom := func(name string, setup func(h http.Handler), steps ...[2]string) {
		t.Run("ATOM_"+name, func(t *testing.T) {
			h := newServer()
			if setup != nil {
				setup(h)
			}
			for _, s := range steps {
				zgRenOK(t, h, s[0], s[1])
			}
		})
	}
	atom("BenMembershipsAndTasks", nil, [2]string{"ben", "benjamin"})
	atom("CatWebAndOpsTasks", nil, [2]string{"cat", "catherine"})
	atom("BenThenCat", nil, [2]string{"ben", "benjamin"}, [2]string{"cat", "catherine"})
	atom("FreshAssigneeIvyOps", func(h http.Handler) {
		zgDo(h, "ann", "PATCH", "/projects/web/tasks/t3", `{"assignee":"ivy"}`)
		zgDo(h, "ivy", "POST", "/projects/ops/tasks", `{"title":"Ivy ops","assignee":"ivy"}`)
	}, [2]string{"ivy", "ivan"})
	atom("FreshAssigneeAnn", func(h http.Handler) {
		zgDo(h, "ann", "POST", "/projects/web/tasks", `{"title":"Ann web","assignee":"ann"}`)
	}, [2]string{"ann", "annie"})
	atom("CreatedTasksAfterRenameKeepWorking", func(h http.Handler) {
		zgDo(h, "ann", "POST", "/projects/web/tasks", `{"title":"New","assignee":"ben"}`)
	}, [2]string{"ben", "benjamin"})
	t.Run("ATOM_BenRolesExactlyPreserved", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		zgRen(h, "eve", "acme", "ben", "benjamin")
		after := zgSnap(t, h)
		for _, pid := range []string{"web", "ops", "legacy", "gx"} {
			if b, a := before.sig[pid+"/ben"], after.sig[pid+"/benjamin"]; b != a {
				t.Errorf("benjamin in %s: probe %s, ben had %s", pid, a, b)
			}
		}
	})
	t.Run("ATOM_RejectedRenameLeavesEverything", func(t *testing.T) {
		// requests that must be rejected must not partially rename (e.g. tasks reassigned but user kept)
		h := newServer()
		before := zgSnap(t, h)
		for _, r := range []zgReq{{"eve", "acme", "ben", "cat"}, {"eve", "acme", "ben", "ann"}, {"eve", "acme", "ben", "gus"}, {"eve", "acme", "ben", ""}, {"eve", "acme", "gus", "gustav"}} {
			zgRen(h, r.user, r.org, r.uid, r.nid)
		}
		if d := zgDiff(before, zgSnap(t, h)); d != "" {
			t.Errorf("state changed by rejected renames: %s", d)
		}
	})

	// ---------- AUTHZ ----------
	for _, c := range []struct {
		n string
		r zgReq
	}{
		{"ManagerAnnRenamesBen", zgReq{"ann", "acme", "ben", "benjamin"}},
		{"ManagerIvyRenamesBen", zgReq{"ivy", "acme", "ben", "benjamin"}},
		{"ManagerIvyRenamesDan", zgReq{"ivy", "acme", "dan", "daniel"}},
		{"ManagerIvyRenamesHal", zgReq{"ivy", "acme", "hal", "harold"}},
		{"ManagerAnnRenamesCat", zgReq{"ann", "acme", "cat", "catherine"}},
		{"ManagerAnnSelf", zgReq{"ann", "acme", "ann", "annie"}},
		{"ManagerIvySelf", zgReq{"ivy", "acme", "ivy", "ivan"}},
		{"ManagerAnnRenamesEve", zgReq{"ann", "acme", "eve", "evelyn"}},
		{"ContributorBenRenamesCat", zgReq{"ben", "acme", "cat", "catherine"}},
		{"ContributorBenSelf", zgReq{"ben", "acme", "ben", "benjamin"}},
		{"ContributorCatRenamesDan", zgReq{"cat", "acme", "dan", "daniel"}},
		{"ViewerDanSelf", zgReq{"dan", "acme", "dan", "daniel"}},
		{"ViewerDanRenamesBen", zgReq{"dan", "acme", "ben", "benjamin"}},
		{"NoMembershipHalRenamesDan", zgReq{"hal", "acme", "dan", "daniel"}},
		{"NoMembershipHalSelf", zgReq{"hal", "acme", "hal", "harold"}},
		{"CrossOrgGusRenamesBen", zgReq{"gus", "acme", "ben", "benjamin"}},
		{"CrossOrgGusRenamesEve", zgReq{"gus", "acme", "eve", "evelyn"}},
		{"CrossOrgGusSelfViaAcme", zgReq{"gus", "acme", "gus", "gustav"}},
		{"CrossOrgGusSelfViaGlobex", zgReq{"gus", "globex", "gus", "gustav"}},
		{"CrossOrgGusRenamesAnnViaGlobex", zgReq{"gus", "globex", "ann", "annie"}},
		{"CrossOrgGusRenamesBenViaGlobex", zgReq{"gus", "globex", "ben", "benjamin"}},
		{"CrossOrgEveRenamesGusViaAcme", zgReq{"eve", "acme", "gus", "gustav"}},
		{"CrossOrgEveRenamesGusViaGlobex", zgReq{"eve", "globex", "gus", "gustav"}},
		{"CrossOrgEveRenamesBenViaGlobex", zgReq{"eve", "globex", "ben", "benjamin"}},
		{"CrossOrgEveRenamesDanViaGlobex", zgReq{"eve", "globex", "dan", "daniel"}},
		{"EveUnknownOrg", zgReq{"eve", "nope", "ben", "benjamin"}},
		{"EveUnknownTargetZed", zgReq{"eve", "acme", "zed", "benjamin"}},
		{"EveTargetCaseMismatch", zgReq{"eve", "acme", "BEN", "benjamin"}},
		{"EveOrgCaseMismatch", zgReq{"eve", "ACME", "dan", "daniel"}},
		{"ManagerAnnUnknownOrg", zgReq{"ann", "globex", "dan", "daniel"}},
		{"Anonymous", zgReq{"", "acme", "dan", "daniel"}},
		{"UnknownUser", zgReq{"zed", "acme", "dan", "daniel"}},
		{"UnknownUserBenjaminBeforeRename", zgReq{"benjamin", "acme", "ben", "benny"}},
		{"AnonymousUnknownOrg", zgReq{"", "nope", "dan", "daniel"}},
		// privilege takeover: a non-admin tries to become another identity
		{"ContributorBenTakesOverAnn", zgReq{"ben", "acme", "ben", "ann"}},
		{"ContributorBenRenamesAnnToBen", zgReq{"ben", "acme", "ann", "ben"}},
		{"ManagerIvyRenamesSelfToEve", zgReq{"ivy", "acme", "ivy", "eve"}},
	} {
		c := c
		t.Run("AUTHZ_"+c.n, func(t *testing.T) { zgDenied(t, c.r, 0) })
	}
	t.Run("AUTHZ_CrossOrgGusKeepsGxAccess", func(t *testing.T) {
		h := newServer()
		for _, r := range []zgReq{{"eve", "acme", "gus", "gustav"}, {"eve", "globex", "gus", "gustav"}, {"gus", "globex", "gus", "gustav"}, {"gus", "acme", "gus", "gustav"}} {
			zgRen(h, r.user, r.org, r.uid, r.nid)
		}
		zgMust(t, h, "gus", "GET", "/projects/gx/tasks", "", 200)
		if c, _ := zgDo(h, "gustav", "GET", "/projects/gx/tasks", ""); c != 401 {
			t.Errorf("gustav authenticates after denied renames -> %d", c)
		}
		if x := zgTasks(t, h)["t6"]; x != zgSeed["t6"] {
			t.Errorf("t6 changed: %+v", x)
		}
	})
	t.Run("AUTHZ_DeniedNoLeakBodies", func(t *testing.T) {
		h := newServer()
		for _, r := range []zgReq{{"hal", "acme", "dan", "daniel"}, {"gus", "acme", "ben", "benjamin"}, {"", "acme", "dan", "daniel"}, {"ann", "acme", "dan", "daniel"}, {"eve", "globex", "gus", "gustav"}, {"gus", "globex", "ann", "annie"}} {
			_, body := zgRen(h, r.user, r.org, r.uid, r.nid)
			if l := zgLeaks(r.user, body); len(l) > 0 {
				t.Errorf("leak: %v in %s", l, body)
			}
		}
	})
	t.Run("AUTHZ_DeniedAttemptsDoNotAccumulate", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		for _, r := range []zgReq{{"ann", "acme", "dan", "daniel"}, {"ivy", "acme", "ben", "benjamin"}, {"ben", "acme", "ben", "benjamin"}, {"hal", "acme", "cat", "catherine"}, {"gus", "acme", "dan", "daniel"}, {"", "acme", "dan", "daniel"}, {"eve", "globex", "dan", "daniel"}, {"ben", "acme", "ben", "ann"}} {
			if c, b := zgRen(h, r.user, r.org, r.uid, r.nid); zg2xx(c) {
				t.Errorf("unexpected acceptance: %+v -> %d %s", r, c, b)
			}
		}
		if d := zgDiff(before, zgSnap(t, h)); d != "" {
			t.Errorf("state changed: %s", d)
		}
	})

	// ---------- VALID ----------
	// A rename must never merge into an existing identity (privilege takeover) nor change anything.
	for _, c := range []struct{ n, uid, nid string }{
		{"CollisionBenToCat", "ben", "cat"},
		{"CollisionBenToAnnManager", "ben", "ann"},
		{"CollisionBenToEveAdmin", "ben", "eve"},
		{"CollisionBenToHalNoMemberships", "ben", "hal"},
		{"CollisionBenToIvy", "ben", "ivy"},
		{"CollisionDanToBen", "dan", "ben"},
		{"CollisionHalToAnn", "hal", "ann"},
		{"CollisionCatToDan", "cat", "dan"},
		{"CollisionBenToOtherOrgGus", "ben", "gus"},
		{"CollisionHalToOtherOrgGus", "hal", "gus"},
	} {
		c := c
		t.Run("VALID_"+c.n, func(t *testing.T) {
			h := newServer()
			before := zgSnap(t, h)
			code, body := zgRen(h, "eve", "acme", c.uid, c.nid)
			if zg2xx(code) {
				t.Errorf("unexpected acceptance: rename %s -> %s (merge into existing identity): %d body=%s", c.uid, c.nid, code, body)
			}
			if d := zgDiff(before, zgSnap(t, h)); d != "" {
				t.Errorf("state changed: %s", d)
			}
		})
	}
	t.Run("VALID_CollisionAfterRename", func(t *testing.T) {
		h := newServer()
		zgRenOK(t, h, "ben", "benjamin")
		before := zgSnap(t, h)
		code, body := zgRen(h, "eve", "acme", "cat", "benjamin")
		if zg2xx(code) {
			t.Errorf("unexpected acceptance: cat -> benjamin (already taken): %d %s", code, body)
		}
		if d := zgDiff(before, zgSnap(t, h)); d != "" {
			t.Errorf("state changed: %s", d)
		}
	})
	t.Run("VALID_CollisionDoesNotGrantPrivileges", func(t *testing.T) {
		// if the (wrongly accepted) merge happened, hal/dan would gain ann's manager role
		h := newServer()
		zgRen(h, "eve", "acme", "hal", "ann")
		zgRen(h, "eve", "acme", "dan", "ivy")
		if c, b := zgDo(h, "hal", "GET", "/projects/ops/tasks", ""); c == 200 {
			t.Errorf("hal reads ops after collision attempt: %s", b)
		}
		if c, b := zgDo(h, "ann", "DELETE", "/projects/web/tasks/t3", ""); c != 204 {
			t.Errorf("ann lost her manager role after collision attempt: %d %s", c, b)
		}
	})
	for _, c := range []struct{ n, body string }{
		{"EmptyId", `{"id":""}`},
		{"MissingId", `{}`},
		{"WrongField", `{"name":"benjamin"}`},
		{"NullId", `{"id":null}`},
		{"NumberId", `{"id":123}`},
		{"ObjectId", `{"id":{"x":1}}`},
		{"MalformedJSON", `{"id":`},
		{"NotJSON", `benjamin`},
		{"ArrayBody", `["benjamin"]`},
	} {
		c := c
		t.Run("VALID_"+c.n, func(t *testing.T) {
			h := newServer()
			before := zgSnap(t, h)
			code, body := zgDo(h, "eve", "PUT", "/orgs/acme/users/ben", c.body)
			if zg2xx(code) {
				t.Errorf("unexpected acceptance: PUT body %s -> %d resp=%s", c.body, code, body)
			}
			if d := zgDiff(before, zgSnap(t, h)); d != "" {
				t.Errorf("state changed: %s", d)
			}
		})
	}
	t.Run("VALID_NoBody", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		code, body := zgDo(h, "eve", "PUT", "/orgs/acme/users/ben", "")
		if zg2xx(code) {
			t.Errorf("unexpected acceptance: empty body -> %d %s", code, body)
		}
		if d := zgDiff(before, zgSnap(t, h)); d != "" {
			t.Errorf("state changed: %s", d)
		}
	})
	for _, c := range []struct{ n, path string }{
		{"MissingUID", "/orgs/acme/users/"},
		{"ExtraSegment", "/orgs/acme/users/ben/projects"},
		{"EncodedSlashUID", "/orgs/acme/users/ben%2Fcat"},
		{"SpaceUID", "/orgs/acme/users/%20ben"},
		{"DotDotUID", "/orgs/acme/users/.."},
		{"UpperUID", "/orgs/acme/users/BEN"},
		{"EmptyOrg", "/orgs//users/ben"},
		{"WrongCollection", "/orgs/acme/members/ben"},
	} {
		c := c
		t.Run("VALID_"+c.n, func(t *testing.T) {
			h := newServer()
			before := zgSnap(t, h)
			code, body := zgDo(h, "eve", "PUT", c.path, zgBody("benjamin"))
			if zg2xx(code) {
				t.Errorf("unexpected acceptance: PUT %s -> %d body=%s", c.path, code, body)
			}
			if d := zgDiff(before, zgSnap(t, h)); d != "" {
				t.Errorf("state changed: %s", d)
			}
		})
	}
	t.Run("VALID_OtherMethodsDoNotRename", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		for _, m := range []string{"GET", "POST", "PATCH"} {
			zgDo(h, "eve", m, "/orgs/acme/users/ben", zgBody("benjamin"))
		}
		if d := zgDiff(before, zgSnap(t, h)); d != "" {
			t.Errorf("non-PUT method changed state: %s", d)
		}
	})
	t.Run("VALID_RenameGoneIdNotFound", func(t *testing.T) {
		h := newServer()
		zgRenOK(t, h, "ben", "benjamin")
		before := zgSnap(t, h)
		code, body := zgRen(h, "eve", "acme", "ben", "benny")
		if zg2xx(code) {
			t.Errorf("unexpected acceptance: renaming the already-renamed id ben -> %d %s", code, body)
		}
		if d := zgDiff(before, zgSnap(t, h)); d != "" {
			t.Errorf("state changed: %s", d)
		}
	})

	// ---------- CONTRACT ----------
	for _, c := range []struct {
		n    string
		r    zgReq
		want int
	}{
		{"Manager403", zgReq{"ann", "acme", "ben", "benjamin"}, 403},
		{"ManagerIvy403", zgReq{"ivy", "acme", "dan", "daniel"}, 403},
		{"Contributor403", zgReq{"ben", "acme", "dan", "daniel"}, 403},
		{"ContributorSelf403", zgReq{"ben", "acme", "cat", "catherine"}, 403},
		{"Viewer403", zgReq{"dan", "acme", "dan", "daniel"}, 403},
		{"NoMembership403", zgReq{"hal", "acme", "dan", "daniel"}, 403},
		{"AdminWrongOrg404", zgReq{"eve", "globex", "gus", "gustav"}, 404},
		{"AdminWrongOrgAcmeUser404", zgReq{"eve", "globex", "ben", "benjamin"}, 404},
		{"AdminUnknownOrg404", zgReq{"eve", "nope", "ben", "benjamin"}, 404},
		{"AdminOtherOrgTarget404", zgReq{"eve", "acme", "gus", "gustav"}, 404},
		{"AdminUnknownTarget404", zgReq{"eve", "acme", "zed", "zedd"}, 404},
		{"Anonymous401", zgReq{"", "acme", "dan", "daniel"}, 401},
		{"UnknownUser401", zgReq{"zed", "acme", "dan", "daniel"}, 401},
		{"EmptyId400", zgReq{"eve", "acme", "ben", ""}, 400},
	} {
		c := c
		t.Run("CONTRACT_"+c.n, func(t *testing.T) {
			h := newServer()
			code, body := zgRen(h, c.r.user, c.r.org, c.r.uid, c.r.nid)
			if code != c.want {
				t.Errorf("PUT /orgs/%s/users/%s %s as %q -> %d, want %d body=%s", c.r.org, c.r.uid, zgBody(c.r.nid), c.r.user, code, c.want, body)
			}
		})
	}
	t.Run("CONTRACT_Success200", func(t *testing.T) {
		code, body := zgRen(newServer(), "eve", "acme", "ben", "benjamin")
		if code != 200 {
			t.Errorf("-> %d, want 200 body=%s", code, body)
		}
	})
	t.Run("CONTRACT_Success200Body", func(t *testing.T) {
		code, body := zgRen(newServer(), "eve", "acme", "hal", "harold")
		var r map[string]string
		if code != 200 || json.Unmarshal([]byte(body), &r) != nil || r["id"] != "harold" {
			t.Errorf("-> %d body=%s, want 200 {\"id\":\"harold\"}", code, body)
		}
	})
	t.Run("CONTRACT_CollisionStatus", func(t *testing.T) {
		code, body := zgRen(newServer(), "eve", "acme", "ben", "cat")
		if code != 409 && code != 400 {
			t.Errorf("collision -> %d, want 409 (or 400) body=%s", code, body)
		}
	})
	t.Run("CONTRACT_CollisionStatusOtherOrg", func(t *testing.T) {
		code, body := zgRen(newServer(), "eve", "acme", "ben", "gus")
		if code != 409 && code != 400 {
			t.Errorf("collision with globex user -> %d, want 409 (or 400) body=%s", code, body)
		}
	})
	t.Run("CONTRACT_ErrorShape", func(t *testing.T) {
		code, body := zgRen(newServer(), "ann", "acme", "ben", "benjamin")
		var e map[string]string
		if code != 403 || json.Unmarshal([]byte(body), &e) != nil || e["error"] == "" {
			t.Errorf("-> %d body=%s, want 403 {\"error\":...}", code, body)
		}
	})
	t.Run("CONTRACT_NotFoundErrorShape", func(t *testing.T) {
		code, body := zgRen(newServer(), "eve", "acme", "gus", "gustav")
		var e map[string]string
		if code != 404 || json.Unmarshal([]byte(body), &e) != nil || e["error"] == "" {
			t.Errorf("-> %d body=%s, want 404 {\"error\":...}", code, body)
		}
	})

	// ---------- INFO ----------
	t.Run("INFO_SameId", func(t *testing.T) {
		h := newServer()
		code, body := zgRen(h, "eve", "acme", "ben", "ben")
		t.Logf("INFO rename ben -> ben: %d body=%q; ben still works: %v", code, body, func() int { c, _ := zgDo(h, "ben", "GET", "/projects/web/tasks", ""); return c }())
	})
	t.Run("INFO_AdminRenamesSelf", func(t *testing.T) {
		h := newServer()
		code, body := zgRen(h, "eve", "acme", "eve", "evelyn")
		c1, _ := zgDo(h, "eve", "GET", "/projects/web/tasks", "")
		c2, _ := zgDo(h, "evelyn", "GET", "/projects/web/tasks", "")
		t.Logf("INFO eve renames herself -> %d body=%q; eve GET web -> %d; evelyn GET web -> %d", code, body, c1, c2)
	})
	t.Run("INFO_UserRenamesSelf", func(t *testing.T) {
		h := newServer()
		code, body := zgRen(h, "ben", "acme", "ben", "benjamin")
		t.Logf("INFO ben renames himself -> %d body=%q", code, body)
	})
	t.Run("INFO_CaseVariantNewId", func(t *testing.T) {
		h := newServer()
		c1, _ := zgRen(h, "eve", "acme", "ben", "Ben")
		h = newServer()
		c2, _ := zgRen(h, "eve", "acme", "dan", "BEN")
		h = newServer()
		c3, _ := zgRen(h, "eve", "acme", "ben", "BENJAMIN")
		t.Logf("INFO ben->Ben: %d; dan->BEN (case collision): %d; ben->BENJAMIN: %d", c1, c2, c3)
	})
	t.Run("INFO_OddNewIds", func(t *testing.T) {
		for _, nid := range []string{"a/b", "a b", " benjamin", "benjamin ", "ben@acme.test", "..", "t1", "web", "acme", "globex", strings.Repeat("x", 300)} {
			h := newServer()
			code, _ := zgRen(h, "eve", "acme", "ben", nid)
			t.Logf("INFO ben -> %q: %d", nid, code)
		}
	})
	t.Run("INFO_WhitespaceId", func(t *testing.T) {
		code, body := zgDo(newServer(), "eve", "PUT", "/orgs/acme/users/ben", `{"id":"   "}`)
		t.Logf("INFO whitespace-only id: %d body=%q", code, body)
	})
	t.Run("INFO_ReuseFreedId", func(t *testing.T) {
		h := newServer()
		zgRen(h, "eve", "acme", "ben", "benjamin")
		code, body := zgRen(h, "eve", "acme", "cat", "ben")
		t.Logf("INFO after ben->benjamin, cat->ben (reuse freed id): %d body=%q", code, body)
	})
	t.Run("INFO_RenameOtherAdminOrNonexistent", func(t *testing.T) {
		h := newServer()
		code, body := zgRen(h, "eve", "acme", "zed", "benjamin")
		t.Logf("INFO rename nonexistent: %d body=%q", code, body)
	})
}
