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

// Hidden grader for task 018 (POST /projects/{pid}/members/import).
// Uses only newServer() and HTTP; works on the if-chain and the capability baseline.
// Roles are observed through non-mutating probes on existing endpoints (see zgSig).
// Rules assumed: caller must be manager of the target (403 contributor/viewer/org admin,
// 404 non-member/cross-org, 409 archived); caller must be able to read the source
// (else 404, never import cross-org); existing members keep their role.

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

func zgImportBody(from string) string { return `{"fromProjectId":"` + from + `"}` }

func zgImport(h http.Handler, user, pid, from string) (int, string) {
	return zgDo(h, user, "POST", "/projects/"+pid+"/members/import", zgImportBody(from))
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

// zgLeaks: task titles of unreadable projects, and the other-org user name in a response to an acme caller.
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
	if user != "gus" && user != "" && strings.Contains(body, "gus") {
		l = append(l, "member gus of globex")
	}
	return l
}

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

var zgUsers = []string{"ann", "ben", "cat", "dan", "eve", "hal", "ivy", "gus"}

// zgSig observes every user's role in web/ops/legacy/gx without mutating anything:
//
//	PATCH <task> {"assignee":"zzz"}: 404 non-member, 403 viewer/org admin/contributor-not-own, 400 manager (409 archived)
//	POST  tasks  {}               : 404 non-member, 403 viewer/org admin, 400 contributor/manager (409 archived)
//
// non-member 404/404, viewer 403/403, contributor 403/400, manager 400/400.
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

var zgRoleSig = map[string]string{"manager": "400/400", "contributor": "403/400", "viewer": "403/403", "": "404/404"}

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

func zgIR(user, pid, from string) zgReq { return zgReq{user, pid, zgImportBody(from)} }

func zgPost(h http.Handler, r zgReq) (int, string) {
	return zgDo(h, r.user, "POST", "/projects/"+r.pid+"/members/import", r.body)
}

// zgDenied: non-2xx, no task or role change anywhere, no leak; want!=0 checks the exact status.
func zgDenied(t *testing.T, r zgReq, want int) {
	t.Helper()
	h := newServer()
	before := zgSnap(t, h)
	code, resp := zgPost(h, r)
	after := zgSnap(t, h)
	if code >= 200 && code < 300 {
		t.Errorf("unexpected acceptance: POST %s/members/import %s as %q -> %d body=%s", r.pid, r.body, r.user, code, resp)
	}
	if d := zgDiff(before, after); d != "" {
		t.Errorf("state changed: %s", d)
	}
	if l := zgLeaks(r.user, resp); len(l) > 0 {
		t.Errorf("leak: response exposes %v: %s", l, resp)
	}
	if want != 0 && code != want {
		t.Errorf("status %d, want %d (body=%s)", code, want, resp)
	}
}

// zgOverride lets a test state non-seed roles for projects other than the target.
var zgOverride = map[string]map[string]string{}

type zgMember struct {
	UserID string `json:"userId"`
	Role   string `json:"role"`
}

func zgSeedRoles(pid string) map[string]string {
	switch pid {
	case "web":
		return map[string]string{"ann": "manager", "ben": "contributor", "cat": "contributor", "dan": "viewer", "ivy": "manager"}
	case "ops":
		return map[string]string{"ann": "viewer", "cat": "contributor", "ivy": "manager"}
	case "legacy":
		return map[string]string{"ann": "manager", "ben": "contributor"}
	case "gx":
		return map[string]string{"gus": "manager"}
	}
	return nil
}

