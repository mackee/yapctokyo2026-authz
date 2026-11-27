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

// Hidden grader for task 029 (X-On-Behalf-Of delegation). Uses only newServer() and HTTP.

type zgTask struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	Assignee  string `json:"assignee"`
}

// zgDo: user "" = no X-User. obo: nil = no header; otherwise the header value (may be "").
func zgDoH(h http.Handler, user string, obo *string, method, path, body string) (int, string) {
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
	if obo != nil {
		req.Header.Set("X-On-Behalf-Of", *obo)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func zgDo(h http.Handler, user, method, path, body string) (int, string) {
	return zgDoH(h, user, nil, method, path, body)
}

func zgOn(h http.Handler, user, obo, method, path, body string) (int, string) {
	return zgDoH(h, user, &obo, method, path, body)
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

// zgLeaks: titles in body of tasks the REAL caller (X-User) cannot read.
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

// zgSig observes roles with plain (header-less) probes.
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

type zgReq struct {
	user, obo, method, path, body string
}

// zgDenied: write rejected (non-2xx), no state change, no leak; want!=0 checks exact status.
func zgDenied(t *testing.T, r zgReq, want int) {
	t.Helper()
	h := newServer()
	before := zgSnap(t, h)
	code, resp := zgOn(h, r.user, r.obo, r.method, r.path, r.body)
	after := zgSnap(t, h)
	if code >= 200 && code < 300 {
		t.Errorf("unexpected acceptance: %s %s as %q on behalf of %q -> %d body=%s", r.method, r.path, r.user, r.obo, code, resp)
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

// zgNoLeak: read with header; any status is fine but no data the real caller cannot read.
func zgNoLeak(t *testing.T, r zgReq) {
	t.Helper()
	h := newServer()
	code, resp := zgOn(h, r.user, r.obo, r.method, r.path, r.body)
	if l := zgLeaks(r.user, resp); len(l) > 0 {
		t.Errorf("leak (status %d): %s as %q on behalf of %q exposes titles %v: %s", code, r.path, r.user, r.obo, l, resp)
	}
}

func zgOK(t *testing.T, h http.Handler, user, obo, method, path, body string, want int) string {
	t.Helper()
	code, resp := zgOn(h, user, obo, method, path, body)
	if code != want {
		t.Fatalf("%s %s as %s on behalf of %s -> %d, want %d body=%s", method, path, user, obo, code, want, resp)
	}
	return resp
}

func zgTaskNow(t *testing.T, h http.Handler, id string) zgTask {
	t.Helper()
	return zgTasks(t, h)[id]
}

func TestGrader(t *testing.T) {
	// ---------- REG ----------
	for _, c := range []struct {
		n, user, method, path, body string
		want                        int
	}{
		{"ListWeb", "ann", "GET", "/projects/web/tasks", "", 200},
		{"ListWebNonMember404", "hal", "GET", "/projects/web/tasks", "", 404},
		{"ListAnon401", "", "GET", "/projects/web/tasks", "", 401},
		{"PatchContributorOthers403", "ben", "PATCH", "/projects/web/tasks/t2", `{"status":"done"}`, 403},
		{"PatchContributorOwn", "ben", "PATCH", "/projects/web/tasks/t1", `{"status":"doing"}`, 200},
		{"PatchContributorReassign403", "ben", "PATCH", "/projects/web/tasks/t1", `{"assignee":"cat"}`, 403},
		{"PatchManagerAny", "ivy", "PATCH", "/projects/web/tasks/t2", `{"status":"done"}`, 200},
		{"PatchViewer403", "dan", "PATCH", "/projects/web/tasks/t1", `{"status":"done"}`, 403},
		{"PatchArchived409", "ann", "PATCH", "/projects/legacy/tasks/t5", `{"status":"done"}`, 409},
		{"DeleteContributor403", "ben", "DELETE", "/projects/web/tasks/t1", "", 403},
		{"DeleteManager", "ivy", "DELETE", "/projects/web/tasks/t3", "", 204},
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
			"web/hal": "404/404", "web/ivy": "400/400", "ops/ann": "403/403", "ops/cat": "403/400",
			"ops/ivy": "400/400", "ops/ben": "404/404", "legacy/ann": "409/409", "legacy/ben": "409/409",
		}
		for k, v := range want {
			if sig[k] != v {
				t.Errorf("probe %s = %s, want %s", k, sig[k], v)
			}
		}
	})
	t.Run("REG_NoHeaderLeavesIdentityAlone", func(t *testing.T) {
		h := newServer()
		zgOK(t, h, "ben", "", "PATCH", "/projects/web/tasks/t1", `{"status":"doing"}`, 200)
		code, _ := zgOn(h, "ben", "", "PATCH", "/projects/web/tasks/t2", `{"status":"done"}`)
		_ = code // empty header semantics are INFO only
		if tk := zgTaskNow(t, h, "t2"); tk.Status != "doing" {
			t.Errorf("ben's empty-header request changed cat's task: %+v", tk)
		}
	})

	// ---------- FUNC ----------
	t.Run("FUNC_ManagerActsForContributorOwnTask", func(t *testing.T) {
		h := newServer()
		zgOK(t, h, "ivy", "ben", "PATCH", "/projects/web/tasks/t1", `{"status":"done"}`, 200)
		want := zgSeed["t1"]
		want.Status = "done"
		if got := zgTaskNow(t, h, "t1"); got != want {
			t.Errorf("t1 = %+v, want %+v", got, want)
		}
	})
	t.Run("FUNC_ManagerActsForContributorInOps", func(t *testing.T) {
		h := newServer()
		zgOK(t, h, "ivy", "cat", "PATCH", "/projects/ops/tasks/t4", `{"status":"doing"}`, 200)
		if got := zgTaskNow(t, h, "t4"); got.Status != "doing" || got.Assignee != "cat" {
			t.Errorf("t4 = %+v", got)
		}
	})
	t.Run("FUNC_OtherManagerAnnActsForCatInWeb", func(t *testing.T) {
		h := newServer()
		zgOK(t, h, "ann", "cat", "PATCH", "/projects/web/tasks/t2", `{"status":"done"}`, 200)
		if got := zgTaskNow(t, h, "t2"); got.Status != "done" {
			t.Errorf("t2 = %+v", got)
		}
	})
	t.Run("FUNC_ActsAsTargetNotAsManager_OthersTask", func(t *testing.T) {
		h := newServer()
		code, _ := zgOn(h, "ivy", "ben", "PATCH", "/projects/web/tasks/t2", `{"status":"done"}`)
		if code != 403 {
			t.Errorf("status %d, want 403 (acting as ben, t2 is cat's)", code)
		}
		zgOK(t, h, "ivy", "ben", "PATCH", "/projects/web/tasks/t1", `{"status":"done"}`, 200)
		if got := zgTaskNow(t, h, "t2"); got != zgSeed["t2"] {
			t.Errorf("t2 changed: %+v", got)
		}
	})
	t.Run("FUNC_ActsAsTargetNotAsManager_NoReassign", func(t *testing.T) {
		h := newServer()
		zgOK(t, h, "ivy", "ben", "PATCH", "/projects/web/tasks/t1", `{"status":"doing"}`, 200)
		code, _ := zgOn(h, "ivy", "ben", "PATCH", "/projects/web/tasks/t1", `{"assignee":"cat"}`)
		if code != 403 {
			t.Errorf("status %d, want 403", code)
		}
		if got := zgTaskNow(t, h, "t1"); got.Assignee != "ben" || got.Status != "doing" {
			t.Errorf("t1 = %+v", got)
		}
	})
	t.Run("FUNC_ActsAsTargetNotAsManager_NoDelete", func(t *testing.T) {
		h := newServer()
		zgOK(t, h, "ivy", "ben", "PATCH", "/projects/web/tasks/t1", `{"status":"doing"}`, 200)
		code, _ := zgOn(h, "ivy", "ben", "DELETE", "/projects/web/tasks/t3", "")
		if code >= 200 && code < 300 {
			t.Errorf("delete as ben accepted: %d", code)
		}
		if _, ok := zgTasks(t, h)["t3"]; !ok {
			t.Errorf("t3 vanished")
		}
	})
	t.Run("FUNC_ActsAsViewerTargetIsReadOnly", func(t *testing.T) {
		h := newServer()
		zgOK(t, h, "ivy", "ben", "PATCH", "/projects/web/tasks/t1", `{"status":"doing"}`, 200)
		code, _ := zgOn(h, "ivy", "dan", "PATCH", "/projects/web/tasks/t1", `{"status":"done"}`)
		if code >= 200 && code < 300 {
			t.Errorf("acting as viewer dan wrote: %d", code)
		}
		if got := zgTaskNow(t, h, "t1"); got.Status != "doing" {
			t.Errorf("t1 = %+v", got)
		}
	})
	t.Run("FUNC_NoStickyIdentity", func(t *testing.T) {
		h := newServer()
		zgOK(t, h, "ivy", "ben", "PATCH", "/projects/web/tasks/t1", `{"status":"done"}`, 200)
		zgOK(t, h, "ben", "", "PATCH", "/projects/web/tasks/t2", `{"status":"done"}`, 403)
		zgOK(t, h, "ivy", "", "PATCH", "/projects/web/tasks/t2", `{"status":"done"}`, 200)
		sig := zgSig(h)
		if sig["web/ben"] != "403/400" || sig["web/ivy"] != "400/400" {
			t.Errorf("roles changed after delegation: ben=%s ivy=%s", sig["web/ben"], sig["web/ivy"])
		}
	})
	t.Run("FUNC_DelegatedWritesDoNotTouchActorsOtherData", func(t *testing.T) {
		h := newServer()
		before := zgTasks(t, h)
		zgOK(t, h, "ivy", "cat", "PATCH", "/projects/web/tasks/t2", `{"status":"done"}`, 200)
		after := zgTasks(t, h)
		for id, b := range before {
			a := after[id]
			if id == "t2" {
				b.Status = "done"
			}
			if a != b {
				t.Errorf("task %s: %+v -> %+v", id, before[id], a)
			}
		}
	})

	// ---------- AUTHZ: non-managers cannot delegate ----------
	for _, c := range []struct {
		n string
		r zgReq
	}{
		{"BenForCatInWeb", zgReq{"ben", "cat", "PATCH", "/projects/web/tasks/t2", `{"status":"done"}`}},
		{"BenForBenHimself", zgReq{"ben", "ben", "PATCH", "/projects/web/tasks/t1", `{"assignee":"cat"}`}},
		{"BenForIvyReassign", zgReq{"ben", "ivy", "PATCH", "/projects/web/tasks/t1", `{"assignee":"cat"}`}},
		{"BenForIvyDelete", zgReq{"ben", "ivy", "DELETE", "/projects/web/tasks/t3", ""}},
		{"CatForIvyInOpsReassign", zgReq{"cat", "ivy", "PATCH", "/projects/ops/tasks/t4", `{"assignee":""}`}},
		{"DanForAnn", zgReq{"dan", "ann", "PATCH", "/projects/web/tasks/t3", `{"status":"done"}`}},
		{"DanForBen", zgReq{"dan", "ben", "PATCH", "/projects/web/tasks/t1", `{"status":"done"}`}},
		{"EveOrgAdminForBen", zgReq{"eve", "ben", "PATCH", "/projects/web/tasks/t1", `{"status":"done"}`}},
		{"EveOrgAdminForAnn", zgReq{"eve", "ann", "PATCH", "/projects/web/tasks/t3", `{"status":"done"}`}},
		{"EveOrgAdminForIvy", zgReq{"eve", "ivy", "DELETE", "/projects/web/tasks/t3", ""}},
		{"HalNonMemberForBen", zgReq{"hal", "ben", "PATCH", "/projects/web/tasks/t1", `{"status":"done"}`}},
		{"GusOtherOrgForAnn", zgReq{"gus", "ann", "PATCH", "/projects/web/tasks/t3", `{"status":"done"}`}},
		{"GusOtherOrgForBen", zgReq{"gus", "ben", "PATCH", "/projects/web/tasks/t1", `{"status":"done"}`}},
	} {
		c := c
		t.Run("AUTHZ_NonManagerDelegates_"+c.n, func(t *testing.T) { zgDenied(t, c.r, 0) })
	}

	// ---------- AUTHZ: per-project scope / escalation ----------
	for _, c := range []struct {
		n string
		r zgReq
	}{
		// ann is a manager of web/legacy but only a viewer in ops
		{"AnnForCatInOps", zgReq{"ann", "cat", "PATCH", "/projects/ops/tasks/t4", `{"status":"done"}`}},
		{"AnnForIvyInOps", zgReq{"ann", "ivy", "PATCH", "/projects/ops/tasks/t4", `{"assignee":""}`}},
		{"AnnForIvyInOpsDelete", zgReq{"ann", "ivy", "DELETE", "/projects/ops/tasks/t4", ""}},
		{"AnnForCatInOpsCreate", zgReq{"ann", "cat", "POST", "/projects/ops/tasks", `{"title":"sneaky","assignee":"cat"}`}},
		// ivy is not a legacy member
		{"IvyForBenInLegacy", zgReq{"ivy", "ben", "PATCH", "/projects/legacy/tasks/t5", `{"status":"done"}`}},
		{"IvyForAnnInLegacy", zgReq{"ivy", "ann", "PATCH", "/projects/legacy/tasks/t5", `{"status":"done"}`}},
		{"IvyForBenInLegacyCreate", zgReq{"ivy", "ben", "POST", "/projects/legacy/tasks", `{"title":"sneaky"}`}},
		// ben is not in ops: delegating for ben must not reach ops
		{"IvyForBenInOps", zgReq{"ivy", "ben", "PATCH", "/projects/ops/tasks/t4", `{"status":"done"}`}},
		{"IvyForBenInOpsCreate", zgReq{"ivy", "ben", "POST", "/projects/ops/tasks", `{"title":"sneaky"}`}},
		// target is not a member / other org
		{"IvyForGusInGx", zgReq{"ivy", "gus", "PATCH", "/projects/gx/tasks/t6", `{"status":"done"}`}},
		{"IvyForGusInWeb", zgReq{"ivy", "gus", "PATCH", "/projects/web/tasks/t1", `{"status":"done"}`}},
		{"IvyForHalInWeb", zgReq{"ivy", "hal", "PATCH", "/projects/web/tasks/t3", `{"status":"done"}`}},
		{"IvyForHalInWebCreate", zgReq{"ivy", "hal", "POST", "/projects/web/tasks", `{"title":"sneaky"}`}},
		{"IvyForEveOrgAdminInWeb", zgReq{"ivy", "eve", "PATCH", "/projects/web/tasks/t3", `{"status":"done"}`}},
		// delegated identity is bounded by the target's own rights
		{"IvyForBenOnCatsTask", zgReq{"ivy", "ben", "PATCH", "/projects/web/tasks/t2", `{"status":"done"}`}},
		{"IvyForBenReassign", zgReq{"ivy", "ben", "PATCH", "/projects/web/tasks/t1", `{"assignee":"cat"}`}},
		{"IvyForBenDelete", zgReq{"ivy", "ben", "DELETE", "/projects/web/tasks/t3", ""}},
		{"IvyForDanWrite", zgReq{"ivy", "dan", "PATCH", "/projects/web/tasks/t1", `{"status":"done"}`}},
		{"IvyForAnnInOpsViewer", zgReq{"ivy", "ann", "PATCH", "/projects/ops/tasks/t4", `{"status":"done"}`}},
		{"IvyForCatInOpsDelete", zgReq{"ivy", "cat", "DELETE", "/projects/ops/tasks/t4", ""}},
		{"IvyForCatReassignInOps", zgReq{"ivy", "cat", "PATCH", "/projects/ops/tasks/t4", `{"assignee":""}`}},
		{"IvyForCatOnBensTask", zgReq{"ivy", "cat", "PATCH", "/projects/web/tasks/t1", `{"status":"done"}`}},
	} {
		c := c
		t.Run("AUTHZ_Scope_"+c.n, func(t *testing.T) { zgDenied(t, c.r, 0) })
	}

	// ---------- AUTHZ: header confusion ----------
	for _, c := range []struct {
		n string
		r zgReq
	}{
		{"UnknownTargetUser", zgReq{"ivy", "zed", "PATCH", "/projects/web/tasks/t1", `{"status":"done"}`}},
		{"UnknownTargetUserDelete", zgReq{"ivy", "zed", "DELETE", "/projects/web/tasks/t3", ""}},
		{"TargetIdWithWhitespace", zgReq{"ivy", " ben ", "PATCH", "/projects/ops/tasks/t4", `{"status":"done"}`}},
		{"NoXUserWithHeader", zgReq{"", "ben", "PATCH", "/projects/web/tasks/t1", `{"status":"done"}`}},
		{"NoXUserWithManagerHeader", zgReq{"", "ivy", "DELETE", "/projects/web/tasks/t3", ""}},
		{"UnknownXUserWithHeader", zgReq{"zed", "ben", "PATCH", "/projects/web/tasks/t1", `{"status":"done"}`}},
		{"HeaderNamesOtherUsersManagerForNonManager", zgReq{"cat", "ivy", "PATCH", "/projects/web/tasks/t1", `{"assignee":"cat"}`}},
	} {
		c := c
		t.Run("AUTHZ_Header_"+c.n, func(t *testing.T) { zgDenied(t, c.r, 0) })
	}
	t.Run("AUTHZ_Header_NoXUserWithHeader401", func(t *testing.T) {
		h := newServer()
		code, _ := zgOn(h, "", "ben", "GET", "/projects/web/tasks", "")
		if code != 401 {
			t.Errorf("anonymous + header on read -> %d, want 401", code)
		}
	})

	// ---------- AUTHZ: reads through the header must not leak ----------
	for _, c := range []struct {
		n string
		r zgReq
	}{
		{"IvyForBenListLegacy", zgReq{"ivy", "ben", "GET", "/projects/legacy/tasks", ""}},
		{"IvyForBenGetLegacyTask", zgReq{"ivy", "ben", "GET", "/projects/legacy/tasks/t5", ""}},
		{"IvyForAnnListLegacy", zgReq{"ivy", "ann", "GET", "/projects/legacy/tasks", ""}},
		{"IvyForEveListLegacy", zgReq{"ivy", "eve", "GET", "/projects/legacy/tasks", ""}},
		{"IvyForGusListGx", zgReq{"ivy", "gus", "GET", "/projects/gx/tasks", ""}},
		{"IvyForGusGetGxTask", zgReq{"ivy", "gus", "GET", "/projects/gx/tasks/t6", ""}},
		{"BenForAnnListOps", zgReq{"ben", "ann", "GET", "/projects/ops/tasks", ""}},
		{"BenForCatGetOpsTask", zgReq{"ben", "cat", "GET", "/projects/ops/tasks/t4", ""}},
		{"DanForCatListOps", zgReq{"dan", "cat", "GET", "/projects/ops/tasks", ""}},
		{"DanForAnnListLegacy", zgReq{"dan", "ann", "GET", "/projects/legacy/tasks", ""}},
		{"HalForBenListWeb", zgReq{"hal", "ben", "GET", "/projects/web/tasks", ""}},
		{"GusForAnnListWeb", zgReq{"gus", "ann", "GET", "/projects/web/tasks", ""}},
		{"GusForAnnGetWebTask", zgReq{"gus", "ann", "GET", "/projects/web/tasks/t1", ""}},
		{"AnnForCatListOpsStillOk", zgReq{"ann", "cat", "GET", "/projects/ops/tasks", ""}},
	} {
		c := c
		t.Run("AUTHZ_ReadNoLeak_"+c.n, func(t *testing.T) { zgNoLeak(t, c.r) })
	}
	t.Run("AUTHZ_ReadNoAcceptance_IvyForBenLegacy", func(t *testing.T) {
		// ivy is not a legacy member: whatever the header means, legacy data must not be served.
		h := newServer()
		code, body := zgOn(h, "ivy", "ben", "GET", "/projects/legacy/tasks", "")
		if code >= 200 && code < 300 {
			t.Errorf("2xx serving legacy to non-member ivy via header: %s", body)
		}
	})
	t.Run("AUTHZ_ReadNoAcceptance_GusForAnnWeb", func(t *testing.T) {
		h := newServer()
		code, body := zgOn(h, "gus", "ann", "GET", "/projects/web/tasks", "")
		if code >= 200 && code < 300 {
			t.Errorf("2xx serving web to globex user gus via header: %s", body)
		}
	})

	// ---------- CONTRACT (exact statuses) ----------
	for _, c := range []struct {
		n string
		r zgReq
		w int
	}{
		{"BenForCat_403", zgReq{"ben", "cat", "PATCH", "/projects/web/tasks/t2", `{"status":"done"}`}, 403},
		{"DanForAnn_403", zgReq{"dan", "ann", "PATCH", "/projects/web/tasks/t3", `{"status":"done"}`}, 403},
		{"AnnForCatInOps_403", zgReq{"ann", "cat", "PATCH", "/projects/ops/tasks/t4", `{"status":"done"}`}, 403},
		{"IvyForBenOnCatsTask_403", zgReq{"ivy", "ben", "PATCH", "/projects/web/tasks/t2", `{"status":"done"}`}, 403},
		{"IvyForBenReassign_403", zgReq{"ivy", "ben", "PATCH", "/projects/web/tasks/t1", `{"assignee":"cat"}`}, 403},
		{"IvyForBenDelete_403", zgReq{"ivy", "ben", "DELETE", "/projects/web/tasks/t3", ""}, 403},
		{"NoXUserWithHeader_401", zgReq{"", "ben", "PATCH", "/projects/web/tasks/t1", `{"status":"done"}`}, 401},
	} {
		c := c
		t.Run("CONTRACT_"+c.n, func(t *testing.T) { zgDenied(t, c.r, c.w) })
	}

	// ---------- INFO (log only) ----------
	t.Run("INFO_Observations", func(t *testing.T) {
		h := newServer()
		for _, c := range []zgReq{
			{"ivy", "zed", "PATCH", "/projects/web/tasks/t1", `{"status":"done"}`},
			{"ivy", "hal", "PATCH", "/projects/web/tasks/t3", `{"status":"done"}`},
			{"ivy", "ivy", "PATCH", "/projects/web/tasks/t3", `{"status":"done"}`},
			{"ivy", "ann", "PATCH", "/projects/web/tasks/t3", `{"title":"by-ann"}`},
			{"ann", "ivy", "PATCH", "/projects/web/tasks/t3", `{"title":"by-ivy"}`},
			{"ivy", "ben", "GET", "/projects/web/tasks", ""},
			{"ivy", "BEN", "PATCH", "/projects/web/tasks/t1", `{"status":"doing"}`},
			{"ben", "", "PATCH", "/projects/web/tasks/t1", `{"status":"doing"}`},
		} {
			code, body := zgOn(h, c.user, c.obo, c.method, c.path, c.body)
			if len(body) > 160 {
				body = body[:160] + "..."
			}
			t.Logf("INFO %s on behalf of %q %s %s -> %d %s", c.user, c.obo, c.method, c.path, code, strings.TrimSpace(body))
		}
		_, body := zgOn(newServer(), "ivy", "ben", "PATCH", "/projects/web/tasks/t1", `{"status":"done"}`)
		t.Logf("INFO delegated response shows actor/identity: %v body=%s", strings.Contains(body, "ivy"), strings.TrimSpace(body))
	})
}
