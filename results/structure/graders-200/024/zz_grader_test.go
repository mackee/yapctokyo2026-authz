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

// Hidden grader for task 024 (POST /orgs/{orgID}/users/{uid}/transfer {"toOrgId":...}).
// Uses only newServer() and HTTP. Roles are observed through non-mutating probes (see zgSig),
// plus a final mutating assignee probe (zgNotWriter) for retained memberships.

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

func zgTrBody(h http.Handler, user, org, uid, body string) (int, string) {
	return zgDo(h, user, "POST", "/orgs/"+org+"/users/"+uid+"/transfer", body)
}

func zgTr(h http.Handler, user, org, uid, to string) (int, string) {
	return zgTrBody(h, user, org, uid, fmt.Sprintf(`{"toOrgId":%q}`, to))
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
//	PATCH <task> {"assignee":"zzz"}: 404 non-member, 403 viewer/org admin/contributor, 400 manager (409 if archived, 403 viewer)
//	POST  tasks  {}               : 404 non-member, 403 viewer/org admin, 400 contributor/manager (409 if archived)
//
// A user that no longer authenticates shows 401/401.
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

// zgCheckTr validates a SUCCESSFUL transfer of uid (acme -> globex) from before/after snapshots:
//   - uid still authenticates but has no access (404/404) in every project, acme and gx
//   - nobody else's roles changed
//   - tasks unchanged, except tasks assigned to uid may have been unassigned
//   - no task keeps a non-writer as assignee
func zgCheckTr(before, after zgState, uid string) []string {
	var p []string
	for k, b := range before.sig {
		a := after.sig[k]
		_, u, _ := strings.Cut(k, "/")
		if u == uid {
			if a == "401/401" {
				p = append(p, fmt.Sprintf("user vanished (no longer authenticates): %s probe %s", k, a))
			} else if a != "404/404" {
				p = append(p, fmt.Sprintf("access retained or gained: %s probe %s (was %s)", k, a, b))
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
		if b.Assignee == uid {
			b.Assignee = ""
			if a.Assignee != "" {
				p = append(p, fmt.Sprintf("invariant violated: %s still assigned to %q after transferring %s out of the org", id, a.Assignee, uid))
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

// zgNotWriter is a MUTATING probe (call last): uid must not be accepted as an assignee in web, ops or gx.
// Catches retained acme memberships (invisible to reads, since project.org != user.org) and gained gx membership.
func zgNotWriter(t *testing.T, h http.Handler, uid string) {
	t.Helper()
	for _, p := range []struct{ user, pid string }{{"ann", "web"}, {"ivy", "ops"}, {"gus", "gx"}} {
		body := fmt.Sprintf(`{"title":"zg-probe","assignee":%q}`, uid)
		if c, b := zgDo(h, p.user, "POST", "/projects/"+p.pid+"/tasks", body); zg2xx(c) {
			t.Errorf("%s can still be assigned in %s after transfer (membership retained/gained): %d %s", uid, p.pid, c, b)
		}
	}
}

type zgReq struct{ user, org, uid, to string }

// zgDenied: non-2xx, nothing changed (tasks and roles), no leak; want!=0 checks exact status.
func zgDenied(t *testing.T, r zgReq, want int) {
	t.Helper()
	h := newServer()
	before := zgSnap(t, h)
	code, resp := zgTr(h, r.user, r.org, r.uid, r.to)
	after := zgSnap(t, h)
	if zg2xx(code) {
		t.Errorf("unexpected acceptance: POST /orgs/%s/users/%s/transfer to=%s as %q -> %d body=%s", r.org, r.uid, r.to, r.user, code, resp)
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

// zgTrOK transfers uid acme->globex as eve, requires 2xx and a fully consistent result.
func zgTrOK(t *testing.T, h http.Handler, uid string) (zgState, zgState) {
	t.Helper()
	before := zgSnap(t, h)
	code, body := zgTr(h, "eve", "acme", uid, "globex")
	if !zg2xx(code) {
		t.Fatalf("POST /orgs/acme/users/%s/transfer as eve -> %d body=%s, want 2xx", uid, code, body)
	}
	after := zgSnap(t, h)
	for _, p := range zgCheckTr(before, after, uid) {
		t.Errorf("%s", p)
	}
	return before, after
}

func zgNoRead(t *testing.T, h http.Handler, user string, paths ...string) {
	t.Helper()
	for _, p := range paths {
		c, b := zgDo(h, user, "GET", p, "")
		if c == 200 {
			t.Errorf("%s can still GET %s: %s", user, p, b)
		}
		if c == 401 {
			t.Errorf("%s no longer authenticates (GET %s -> 401)", user, p)
		}
		if l := zgLeaks("nobody", b); len(l) > 0 {
			t.Errorf("leak to %s on %s: %v", user, p, l)
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
		{"ListGxOtherOrg404", "ann", "GET", "/projects/gx/tasks", "", 404},
		{"ListAdminReads", "eve", "GET", "/projects/ops/tasks", "", 200},
		{"ListAnon401", "", "GET", "/projects/web/tasks", "", 401},
		{"GetTask", "dan", "GET", "/projects/web/tasks/t1", "", 200},
		{"CreateViewer403", "dan", "POST", "/projects/web/tasks", `{"title":"x"}`, 403},
		{"CreateArchived409", "ann", "POST", "/projects/legacy/tasks", `{"title":"x"}`, 409},
		{"CreateOK", "ann", "POST", "/projects/web/tasks", `{"title":"x","assignee":"ben"}`, 200},
		{"CreateViewerAssignee400", "ann", "POST", "/projects/web/tasks", `{"title":"x","assignee":"dan"}`, 400},
		{"CreateHalAssignee400", "ann", "POST", "/projects/web/tasks", `{"title":"x","assignee":"hal"}`, 400},
		{"CreateGusAssignee400", "ann", "POST", "/projects/web/tasks", `{"title":"x","assignee":"gus"}`, 400},
		{"PatchContributorOthers403", "ben", "PATCH", "/projects/web/tasks/t2", `{"status":"done"}`, 403},
		{"PatchContributorOwn", "ben", "PATCH", "/projects/web/tasks/t1", `{"status":"doing"}`, 200},
		{"PatchContributorReassign403", "ben", "PATCH", "/projects/web/tasks/t1", `{"assignee":"cat"}`, 403},
		{"PatchManagerReassignOK", "ivy", "PATCH", "/projects/web/tasks/t3", `{"assignee":"ben"}`, 200},
		{"PatchArchived409", "ann", "PATCH", "/projects/legacy/tasks/t5", `{"status":"done"}`, 409},
		{"DeleteContributor403", "ben", "DELETE", "/projects/web/tasks/t1", "", 403},
		{"DeleteManager", "ann", "DELETE", "/projects/web/tasks/t3", "", 204},
		{"DeleteOrgAdmin403", "eve", "DELETE", "/projects/web/tasks/t1", "", 403},
		{"GxManagerWrites", "gus", "PATCH", "/projects/gx/tasks/t6", `{"status":"doing"}`, 200},
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
			"gx/gus": "400/400", "gx/ann": "404/404", "gx/eve": "404/404", "gx/ben": "404/404",
		}
		for k, v := range want {
			if sig[k] != v {
				t.Errorf("probe %s = %s, want %s", k, sig[k], v)
			}
		}
	})

	// ---------- FUNC ----------
	t.Run("FUNC_TransferHalNoMembershipsNoTasks", func(t *testing.T) {
		h := newServer()
		before, after := zgTrOK(t, h, "hal")
		if d := zgDiff(before, after); strings.Contains(d, "task") || strings.Contains(d, " vanished") {
			t.Errorf("transferring hal changed tasks: %s", d)
		}
		zgNoRead(t, h, "hal", "/projects/web/tasks", "/projects/gx/tasks", "/projects/gx/tasks/t6")
		zgNotWriter(t, h, "hal")
	})
	t.Run("FUNC_TransferViewerDan", func(t *testing.T) {
		h := newServer()
		zgTrOK(t, h, "dan")
		zgNoRead(t, h, "dan", "/projects/web/tasks", "/projects/web/tasks/t1", "/projects/gx/tasks")
		zgNotWriter(t, h, "dan")
	})
	t.Run("FUNC_TransferManagerIvy", func(t *testing.T) {
		h := newServer()
		zgTrOK(t, h, "ivy")
		zgNoRead(t, h, "ivy", "/projects/web/tasks", "/projects/ops/tasks", "/projects/gx/tasks")
		if c, b := zgDo(h, "ivy", "POST", "/projects/ops/tasks", `{"title":"after"}`); zg2xx(c) {
			t.Errorf("ivy can still write ops: %d %s", c, b)
		}
		if c, b := zgDo(h, "ivy", "PATCH", "/projects/web/tasks/t3", `{"status":"done"}`); zg2xx(c) {
			t.Errorf("ivy can still modify web tasks: %d %s", c, b)
		}
		zgNotWriter(t, h, "ivy")
		zgMust(t, h, "ann", "POST", "/projects/web/tasks", `{"title":"x"}`, 200)
	})
	t.Run("FUNC_TransferAnnManagerAllThree", func(t *testing.T) {
		h := newServer()
		zgTrOK(t, h, "ann")
		zgNoRead(t, h, "ann", "/projects/web/tasks", "/projects/ops/tasks", "/projects/legacy/tasks", "/projects/gx/tasks")
		if c, b := zgDo(h, "ann", "DELETE", "/projects/web/tasks/t3", ""); zg2xx(c) {
			t.Errorf("ann can still delete in web: %d %s", c, b)
		}
		zgNotWriter(t, h, "ann")
		zgMust(t, h, "ivy", "PATCH", "/projects/web/tasks/t1", `{"status":"doing"}`, 200)
	})
	t.Run("FUNC_ResponseBodyHal", func(t *testing.T) {
		h := newServer()
		code, body := zgTr(h, "eve", "acme", "hal", "globex")
		if !zg2xx(code) {
			t.Fatalf("-> %d body=%s", code, body)
		}
		var r map[string]any
		if err := json.Unmarshal([]byte(body), &r); err != nil {
			t.Fatalf("body is not JSON: %v: %s", err, body)
		}
		if r["id"] != "hal" || r["orgId"] != "globex" {
			t.Errorf("body = %s, want id=hal orgId=globex", body)
		}
		if l := zgLeaks("eve", body); len(l) > 0 {
			t.Errorf("leak: %v", l)
		}
	})
	t.Run("FUNC_ResponseBodyBen", func(t *testing.T) {
		h := newServer()
		code, body := zgTr(h, "eve", "acme", "ben", "globex")
		if code == 400 || code == 409 {
			return // rejection of an assignee is covered by ATOM_
		}
		if !zg2xx(code) {
			t.Fatalf("-> %d body=%s", code, body)
		}
		var r map[string]any
		if err := json.Unmarshal([]byte(body), &r); err != nil || r["id"] != "ben" || r["orgId"] != "globex" {
			t.Errorf("body = %s, want id=ben orgId=globex", body)
		}
	})
	t.Run("FUNC_TransferredUserStillAuthenticatesAndSeesNothing", func(t *testing.T) {
		h := newServer()
		zgTrOK(t, h, "hal")
		for _, p := range []string{"/projects/web/tasks", "/projects/ops/tasks", "/projects/legacy/tasks", "/projects/gx/tasks"} {
			zgMust(t, h, "hal", "GET", p, "", 404)
		}
	})
	t.Run("FUNC_OnlyAffectsTarget", func(t *testing.T) {
		h := newServer()
		zgTrOK(t, h, "dan")
		zgMust(t, h, "gus", "GET", "/projects/gx/tasks", "", 200)
		zgMust(t, h, "ben", "PATCH", "/projects/web/tasks/t1", `{"status":"doing"}`, 200)
		zgMust(t, h, "cat", "GET", "/projects/ops/tasks", "", 200)
		zgMust(t, h, "eve", "GET", "/projects/legacy/tasks", "", 200)
		zgMust(t, h, "hal", "GET", "/projects/gx/tasks", "", 404)
	})
	t.Run("FUNC_SequentialDanHalIvy", func(t *testing.T) {
		h := newServer()
		for _, u := range []string{"dan", "hal", "ivy"} {
			zgTrOK(t, h, u)
		}
	})
	t.Run("FUNC_TransferredUserGoneFromSourceOrg", func(t *testing.T) {
		h := newServer()
		zgTrOK(t, h, "hal")
		before := zgSnap(t, h)
		for _, r := range []zgReq{{"eve", "acme", "hal", "globex"}, {"eve", "acme", "hal", "acme"}, {"eve", "globex", "hal", "globex"}, {"gus", "globex", "hal", "acme"}, {"hal", "globex", "hal", "acme"}, {"hal", "acme", "hal", "acme"}} {
			if c, b := zgTr(h, r.user, r.org, r.uid, r.to); zg2xx(c) {
				t.Errorf("unexpected acceptance after hal moved: %+v -> %d %s", r, c, b)
			}
		}
		if d := zgDiff(before, zgSnap(t, h)); d != "" {
			t.Errorf("state changed: %s", d)
		}
	})
	t.Run("FUNC_TransferredUserCannotAdminOrWriteAcme", func(t *testing.T) {
		h := newServer()
		zgTrOK(t, h, "hal")
		before := zgSnap(t, h)
		if c, b := zgDo(h, "hal", "POST", "/projects/web/tasks", `{"title":"hal-write"}`); zg2xx(c) {
			t.Errorf("hal writes to web: %d %s", c, b)
		}
		if c, b := zgTr(h, "hal", "acme", "dan", "globex"); zg2xx(c) {
			t.Errorf("hal transfers dan: %d %s", c, b)
		}
		if d := zgDiff(before, zgSnap(t, h)); d != "" {
			t.Errorf("state changed: %s", d)
		}
	})
	t.Run("FUNC_BodyExtraFieldsIgnored", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		code, body := zgTrBody(h, "eve", "acme", "dan", `{"toOrgId":"globex","uid":"cat","org":"globex","orgId":"acme"}`)
		if !zg2xx(code) {
			t.Fatalf("-> %d body=%s", code, body)
		}
		for _, p := range zgCheckTr(before, zgSnap(t, h), "dan") {
			t.Errorf("%s", p)
		}
	})

	// ---------- ATOM: assignee invariant ----------
	// Accept either rejection (non-2xx, nothing changed) or success where the user lost every
	// acme membership and tasks assigned to them were unassigned (nothing else changed).
	atom := func(name string, setup func(h http.Handler), uids ...string) {
		t.Run("ATOM_"+name, func(t *testing.T) {
			h := newServer()
			if setup != nil {
				setup(h)
			}
			for _, uid := range uids {
				before := zgSnap(t, h)
				code, body := zgTr(h, "eve", "acme", uid, "globex")
				after := zgSnap(t, h)
				if zg2xx(code) {
					for _, p := range zgCheckTr(before, after, uid) {
						t.Errorf("%s (transfer %s -> %d)", p, uid, code)
					}
					zgNotWriter(t, h, uid)
				} else if d := zgDiff(before, after); d != "" {
					t.Errorf("state changed despite %d for %s: %s body=%s", code, uid, d, body)
				}
			}
		})
	}
	atom("TransferBen_webAndArchivedLegacyTasks", nil, "ben")
	atom("TransferCat_webAndOpsTasks", nil, "cat")
	atom("TransferBenThenCat", nil, "ben", "cat")
	atom("TransferFreshAssigneeIvy", func(h http.Handler) {
		zgDo(h, "ann", "PATCH", "/projects/web/tasks/t3", `{"assignee":"ivy"}`)
		zgDo(h, "ivy", "POST", "/projects/ops/tasks", `{"title":"Ivy ops","assignee":"ivy"}`)
	}, "ivy")
	atom("TransferFreshAssigneeAnn", func(h http.Handler) {
		zgDo(h, "ann", "POST", "/projects/web/tasks", `{"title":"Ann web","assignee":"ann"}`)
	}, "ann")
	t.Run("ATOM_BenRejectedMeansStillFullAcmeMember", func(t *testing.T) {
		h := newServer()
		code, _ := zgTr(h, "eve", "acme", "ben", "globex")
		if zg2xx(code) {
			return // covered by ATOM_TransferBen_*
		}
		sig := zgSig(h)
		if sig["web/ben"] != "403/400" || sig["legacy/ben"] != "409/409" || sig["gx/ben"] != "404/404" {
			t.Errorf("transfer rejected (%d) but ben's roles changed: web=%s legacy=%s gx=%s", code, sig["web/ben"], sig["legacy/ben"], sig["gx/ben"])
		}
		zgMust(t, h, "ben", "GET", "/projects/web/tasks", "", 200)
	})
	t.Run("ATOM_TransferBenThenBenCannotKeepWorking", func(t *testing.T) {
		h := newServer()
		code, _ := zgTr(h, "eve", "acme", "ben", "globex")
		if !zg2xx(code) {
			return
		}
		if c, b := zgDo(h, "ben", "PATCH", "/projects/web/tasks/t1", `{"status":"done"}`); zg2xx(c) {
			t.Errorf("transferred ben can still modify his old acme task: %d %s", c, b)
		}
		if c, b := zgDo(h, "ben", "POST", "/projects/web/tasks", `{"title":"ben-after"}`); zg2xx(c) {
			t.Errorf("transferred ben can still create in web: %d %s", c, b)
		}
		zgNoRead(t, h, "ben", "/projects/web/tasks", "/projects/web/tasks/t1", "/projects/legacy/tasks", "/projects/legacy/tasks/t5", "/projects/ops/tasks", "/projects/gx/tasks", "/projects/gx/tasks/t6")
		if c, b := zgDo(h, "ben", "PATCH", "/projects/gx/tasks/t6", `{"status":"done"}`); zg2xx(c) {
			t.Errorf("transferred ben can modify gx task: %d %s", c, b)
		}
		zgNotWriter(t, h, "ben")
	})
	t.Run("ATOM_TransferBenThenOthersStillWork", func(t *testing.T) {
		h := newServer()
		code, _ := zgTr(h, "eve", "acme", "ben", "globex")
		if !zg2xx(code) {
			return
		}
		zgMust(t, h, "ann", "PATCH", "/projects/web/tasks/t2", `{"status":"done"}`, 200)
		zgMust(t, h, "cat", "PATCH", "/projects/web/tasks/t2", `{"status":"open"}`, 200)
		zgMust(t, h, "ann", "POST", "/projects/web/tasks", `{"title":"after","assignee":"cat"}`, 200)
		zgMust(t, h, "gus", "PATCH", "/projects/gx/tasks/t6", `{"status":"doing"}`, 200)
	})
	t.Run("ATOM_TransferBenThenGusIsolatedFromAcme", func(t *testing.T) {
		h := newServer()
		zgTr(h, "eve", "acme", "ben", "globex")
		zgNoRead(t, h, "gus", "/projects/web/tasks", "/projects/web/tasks/t1", "/projects/ops/tasks", "/projects/legacy/tasks", "/projects/legacy/tasks/t5")
		zgMust(t, h, "gus", "GET", "/projects/gx/tasks", "", 200)
	})

	// ---------- AUTHZ ----------
	for _, c := range []struct {
		n string
		r zgReq
	}{
		{"ManagerAnnTransfersDan", zgReq{"ann", "acme", "dan", "globex"}},
		{"ManagerIvyTransfersDan", zgReq{"ivy", "acme", "dan", "globex"}},
		{"ManagerIvyTransfersHal", zgReq{"ivy", "acme", "hal", "globex"}},
		{"ManagerIvyTransfersBen", zgReq{"ivy", "acme", "ben", "globex"}},
		{"ManagerAnnSelf", zgReq{"ann", "acme", "ann", "globex"}},
		{"ManagerIvySelf", zgReq{"ivy", "acme", "ivy", "globex"}},
		{"ContributorBenTransfersCat", zgReq{"ben", "acme", "cat", "globex"}},
		{"ContributorBenSelf", zgReq{"ben", "acme", "ben", "globex"}},
		{"ContributorCatTransfersDan", zgReq{"cat", "acme", "dan", "globex"}},
		{"ViewerDanSelf", zgReq{"dan", "acme", "dan", "globex"}},
		{"ViewerDanTransfersBen", zgReq{"dan", "acme", "ben", "globex"}},
		{"NoMembershipHalTransfersDan", zgReq{"hal", "acme", "dan", "globex"}},
		{"NoMembershipHalSelf", zgReq{"hal", "acme", "hal", "globex"}},
		{"CrossOrgGusTransfersBen", zgReq{"gus", "acme", "ben", "globex"}},
		{"CrossOrgGusTransfersBenToGlobexViaGlobexPath", zgReq{"gus", "globex", "ben", "globex"}},
		{"CrossOrgGusTransfersDanToGlobex", zgReq{"gus", "acme", "dan", "globex"}},
		{"CrossOrgGusSelfToAcmeViaAcme", zgReq{"gus", "acme", "gus", "acme"}},
		{"CrossOrgGusSelfToAcmeViaGlobex", zgReq{"gus", "globex", "gus", "acme"}},
		{"CrossOrgGusSelfToGlobexViaGlobex", zgReq{"gus", "globex", "gus", "globex"}},
		{"CrossOrgGusTransfersAnnViaGlobex", zgReq{"gus", "globex", "ann", "globex"}},
		{"CrossOrgGusTransfersAnnToAcmeViaGlobex", zgReq{"gus", "globex", "ann", "acme"}},
		{"CrossOrgEveTransfersGusViaAcme", zgReq{"eve", "acme", "gus", "acme"}},
		{"CrossOrgEveTransfersGusToAcmeViaGlobex", zgReq{"eve", "globex", "gus", "acme"}},
		{"CrossOrgEveTransfersGusToGlobexViaGlobex", zgReq{"eve", "globex", "gus", "globex"}},
		{"CrossOrgEveTransfersBenViaGlobex", zgReq{"eve", "globex", "ben", "globex"}},
		{"CrossOrgEveTransfersBenViaGlobexToAcme", zgReq{"eve", "globex", "ben", "acme"}},
		{"CrossOrgEveTransfersDanViaGlobex", zgReq{"eve", "globex", "dan", "globex"}},
		{"EveUnknownOrgPath", zgReq{"eve", "nope", "ben", "globex"}},
		{"EveUnknownTargetZed", zgReq{"eve", "acme", "zed", "globex"}},
		{"EveTargetCaseMismatch", zgReq{"eve", "acme", "BEN", "globex"}},
		{"EveOrgCaseMismatch", zgReq{"eve", "ACME", "dan", "globex"}},
		{"ManagerAnnWrongOrgPath", zgReq{"ann", "globex", "dan", "globex"}},
		{"Anonymous", zgReq{"", "acme", "dan", "globex"}},
		{"UnknownUser", zgReq{"zed", "acme", "dan", "globex"}},
		{"AnonymousUnknownOrg", zgReq{"", "nope", "dan", "globex"}},
	} {
		c := c
		t.Run("AUTHZ_"+c.n, func(t *testing.T) { zgDenied(t, c.r, 0) })
	}
	t.Run("AUTHZ_CrossOrgGusNeverReadsAcme", func(t *testing.T) {
		h := newServer()
		for _, r := range []zgReq{{"eve", "acme", "gus", "acme"}, {"eve", "globex", "gus", "acme"}, {"gus", "globex", "gus", "acme"}, {"gus", "acme", "gus", "acme"}, {"ann", "acme", "gus", "acme"}, {"hal", "globex", "gus", "acme"}} {
			zgTr(h, r.user, r.org, r.uid, r.to)
		}
		zgNoRead(t, h, "gus", "/projects/web/tasks", "/projects/ops/tasks", "/projects/legacy/tasks", "/projects/web/tasks/t1")
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
		for _, r := range []zgReq{{"hal", "acme", "dan", "globex"}, {"gus", "acme", "ben", "globex"}, {"", "acme", "dan", "globex"}, {"ann", "acme", "dan", "globex"}, {"eve", "globex", "gus", "acme"}, {"gus", "globex", "ann", "globex"}} {
			_, body := zgTr(h, r.user, r.org, r.uid, r.to)
			if l := zgLeaks(r.user, body); len(l) > 0 {
				t.Errorf("leak: %v in %s", l, body)
			}
		}
	})
	t.Run("AUTHZ_DeniedAttemptsDoNotAccumulate", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		for _, r := range []zgReq{{"ann", "acme", "dan", "globex"}, {"ivy", "acme", "ben", "globex"}, {"ben", "acme", "ben", "globex"}, {"hal", "acme", "cat", "globex"}, {"gus", "acme", "dan", "globex"}, {"", "acme", "dan", "globex"}, {"eve", "globex", "dan", "globex"}, {"gus", "globex", "gus", "acme"}} {
			if c, b := zgTr(h, r.user, r.org, r.uid, r.to); zg2xx(c) {
				t.Errorf("unexpected acceptance: %+v -> %d %s", r, c, b)
			}
		}
		if d := zgDiff(before, zgSnap(t, h)); d != "" {
			t.Errorf("state changed: %s", d)
		}
		zgNotWriter(t, h, "dan")
	})

	// ---------- VALID ----------
	for _, c := range []struct{ n, path string }{
		{"MissingUID", "/orgs/acme/users//transfer"},
		{"ExtraSegment", "/orgs/acme/users/dan/transfer/now"},
		{"EncodedSlashUID", "/orgs/acme/users/dan%2Fben/transfer"},
		{"SpaceUID", "/orgs/acme/users/%20dan/transfer"},
		{"DotDotUID", "/orgs/acme/users/../transfer"},
		{"UpperUID", "/orgs/acme/users/DAN/transfer"},
		{"EmptyOrg", "/orgs//users/dan/transfer"},
		{"WrongCollection", "/orgs/acme/members/dan/transfer"},
		{"WrongVerb", "/orgs/acme/users/dan/move"},
	} {
		c := c
		t.Run("VALID_Path"+c.n, func(t *testing.T) {
			h := newServer()
			before := zgSnap(t, h)
			code, body := zgDo(h, "eve", "POST", c.path, `{"toOrgId":"globex"}`)
			if zg2xx(code) {
				t.Errorf("unexpected acceptance: POST %s -> %d body=%s", c.path, code, body)
			}
			if d := zgDiff(before, zgSnap(t, h)); d != "" {
				t.Errorf("state changed: %s", d)
			}
		})
	}
	for _, c := range []struct{ n, body string }{
		{"EmptyBody", ``},
		{"EmptyObject", `{}`},
		{"EmptyToOrgId", `{"toOrgId":""}`},
		{"NullToOrgId", `{"toOrgId":null}`},
		{"NotJSON", `not json`},
		{"NumberToOrgId", `{"toOrgId":5}`},
		{"ArrayToOrgId", `{"toOrgId":["globex"]}`},
		{"UnknownOrg", `{"toOrgId":"nope"}`},
		{"UnknownOrgInitech", `{"toOrgId":"initech"}`},
		{"CaseMismatchOrg", `{"toOrgId":"GLOBEX"}`},
		{"SpacedOrg", `{"toOrgId":" globex"}`},
		{"TrailingSpaceOrg", `{"toOrgId":"globex "}`},
		{"WrongFieldName", `{"orgId":"globex"}`},
		{"WrongFieldName2", `{"toOrg":"globex"}`},
		{"TruncatedJSON", `{"toOrgId":"globex"`},
	} {
		c := c
		t.Run("VALID_Body"+c.n, func(t *testing.T) {
			h := newServer()
			before := zgSnap(t, h)
			code, body := zgTrBody(h, "eve", "acme", "dan", c.body)
			if zg2xx(code) {
				t.Errorf("unexpected acceptance: body %q -> %d %s", c.body, code, body)
			}
			if d := zgDiff(before, zgSnap(t, h)); d != "" {
				t.Errorf("state changed: %s", d)
			}
		})
	}
	t.Run("VALID_OtherMethodsDoNotTransfer", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		for _, m := range []string{"GET", "PUT", "PATCH", "DELETE"} {
			zgDo(h, "eve", m, "/orgs/acme/users/dan/transfer", `{"toOrgId":"globex"}`)
		}
		for _, m := range []string{"PUT", "PATCH", "DELETE", "POST"} {
			zgDo(h, "eve", m, "/orgs/acme/users/dan", `{"toOrgId":"globex","orgId":"globex"}`)
		}
		if d := zgDiff(before, zgSnap(t, h)); d != "" {
			t.Errorf("non-POST method changed state: %s", d)
		}
		zgNotWriter(t, h, "dan")
	})
	t.Run("VALID_QueryStringOrgIgnored", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		code, body := zgDo(h, "eve", "POST", "/orgs/acme/users/dan/transfer?toOrgId=globex", "")
		if zg2xx(code) {
			t.Errorf("unexpected acceptance with query-only toOrgId: %d %s", code, body)
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
		{"Manager403", zgReq{"ann", "acme", "dan", "globex"}, 403},
		{"ManagerIvy403", zgReq{"ivy", "acme", "dan", "globex"}, 403},
		{"Contributor403", zgReq{"ben", "acme", "dan", "globex"}, 403},
		{"Viewer403", zgReq{"dan", "acme", "dan", "globex"}, 403},
		{"NoMembership403", zgReq{"hal", "acme", "dan", "globex"}, 403},
		{"AdminWrongOrg404", zgReq{"eve", "globex", "gus", "acme"}, 404},
		{"AdminWrongOrgAcmeUser404", zgReq{"eve", "globex", "ben", "globex"}, 404},
		{"AdminUnknownOrg404", zgReq{"eve", "nope", "ben", "globex"}, 404},
		{"AdminOtherOrgTarget404", zgReq{"eve", "acme", "gus", "acme"}, 404},
		{"AdminUnknownTarget404", zgReq{"eve", "acme", "zed", "globex"}, 404},
		{"Anonymous401", zgReq{"", "acme", "dan", "globex"}, 401},
		{"UnknownUser401", zgReq{"zed", "acme", "dan", "globex"}, 401},
	} {
		c := c
		t.Run("CONTRACT_"+c.n, func(t *testing.T) {
			h := newServer()
			code, body := zgTr(h, c.r.user, c.r.org, c.r.uid, c.r.to)
			if code != c.want {
				t.Errorf("POST /orgs/%s/users/%s/transfer as %q -> %d, want %d body=%s", c.r.org, c.r.uid, c.r.user, code, c.want, body)
			}
		})
	}
	t.Run("CONTRACT_Success200Dan", func(t *testing.T) {
		h := newServer()
		code, body := zgTr(h, "eve", "acme", "dan", "globex")
		var r map[string]string
		if code != 200 || json.Unmarshal([]byte(body), &r) != nil || r["id"] != "dan" || r["orgId"] != "globex" {
			t.Errorf("-> %d body=%s, want 200 {\"id\":\"dan\",\"orgId\":\"globex\"}", code, body)
		}
	})
	t.Run("CONTRACT_Success200Hal", func(t *testing.T) {
		code, body := zgTr(newServer(), "eve", "acme", "hal", "globex")
		if code != 200 {
			t.Errorf("-> %d, want 200 body=%s", code, body)
		}
	})
	t.Run("CONTRACT_UnknownToOrg400or404", func(t *testing.T) {
		code, body := zgTr(newServer(), "eve", "acme", "dan", "nope")
		if code != 400 && code != 404 {
			t.Errorf("-> %d, want 400 or 404 body=%s", code, body)
		}
	})
	t.Run("CONTRACT_MissingToOrg400", func(t *testing.T) {
		code, body := zgTrBody(newServer(), "eve", "acme", "dan", `{}`)
		if code != 400 {
			t.Errorf("-> %d, want 400 body=%s", code, body)
		}
	})
	t.Run("CONTRACT_ErrorShape", func(t *testing.T) {
		code, body := zgTr(newServer(), "ann", "acme", "dan", "globex")
		var e map[string]string
		if code != 403 || json.Unmarshal([]byte(body), &e) != nil || e["error"] == "" {
			t.Errorf("-> %d body=%s, want 403 {\"error\":...}", code, body)
		}
	})
	t.Run("CONTRACT_NotFoundErrorShape", func(t *testing.T) {
		code, body := zgTr(newServer(), "eve", "acme", "gus", "acme")
		var e map[string]string
		if code != 404 || json.Unmarshal([]byte(body), &e) != nil || e["error"] == "" {
			t.Errorf("-> %d body=%s, want 404 {\"error\":...}", code, body)
		}
	})
	t.Run("CONTRACT_AssigneeRejectionStatus", func(t *testing.T) {
		code, body := zgTr(newServer(), "eve", "acme", "ben", "globex")
		if zg2xx(code) {
			return
		}
		if code != 409 && code != 400 {
			t.Errorf("rejected transfer of assignee ben with %d (want 409 or 400) body=%s", code, body)
		}
	})

	// ---------- INFO ----------
	t.Run("INFO_TransferBen", func(t *testing.T) {
		h := newServer()
		code, body := zgTr(h, "eve", "acme", "ben", "globex")
		tt := zgTasks(t, h)
		sig := zgSig(h)
		t.Logf("INFO eve transfers ben (t1 web, t5 archived legacy): -> %d body=%q; t1=%+v t5=%+v; probes web=%s legacy=%s gx=%s",
			code, body, tt["t1"], tt["t5"], sig["web/ben"], sig["legacy/ben"], sig["gx/ben"])
	})
	t.Run("INFO_TransferCat", func(t *testing.T) {
		h := newServer()
		code, body := zgTr(h, "eve", "acme", "cat", "globex")
		tt := zgTasks(t, h)
		t.Logf("INFO eve transfers cat (t2 web, t4 ops): -> %d body=%q; t2=%+v t4=%+v", code, body, tt["t2"], tt["t4"])
	})
	t.Run("INFO_TransferToSameOrg", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		code, body := zgTr(h, "eve", "acme", "dan", "acme")
		t.Logf("INFO eve transfers dan to acme (same org): -> %d body=%q; diff=%q", code, body, zgDiff(before, zgSnap(t, h)))
	})
	t.Run("INFO_AdminTransfersSelf", func(t *testing.T) {
		h := newServer()
		code, body := zgTr(h, "eve", "acme", "eve", "globex")
		c1, _ := zgDo(h, "eve", "GET", "/projects/web/tasks", "")
		c2, _ := zgDo(h, "eve", "GET", "/projects/gx/tasks", "")
		t.Logf("INFO eve transfers herself -> %d body=%q; eve GET web -> %d, GET gx -> %d (200 = admin flag carried over to globex)", code, body, c1, c2)
	})
	t.Run("INFO_TransferToUnadministeredOrg", func(t *testing.T) {
		h := newServer()
		code, body := zgTr(h, "eve", "acme", "hal", "globex")
		t.Logf("INFO eve (acme admin only) transfers hal into globex, which she does not administer -> %d body=%q (task intends this to work)", code, body)
	})
	t.Run("INFO_ResponseShape", func(t *testing.T) {
		code, body := zgTr(newServer(), "eve", "acme", "hal", "globex")
		t.Logf("INFO response for hal: %d %q", code, body)
	})
	t.Run("INFO_SecondTransferSameUser", func(t *testing.T) {
		h := newServer()
		c1, _ := zgTr(h, "eve", "acme", "dan", "globex")
		c2, _ := zgTr(h, "eve", "acme", "dan", "globex")
		t.Logf("INFO transfer dan twice -> %d then %d", c1, c2)
	})
	t.Run("INFO_TransferredUserAsCaller", func(t *testing.T) {
		h := newServer()
		zgTr(h, "eve", "acme", "dan", "globex")
		c, _ := zgTr(h, "dan", "acme", "hal", "globex")
		t.Logf("INFO transferred dan tries to transfer hal -> %d", c)
	})
	t.Run("INFO_RoundTrip", func(t *testing.T) {
		h := newServer()
		c1, _ := zgTr(h, "eve", "acme", "hal", "globex")
		c2, _ := zgTr(h, "gus", "globex", "hal", "acme")
		t.Logf("INFO hal to globex then gus (globex non-admin) moves back -> %d, %d", c1, c2)
	})
}