// zgImportOK performs the import (must be 200), checks the returned members equal want,
// and verifies via role probes that every project's roles are exactly as expected.
func zgImportOK(t *testing.T, h http.Handler, user, pid, from string, want map[string]string) []zgMember {
	t.Helper()
	before := zgSnap(t, h)
	code, body := zgImport(h, user, pid, from)
	if code != 200 {
		t.Fatalf("import %s into %s as %s: status %d, want 200 (body=%s)", from, pid, user, code, body)
	}
	var ms []zgMember
	if err := json.Unmarshal([]byte(body), &ms); err != nil {
		t.Fatalf("response is not [{userId,role}]: %v (%s)", err, body)
	}
	got := map[string]string{}
	for _, m := range ms {
		if _, dup := got[m.UserID]; dup {
			t.Errorf("duplicate member %s in response", m.UserID)
		}
		got[m.UserID] = m.Role
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("response members %v, want %v", got, want)
	}
	after := zgSnap(t, h)
	for k, v := range after.tasks {
		if before.tasks[k] != v {
			t.Errorf("task changed: %+v -> %+v", before.tasks[k], v)
		}
	}
	for _, p := range []string{"web", "ops", "legacy", "gx"} {
		exp := zgSeedRoles(p)
		if o, ok := zgOverride[p]; ok {
			exp = o
		}
		if p == pid {
			exp = want
		}
		for _, u := range zgUsers {
			e := zgRoleSig[exp[u]]
			if u == "eve" && p != "gx" {
				e = "403/403"
			}
			if p == "legacy" && exp[u] != "" {
				e = "409/409"
			}
			if after.sig[p+"/"+u] != e {
				t.Errorf("role probe %s/%s = %s, want %s (role %q)", p, u, after.sig[p+"/"+u], e, exp[u])
			}
		}
	}
	return ms
}

func zgWith(base map[string]string, kv ...string) map[string]string {
	out := map[string]string{}
	for k, v := range base {
		out[k] = v
	}
	for i := 0; i+1 < len(kv); i += 2 {
		out[kv[i]] = kv[i+1]
	}
	return out
}

func TestGrader(t *testing.T) {
	// ---------- REG ----------
	for _, c := range []struct {
		n, user, path string
		want          int
	}{
		{"ListWebManager", "ann", "/projects/web/tasks", 200},
		{"ListWebViewer", "dan", "/projects/web/tasks", 200},
		{"ListWebOrgAdmin", "eve", "/projects/web/tasks", 200},
		{"ListLegacyArchived", "ann", "/projects/legacy/tasks", 200},
		{"ListGxOtherOrg404", "eve", "/projects/gx/tasks", 404},
		{"ListWebNonMember404", "hal", "/projects/web/tasks", 404},
		{"ListWebOtherOrg404", "gus", "/projects/web/tasks", 404},
		{"ListOpsNotMember404", "ben", "/projects/ops/tasks", 404},
		{"ListOpsIvy", "ivy", "/projects/ops/tasks", 200},
		{"ListAnon401", "", "/projects/web/tasks", 401},
		{"ListUnknown401", "zed", "/projects/web/tasks", 401},
		{"GetTask", "ben", "/projects/web/tasks/t1", 200},
		{"GetTaskWrongProject404", "ann", "/projects/web/tasks/t4", 404},
		{"GetTaskOtherOrg404", "ann", "/projects/gx/tasks/t6", 404},
	} {
		c := c
		t.Run("REG_"+c.n, func(t *testing.T) {
			code, _ := zgDo(newServer(), c.user, "GET", c.path, "")
			if code != c.want {
				t.Fatalf("got %d want %d", code, c.want)
			}
		})
	}
	regDeny := []struct {
		n                         string
		user, method, path, body string
		w                         int
	}{
		{"ViewerCreate", "dan", "POST", "/projects/web/tasks", `{"title":"x"}`, 403},
		{"ViewerUpdate", "dan", "PATCH", "/projects/web/tasks/t1", `{"status":"done"}`, 403},
		{"ViewerDelete", "dan", "DELETE", "/projects/web/tasks/t1", "", 403},
		{"OrgAdminCreate", "eve", "POST", "/projects/web/tasks", `{"title":"x"}`, 403},
		{"AnnViewerOpsCreate", "ann", "POST", "/projects/ops/tasks", `{"title":"x"}`, 403},
		{"NonMemberCreate", "hal", "POST", "/projects/web/tasks", `{"title":"x"}`, 404},
		{"ContribUpdateOthers", "ben", "PATCH", "/projects/web/tasks/t2", `{"status":"done"}`, 403},
		{"ContribReassign", "ben", "PATCH", "/projects/web/tasks/t1", `{"assignee":"cat"}`, 403},
		{"ContribDelete", "ben", "DELETE", "/projects/web/tasks/t1", "", 403},
		{"ContribCreateForOther", "ben", "POST", "/projects/web/tasks", `{"title":"x","assignee":"cat"}`, 403},
		{"OtherOrgUpdate", "gus", "PATCH", "/projects/web/tasks/t1", `{"status":"done"}`, 404},
		{"AnonUpdate", "", "PATCH", "/projects/web/tasks/t1", `{"status":"done"}`, 401},
		{"ArchivedManagerCreate", "ann", "POST", "/projects/legacy/tasks", `{"title":"x"}`, 409},
		{"ArchivedManagerUpdate", "ann", "PATCH", "/projects/legacy/tasks/t5", `{"status":"done"}`, 409},
		{"ManagerUpdateWrongProject", "ann", "PATCH", "/projects/web/tasks/t4", `{"status":"done"}`, 404},
		{"ManagerInvalidStatus", "ann", "PATCH", "/projects/web/tasks/t1", `{"status":"bogus"}`, 400},
		{"ManagerCreateBadAssignee", "ann", "POST", "/projects/web/tasks", `{"title":"x","assignee":"hal"}`, 400},
		{"IvyCreateOpsBadAssignee", "ivy", "POST", "/projects/ops/tasks", `{"title":"x","assignee":"ben"}`, 400},
		{"IvyCreateOpsViewerAssignee", "ivy", "POST", "/projects/ops/tasks", `{"title":"x","assignee":"ann"}`, 400},
	}
	for _, c := range regDeny {
		c := c
		t.Run("REG_"+c.n, func(t *testing.T) {
			h := newServer()
			before := zgSnap(t, h)
			code, _ := zgDo(h, c.user, c.method, c.path, c.body)
			if code != c.w {
				t.Errorf("got %d want %d", code, c.w)
			}
			if d := zgDiff(before, zgSnap(t, h)); d != "" {
				t.Errorf("state changed: %s", d)
			}
		})
	}
	t.Run("REG_ManagerCreateUpdateDelete", func(t *testing.T) {
		h := newServer()
		code, body := zgDo(h, "ivy", "POST", "/projects/ops/tasks", `{"title":"n","assignee":"cat"}`)
		var tk zgTask
		json.Unmarshal([]byte(body), &tk)
		if code != 201 {
			t.Fatalf("create %d %s", code, body)
		}
		if c, b := zgDo(h, "ivy", "PATCH", "/projects/ops/tasks/"+tk.ID, `{"assignee":"ivy","status":"doing"}`); c != 200 {
			t.Fatalf("patch %d %s", c, b)
		}
		if c, _ := zgDo(h, "ivy", "DELETE", "/projects/ops/tasks/"+tk.ID, ""); c != 204 {
			t.Fatalf("delete %d", c)
		}
	})
	t.Run("REG_ContributorOwnUpdate", func(t *testing.T) {
		h := newServer()
		c, _ := zgDo(h, "ben", "PATCH", "/projects/web/tasks/t1", `{"status":"done"}`)
		if c != 200 || zgTasks(t, h)["t1"].Status != "done" {
			t.Fatalf("code=%d", c)
		}
	})
	t.Run("REG_ContributorCreateSelfAndOps", func(t *testing.T) {
		h := newServer()
		c1, _ := zgDo(h, "ben", "POST", "/projects/web/tasks", `{"title":"x","assignee":"ben"}`)
		c2, _ := zgDo(h, "cat", "POST", "/projects/ops/tasks", `{"title":"y"}`)
		if c1 != 201 || c2 != 201 {
			t.Fatalf("got %d %d", c1, c2)
		}
	})
	t.Run("REG_OtherOrgManagerGx", func(t *testing.T) {
		c, _ := zgDo(newServer(), "gus", "PATCH", "/projects/gx/tasks/t6", `{"status":"done"}`)
		if c != 200 {
			t.Fatalf("got %d", c)
		}
	})
	t.Run("REG_ProbeSanity", func(t *testing.T) {
		sig := zgSig(newServer())
		for _, p := range []string{"web", "ops", "gx"} {
			for _, u := range zgUsers {
				e := zgRoleSig[zgSeedRoles(p)[u]]
				if u == "eve" && p != "gx" {
					e = "403/403"
				}
				if sig[p+"/"+u] != e {
					t.Errorf("probe %s/%s = %s, want %s", p, u, sig[p+"/"+u], e)
				}
			}
		}
		if sig["legacy/ann"] != "409/409" || sig["legacy/hal"] != "404/404" {
			t.Errorf("legacy probes: ann=%s hal=%s", sig["legacy/ann"], sig["legacy/hal"])
		}
	})
	t.Run("REG_SeedUnchanged", func(t *testing.T) {
		s := zgTasks(t, newServer())
		if len(s) != 6 || s["t2"].Status != "doing" || s["t4"].Assignee != "cat" {
			t.Fatalf("seed unexpected: %+v", s)
		}
	})

	// ---------- FUNC ----------
	webIntoOps := zgWith(zgSeedRoles("ops"), "ben", "contributor", "dan", "viewer")
	t.Run("FUNC_IvyImportsWebIntoOps", func(t *testing.T) {
		zgImportOK(t, newServer(), "ivy", "ops", "web", webIntoOps)
	})
	t.Run("FUNC_ExistingMembersKeepRole_AnnStaysViewer", func(t *testing.T) {
		h := newServer()
		zgImportOK(t, h, "ivy", "ops", "web", webIntoOps)
		// ann is manager of web but viewer of ops: she must still be unable to write ops
		if c, b := zgDo(h, "ann", "POST", "/projects/ops/tasks", `{"title":"x"}`); c != 403 {
			t.Errorf("ann (existing viewer) can now write ops: %d %s", c, b)
		}
		if c, _ := zgDo(h, "ann", "PATCH", "/projects/ops/tasks/t4", `{"assignee":"ivy"}`); c != 403 {
			t.Errorf("ann (existing viewer) can now reassign in ops: %d", c)
		}
	})
	t.Run("FUNC_ExistingContributorStaysContributor_Cat", func(t *testing.T) {
		h := newServer()
		zgImportOK(t, h, "ivy", "ops", "web", webIntoOps)
		if c, _ := zgDo(h, "cat", "PATCH", "/projects/ops/tasks/t4", `{"assignee":"ivy"}`); c != 403 {
			t.Errorf("cat (contributor) can reassign in ops: %d", c)
		}
	})
	t.Run("FUNC_ImportedContributorCanCreateOwnInOps", func(t *testing.T) {
		h := newServer()
		zgImportOK(t, h, "ivy", "ops", "web", webIntoOps)
		if c, b := zgDo(h, "ben", "POST", "/projects/ops/tasks", `{"title":"ben ops","assignee":"ben"}`); c != 201 {
			t.Errorf("imported contributor ben cannot create own task in ops: %d %s", c, b)
		}
		if c, _ := zgDo(h, "ben", "POST", "/projects/ops/tasks", `{"title":"x","assignee":"cat"}`); c != 403 {
			t.Errorf("imported contributor ben can assign others: %d", c)
		}
	})
	t.Run("FUNC_ImportedViewerCanReadNotWrite", func(t *testing.T) {
		h := newServer()
		if c, _ := zgDo(h, "dan", "GET", "/projects/ops/tasks", ""); c != 404 {
			t.Fatalf("precondition: dan reads ops before import: %d", c)
		}
		zgImportOK(t, h, "ivy", "ops", "web", webIntoOps)
		if c, _ := zgDo(h, "dan", "GET", "/projects/ops/tasks", ""); c != 200 {
			t.Errorf("imported viewer dan cannot read ops: %d", c)
		}
		if c, b := zgDo(h, "dan", "POST", "/projects/ops/tasks", `{"title":"x"}`); c != 403 {
			t.Errorf("imported viewer dan can write ops: %d %s", c, b)
		}
		if c, _ := zgDo(h, "dan", "PATCH", "/projects/ops/tasks/t4", `{"status":"done"}`); c != 403 {
			t.Errorf("imported viewer dan can patch in ops: %d", c)
		}
	})
	t.Run("FUNC_ImportedMemberUsableAsAssignee", func(t *testing.T) {
		h := newServer()
		zgImportOK(t, h, "ivy", "ops", "web", webIntoOps)
		if c, b := zgDo(h, "ivy", "POST", "/projects/ops/tasks", `{"title":"for ben","assignee":"ben"}`); c != 201 {
			t.Errorf("imported contributor not assignable in ops: %d %s", c, b)
		}
		if c, _ := zgDo(h, "ivy", "POST", "/projects/ops/tasks", `{"title":"for dan","assignee":"dan"}`); c != 400 {
			t.Errorf("imported viewer assignable in ops: %d", c)
		}
	})
	t.Run("FUNC_IvyImportsOpsIntoWebNoChange", func(t *testing.T) {
		zgImportOK(t, newServer(), "ivy", "web", "ops", zgSeedRoles("web"))
	})
	t.Run("FUNC_AnnImportsLegacyIntoWebNoChange", func(t *testing.T) {
		zgImportOK(t, newServer(), "ann", "web", "legacy", zgSeedRoles("web"))
	})
	t.Run("FUNC_ImportIsIdempotent", func(t *testing.T) {
		h := newServer()
		zgImportOK(t, h, "ivy", "ops", "web", webIntoOps)
		zgImportOK(t, h, "ivy", "ops", "web", webIntoOps)
	})
	t.Run("FUNC_SourceMembershipUnchanged", func(t *testing.T) {
		h := newServer()
		zgImportOK(t, h, "ivy", "ops", "web", webIntoOps) // verifies web roles via probes
	})
	t.Run("FUNC_ResponseListsOnlyTargetMembers", func(t *testing.T) {
		h := newServer()
		ms := zgImportOK(t, h, "ivy", "ops", "web", webIntoOps)
		if len(ms) != 5 {
			t.Errorf("got %d members, want 5", len(ms))
		}
	})
	t.Run("FUNC_ChainedImportDoesNotLeakThroughSource", func(t *testing.T) {
		h := newServer()
		zgImportOK(t, h, "ivy", "ops", "web", webIntoOps)
		// web now imports from ops: nothing new for web
		zgOverride = map[string]map[string]string{"ops": webIntoOps}
		defer func() { zgOverride = map[string]map[string]string{} }()
		zgImportOK(t, h, "ivy", "web", "ops", zgSeedRoles("web"))
	})

	// ---------- AUTHZ ----------
	authz := []struct {
		n string
		r zgReq
	}{
		// caller not manager of target
		{"ContributorOfTarget", zgIR("cat", "ops", "web")},
		{"ViewerOfTarget", zgIR("ann", "ops", "web")},
		{"OrgAdminTarget", zgIR("eve", "ops", "web")},
		{"OrgAdminTargetWeb", zgIR("eve", "web", "ops")},
		{"ContributorOfTargetWeb", zgIR("cat", "web", "ops")},
		{"ContributorBenImportsLegacyToWeb", zgIR("ben", "web", "legacy")},
		{"ViewerDanImportsToWeb", zgIR("dan", "web", "ops")},
		{"ViewerDanImportsWebToWeb", zgIR("dan", "web", "web")},
		{"NonMemberOfTarget", zgIR("ben", "ops", "web")},
		{"NonMemberHalTarget", zgIR("hal", "web", "ops")},
		{"NonMemberHalImportsOps", zgIR("hal", "ops", "web")},
		{"OtherOrgCallerTargetWeb", zgIR("gus", "web", "gx")},
		{"OtherOrgCallerTargetOps", zgIR("gus", "ops", "gx")},
		{"Anonymous", zgIR("", "ops", "web")},
		{"UnknownUser", zgIR("zed", "ops", "web")},
		// archived / unknown target
		{"ArchivedTargetManager", zgIR("ann", "legacy", "web")},
		{"ArchivedTargetContributor", zgIR("ben", "legacy", "web")},
		{"ArchivedTargetNonMember", zgIR("ivy", "legacy", "web")},
		{"UnknownTarget", zgIR("ivy", "nope", "web")},
		{"OtherOrgTarget", zgIR("ivy", "gx", "web")},
		// source not readable / cross-org
		{"CrossOrgSourceByIvy", zgIR("ivy", "ops", "gx")},
		{"CrossOrgSourceByIvyIntoWeb", zgIR("ivy", "web", "gx")},
		{"CrossOrgSourceByAnn", zgIR("ann", "web", "gx")},
		{"ArchivedSourceNonMemberIvyOps", zgIR("ivy", "ops", "legacy")},
		{"ArchivedSourceNonMemberIvyWeb", zgIR("ivy", "web", "legacy")},
		{"UnknownSource", zgIR("ivy", "ops", "nope")},
		{"UnknownSourceAnn", zgIR("ann", "web", "nope")},
		{"AcmeCallerIntoGlobexTarget", zgIR("gus", "gx", "web")},
		{"AcmeCallerIntoGlobexTargetOps", zgIR("gus", "gx", "ops")},
		{"GusImportsLegacy", zgIR("gus", "gx", "legacy")},
		{"AnnLegacyFromGx", zgIR("ann", "legacy", "gx")},
	}
	for _, c := range authz {
		c := c
		t.Run("AUTHZ_"+c.n, func(t *testing.T) { zgDenied(t, c.r, 0) })
	}
	t.Run("AUTHZ_CrossOrgUserNeverBecomesAcmeMember", func(t *testing.T) {
		h := newServer()
		for _, r := range []zgReq{zgIR("ivy", "ops", "gx"), zgIR("ivy", "web", "gx"), zgIR("ann", "web", "gx")} {
			zgPost(h, r)
		}
		for _, p := range []string{"web", "ops", "legacy"} {
			if c, _ := zgDo(h, "gus", "GET", "/projects/"+p+"/tasks", ""); c != 404 {
				t.Errorf("gus can read acme project %s after import attempts: %d", p, c)
			}
		}
		if c, _ := zgDo(h, "ivy", "POST", "/projects/ops/tasks", `{"title":"x","assignee":"gus"}`); c < 400 {
			t.Errorf("gus is assignable in ops after import attempts: %d", c)
		}
	})
	t.Run("AUTHZ_UnreadableSourceDoesNotLeakMembers", func(t *testing.T) {
		h := newServer()
		for _, r := range []zgReq{zgIR("ivy", "ops", "gx"), zgIR("ivy", "ops", "legacy"), zgIR("ann", "web", "gx")} {
			_, body := zgPost(h, r)
			if l := zgLeaks(r.user, body); len(l) > 0 {
				t.Errorf("leak: %s into %s exposes %v: %s", r.body, r.pid, l, body)
			}
		}
	})

	// ---------- ATOM ----------
	t.Run("ATOM_AllDeniedBatchChangesNothing", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		n := 0
		for _, c := range authz {
			code, _ := zgPost(h, c.r)
			if code >= 200 && code < 300 {
				n++
			}
		}
		after := zgSnap(t, h)
		if n > 0 {
			t.Errorf("unexpected acceptance: %d denied imports accepted", n)
		}
		if d := zgDiff(before, after); d != "" {
			t.Errorf("state changed: %s", d)
		}
	})
	t.Run("ATOM_DeniedBetweenAllowedOnlyAllowedLand", func(t *testing.T) {
		h := newServer()
		zgImportOK(t, h, "ivy", "ops", "web", webIntoOps)
		mid := zgSnap(t, h)
		for _, r := range []zgReq{zgIR("ivy", "ops", "gx"), zgIR("ivy", "ops", "legacy"), zgIR("cat", "ops", "web"), zgIR("ann", "legacy", "web"), zgIR("ivy", "ops", "nope")} {
			zgPost(h, r)
		}
		if d := zgDiff(mid, zgSnap(t, h)); d != "" {
			t.Errorf("state changed by denied imports: %s", d)
		}
	})
	t.Run("ATOM_ArchivedTargetAddsNobody", func(t *testing.T) {
		zgDenied(t, zgIR("ann", "legacy", "web"), 0)
	})

	// ---------- VALID ----------
	valid := []struct{ n, body string }{
		{"MalformedJSON", `{"fromProjectId":`},
		{"NotJSON", `web`},
		{"NumberSource", `{"fromProjectId":5}`},
		{"ArraySource", `{"fromProjectId":["web"]}`},
		{"NullBody", `null`},
		{"ArrayBody", `["web"]`},
	}
	for _, c := range valid {
		c := c
		t.Run("VALID_"+c.n, func(t *testing.T) { zgDenied(t, zgReq{"ivy", "ops", c.body}, 0) })
	}
	t.Run("VALID_EmptyBodyRejected", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		code, _ := zgDo(h, "ivy", "POST", "/projects/ops/members/import", "")
		if code >= 200 && code < 300 {
			t.Errorf("unexpected acceptance: empty body -> %d", code)
		}
		if d := zgDiff(before, zgSnap(t, h)); d != "" {
			t.Errorf("state changed: %s", d)
		}
	})
	t.Run("VALID_ExtraFieldsDoNotChangeRoles", func(t *testing.T) {
		h := newServer()
		code, _ := zgDo(h, "ivy", "POST", "/projects/ops/members/import", `{"fromProjectId":"web","role":"manager","userId":"hal","fromProjectIds":["gx"]}`)
		after := zgSnap(t, h)
		if code == 200 {
			for _, u := range []string{"hal", "gus"} {
				if after.sig["ops/"+u] != "404/404" {
					t.Errorf("%s became member of ops via extra fields: %s", u, after.sig["ops/"+u])
				}
			}
			if after.sig["ops/ben"] != "403/400" || after.sig["ops/dan"] != "403/403" {
				t.Errorf("extra field altered imported roles: ben=%s dan=%s", after.sig["ops/ben"], after.sig["ops/dan"])
			}
		}
	})

	// ---------- CONTRACT ----------
	for _, c := range []struct {
		n string
		r zgReq
		w int
	}{
		{"ContributorTarget403", zgIR("cat", "ops", "web"), 403},
		{"ViewerTarget403", zgIR("ann", "ops", "web"), 403},
		{"OrgAdminTarget403", zgIR("eve", "ops", "web"), 403},
		{"NonMemberTarget404", zgIR("ben", "ops", "web"), 404},
		{"CrossOrgCallerTarget404", zgIR("gus", "web", "gx"), 404},
		{"ArchivedTarget409", zgIR("ann", "legacy", "web"), 409},
		{"UnknownTarget404", zgIR("ivy", "nope", "web"), 404},
		{"CrossOrgSource404", zgIR("ivy", "ops", "gx"), 404},
		{"UnreadableSource404", zgIR("ivy", "ops", "legacy"), 404},
		{"UnknownSource404", zgIR("ivy", "ops", "nope"), 404},
		{"Anon401", zgIR("", "ops", "web"), 401},
		{"UnknownUser401", zgIR("zed", "ops", "web"), 401},
		{"MalformedBody400", zgReq{"ivy", "ops", `{"fromProjectId":`}, 400},
	} {
		c := c
		t.Run("CONTRACT_"+c.n, func(t *testing.T) { zgDenied(t, c.r, c.w) })
	}
	t.Run("CONTRACT_SuccessBodyShape", func(t *testing.T) {
		code, body := zgImport(newServer(), "ivy", "ops", "web")
		if code != 200 {
			t.Fatalf("status %d", code)
		}
		var raw []map[string]any
		if err := json.Unmarshal([]byte(body), &raw); err != nil {
			t.Fatalf("not an array of objects: %v (%s)", err, body)
		}
		for _, m := range raw {
			if _, ok := m["userId"]; !ok {
				t.Errorf("member lacks userId: %v", m)
			}
			if _, ok := m["role"]; !ok {
				t.Errorf("member lacks role: %v", m)
			}
		}
	})

	// ---------- INFO (log only) ----------
	logImport := func(t *testing.T, r zgReq) {
		h := newServer()
		before := zgSnap(t, h)
		code, body := zgPost(h, r)
		after := zgSnap(t, h)
		t.Logf("INFO POST %s/members/import %s as %q -> %d; diff=[%s]; body=%s", r.pid, r.body, r.user, code, zgDiff(before, after), strings.TrimSpace(body))
	}
	for _, c := range []struct {
		n string
		r zgReq
	}{
		{"SameProject_IvyWebWeb", zgIR("ivy", "web", "web")},
		{"SameProject_ContributorCatWebWeb", zgIR("cat", "web", "web")},
		{"EmptyFromProjectId", zgReq{"ivy", "ops", `{"fromProjectId":""}`}},
		{"MissingFromProjectId", zgReq{"ivy", "ops", `{}`}},
		{"NullFromProjectId", zgReq{"ivy", "ops", `{"fromProjectId":null}`}},
		{"GusImportsOwnProject_GxGx", zgIR("gus", "gx", "gx")},
		{"OrderUnreadableSourceViewerTarget_AnnOpsFromGx", zgIR("ann", "ops", "gx")},
		{"OrderUnknownSourceViewerTarget_AnnOpsFromNope", zgIR("ann", "ops", "nope")},
		{"OrderUnknownSourceNonMemberTarget_BenOpsFromNope", zgIR("ben", "ops", "nope")},
		{"OrderArchivedTargetUnreadableSource_AnnLegacyFromGx", zgIR("ann", "legacy", "gx")},
		{"OrderArchivedTargetUnknownSource_AnnLegacyFromNope", zgIR("ann", "legacy", "nope")},
		{"OrderArchivedTargetContributor_BenLegacyFromWeb", zgIR("ben", "legacy", "web")},
		{"ArchivedSourceReadableByManager_AnnWebFromLegacy", zgIR("ann", "web", "legacy")},
		{"ArchivedSourceOrgAdminTarget_EveWebFromLegacy", zgIR("eve", "web", "legacy")},
	} {
		c := c
		t.Run("INFO_"+c.n, func(t *testing.T) { logImport(t, c.r) })
	}
	t.Run("INFO_ResponseOrder", func(t *testing.T) {
		code, body := zgImport(newServer(), "ivy", "ops", "web")
		var ms []zgMember
		json.Unmarshal([]byte(body), &ms)
		ids := make([]string, len(ms))
		for i, m := range ms {
			ids[i] = m.UserID
		}
		t.Logf("INFO response order (%d): %v sorted=%v", code, ids, sort.StringsAreSorted(ids))
	})
}
