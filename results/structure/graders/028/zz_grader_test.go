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

// Hidden grader for task 028 (access requests). Uses only newServer() and HTTP.
// Roles are observed through non-mutating probes on existing endpoints (see zgSig).

type zgTask struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	Assignee  string `json:"assignee"`
}

type zgAR struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	UserID    string `json:"userId"`
	Status    string `json:"status"`
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

func zgMust(t *testing.T, h http.Handler, user, method, path, body string, want int) string {
	t.Helper()
	code, resp := zgDo(h, user, method, path, body)
	if code != want {
		t.Fatalf("%s %s as %s -> %d, want %d body=%s", method, path, user, code, want, resp)
	}
	return resp
}

func zgIs2xx(c int) bool { return c >= 200 && c < 300 }

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

// zgSig observes every user's role without mutating anything:
//
//	PATCH <task> {"assignee":"zzz"}: 404 non-member, 403 viewer/org admin/contributor, 400 manager (409 if archived, 403 viewer)
//	POST  tasks  {}               : 404 non-member, 403 viewer/org admin, 400 contributor/manager (409 if archived)
//	gx/<user>: GET gx tasks status (200 only for gus)
//
// "404/404" = no access, "403/403" = viewer (or org admin), "403/400" = contributor, "400/400" = manager.
func zgSig(h http.Handler) map[string]string {
	out := map[string]string{}
	for _, p := range []struct{ pid, tid string }{{"web", "t3"}, {"ops", "t4"}, {"legacy", "t5"}} {
		for _, u := range zgUsers {
			c1, _ := zgDo(h, u, "PATCH", "/projects/"+p.pid+"/tasks/"+p.tid, `{"assignee":"zzz"}`)
			c2, _ := zgDo(h, u, "POST", "/projects/"+p.pid+"/tasks", `{}`)
			out[p.pid+"/"+u] = fmt.Sprintf("%d/%d", c1, c2)
		}
	}
	for _, u := range zgUsers {
		c, _ := zgDo(h, u, "GET", "/projects/gx/tasks", "")
		out["gx/"+u] = fmt.Sprint(c)
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

func zgDiffAllow(a, b zgState, allow ...string) string {
	al := map[string]bool{}
	for _, k := range allow {
		al[k] = true
	}
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
		if y := b.sig[k]; x != y && !al[k] {
			d = append(d, fmt.Sprintf("role-probe %s %s -> %s", k, x, y))
		}
	}
	sort.Strings(d)
	return strings.Join(d, "; ")
}

func zgDiff(a, b zgState) string { return zgDiffAllow(a, b) }

func zgApprovePath(pid, rid string) string {
	return "/projects/" + pid + "/access-requests/" + rid + "/approve"
}

// zgFile files an access request; id is "" when the response has none.
func zgFile(h http.Handler, user, pid, body string) (int, string, string) {
	code, resp := zgDo(h, user, "POST", "/projects/"+pid+"/access-requests", body)
	var r zgAR
	if zgIs2xx(code) {
		json.Unmarshal([]byte(resp), &r)
	}
	return code, resp, r.ID
}

func zgMustFile(t *testing.T, h http.Handler, user, pid string) string {
	t.Helper()
	code, resp, id := zgFile(h, user, pid, "")
	if code != 201 || id == "" {
		t.Fatalf("POST /projects/%s/access-requests as %s -> %d body=%s", pid, user, code, resp)
	}
	return id
}

func zgApprove(h http.Handler, user, pid, rid string) (int, string) {
	return zgDo(h, user, "POST", zgApprovePath(pid, rid), "")
}

func zgMustApprove(t *testing.T, h http.Handler, user, pid, rid string) zgAR {
	t.Helper()
	code, resp := zgApprove(h, user, pid, rid)
	if code != 200 {
		t.Fatalf("approve %s/%s as %s -> %d body=%s", pid, rid, user, code, resp)
	}
	var r zgAR
	if err := json.Unmarshal([]byte(resp), &r); err != nil {
		t.Fatalf("approve response not JSON object: %v body=%s", err, resp)
	}
	return r
}

func zgParseList(body string) ([]zgAR, bool) {
	var a []zgAR
	if json.Unmarshal([]byte(body), &a) == nil {
		return a, true
	}
	var m map[string]json.RawMessage
	if json.Unmarshal([]byte(body), &m) == nil {
		for _, v := range m {
			var b []zgAR
			if json.Unmarshal(v, &b) == nil {
				return b, true
			}
		}
	}
	return nil, false
}

func zgList(h http.Handler, user, pid string) (int, string, []zgAR, bool) {
	code, body := zgDo(h, user, "GET", "/projects/"+pid+"/access-requests", "")
	l, ok := zgParseList(body)
	return code, body, l, ok && zgIs2xx(code)
}

type zgAC struct {
	n, requester, reqPid, actor, apprPid string
	strict                               bool
	allow                                []string
}

// zgApproveDenied: requester files a request, actor tries to approve it via apprPid.
// strict: must be non-2xx. Always: no state change (tasks and every role probe, except allow), no leak.
func zgApproveDenied(t *testing.T, c zgAC) {
	t.Helper()
	h := newServer()
	before := zgSnap(t, h)
	_, _, rid := zgFile(h, c.requester, c.reqPid, "")
	if rid == "" {
		rid = "r1"
	}
	code, resp := zgApprove(h, c.actor, c.apprPid, rid)
	after := zgSnap(t, h)
	if c.strict && zgIs2xx(code) {
		t.Errorf("unexpected acceptance: %s requested %s, %q approved via %s -> %d body=%s", c.requester, c.reqPid, c.actor, c.apprPid, code, resp)
	}
	if d := zgDiffAllow(before, after, c.allow...); d != "" {
		t.Errorf("state changed (approve -> %d): %s", code, d)
	}
	if l := zgLeaks(c.actor, resp); len(l) > 0 {
		t.Errorf("leak: response exposes titles %v: %s", l, resp)
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
		{"ListAnon401", "", "GET", "/projects/web/tasks", "", 401},
		{"GetTask", "dan", "GET", "/projects/web/tasks/t1", "", 200},
		{"CreateViewer403", "dan", "POST", "/projects/web/tasks", `{"title":"x"}`, 403},
		{"CreateNonMember404", "hal", "POST", "/projects/web/tasks", `{"title":"x"}`, 404},
		{"CreateArchived409", "ann", "POST", "/projects/legacy/tasks", `{"title":"x"}`, 409},
		{"CreateOK", "ann", "POST", "/projects/web/tasks", `{"title":"x","assignee":"ben"}`, 201},
		{"CreateBadAssignee400", "ann", "POST", "/projects/web/tasks", `{"title":"x","assignee":"hal"}`, 400},
		{"PatchContributorOthers403", "ben", "PATCH", "/projects/web/tasks/t2", `{"status":"done"}`, 403},
		{"PatchContributorOwn", "ben", "PATCH", "/projects/web/tasks/t1", `{"status":"doing"}`, 200},
		{"PatchContributorReassign403", "ben", "PATCH", "/projects/web/tasks/t1", `{"assignee":"cat"}`, 403},
		{"PatchManagerReassign", "ivy", "PATCH", "/projects/web/tasks/t1", `{"assignee":"cat"}`, 200},
		{"PatchManagerBadAssignee400", "ivy", "PATCH", "/projects/web/tasks/t1", `{"assignee":"hal"}`, 400},
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
			"web/hal": "404/404", "web/ivy": "400/400", "web/gus": "404/404", "web/cat": "403/400",
			"ops/ann": "403/403", "ops/cat": "403/400", "ops/ivy": "400/400", "ops/ben": "404/404", "ops/hal": "404/404",
			"legacy/ann": "409/409", "legacy/ben": "409/409", "legacy/cat": "404/404", "legacy/hal": "404/404",
			"gx/gus": "200", "gx/ann": "404", "gx/hal": "404", "gx/eve": "404",
		}
		for k, v := range want {
			if sig[k] != v {
				t.Errorf("probe %s = %s, want %s", k, sig[k], v)
			}
		}
	})

	// ---------- FUNC ----------
	t.Run("FUNC_RequestCreatedShape", func(t *testing.T) {
		h := newServer()
		code, body, _ := zgFile(h, "hal", "web", "")
		if code != 201 {
			t.Fatalf("-> %d body=%s", code, body)
		}
		var r zgAR
		if err := json.Unmarshal([]byte(body), &r); err != nil {
			t.Fatalf("not a JSON object: %v body=%s", err, body)
		}
		if r.ID == "" || r.ProjectID != "web" || r.UserID != "hal" || r.Status != "pending" {
			t.Errorf("request = %+v, want id set, projectId web, userId hal, status pending", r)
		}
	})
	t.Run("FUNC_RequestWithEmptyJSONBody", func(t *testing.T) {
		h := newServer()
		code, body, _ := zgFile(h, "hal", "web", `{}`)
		if code != 201 {
			t.Errorf("-> %d body=%s", code, body)
		}
	})
	t.Run("FUNC_RequestDoesNotGrantAccess", func(t *testing.T) {
		h := newServer()
		zgMustFile(t, h, "hal", "web")
		if s := zgSig(h)["web/hal"]; s != "404/404" {
			t.Errorf("hal probe after only filing = %s, want 404/404", s)
		}
		zgMust(t, h, "hal", "GET", "/projects/web/tasks", "", 404)
	})
	t.Run("FUNC_ApproveByManagerIvy", func(t *testing.T) {
		h := newServer()
		id := zgMustFile(t, h, "hal", "web")
		r := zgMustApprove(t, h, "ivy", "web", id)
		if r.ID != id || r.ProjectID != "web" || r.UserID != "hal" || r.Status != "approved" {
			t.Errorf("approved request = %+v, want id %s web hal approved", r, id)
		}
		if s := zgSig(h)["web/hal"]; s != "403/400" {
			t.Errorf("hal probe after approval = %s, want contributor 403/400", s)
		}
		zgMust(t, h, "hal", "GET", "/projects/web/tasks", "", 200)
	})
	t.Run("FUNC_ApproveByManagerAnn", func(t *testing.T) {
		h := newServer()
		id := zgMustFile(t, h, "hal", "web")
		zgMustApprove(t, h, "ann", "web", id)
		if s := zgSig(h)["web/hal"]; s != "403/400" {
			t.Errorf("hal probe after approval = %s, want contributor 403/400", s)
		}
	})
	t.Run("FUNC_ApprovedHalCreatesTaskForSelf", func(t *testing.T) {
		h := newServer()
		id := zgMustFile(t, h, "hal", "web")
		zgMustApprove(t, h, "ivy", "web", id)
		body := zgMust(t, h, "hal", "POST", "/projects/web/tasks", `{"title":"hal task","assignee":"hal"}`, 201)
		var tk zgTask
		json.Unmarshal([]byte(body), &tk)
		if tk.Assignee != "hal" || tk.ProjectID != "web" {
			t.Errorf("created task %+v", tk)
		}
		zgMust(t, h, "hal", "PATCH", "/projects/web/tasks/"+tk.ID, `{"status":"doing"}`, 200)
	})
	t.Run("FUNC_ApprovedHalIsOnlyContributor", func(t *testing.T) {
		h := newServer()
		id := zgMustFile(t, h, "hal", "web")
		zgMustApprove(t, h, "ivy", "web", id)
		zgMust(t, h, "hal", "PATCH", "/projects/web/tasks/t1", `{"status":"done"}`, 403)
		zgMust(t, h, "hal", "DELETE", "/projects/web/tasks/t1", "", 403)
		zgMust(t, h, "hal", "PATCH", "/projects/web/tasks/t3", `{"assignee":"hal"}`, 403)
		zgMust(t, h, "hal", "GET", "/projects/ops/tasks", "", 404)
		zgMust(t, h, "hal", "GET", "/projects/legacy/tasks", "", 404)
		zgMust(t, h, "hal", "GET", "/projects/gx/tasks", "", 404)
	})
	t.Run("FUNC_ApprovedHalAssignableByManager", func(t *testing.T) {
		h := newServer()
		id := zgMustFile(t, h, "hal", "web")
		zgMustApprove(t, h, "ivy", "web", id)
		zgMust(t, h, "ivy", "PATCH", "/projects/web/tasks/t3", `{"assignee":"hal"}`, 200)
	})
	t.Run("FUNC_ApproveOnlyChangesRequester", func(t *testing.T) {
		h := newServer()
		id := zgMustFile(t, h, "hal", "web")
		before := zgSnap(t, h)
		zgMustApprove(t, h, "ivy", "web", id)
		after := zgSnap(t, h)
		if d := zgDiffAllow(before, after, "web/hal"); d != "" {
			t.Errorf("collateral change: %s", d)
		}
	})
	t.Run("FUNC_ApproveOpsRequest", func(t *testing.T) {
		h := newServer()
		id := zgMustFile(t, h, "hal", "ops")
		r := zgMustApprove(t, h, "ivy", "ops", id)
		if r.ProjectID != "ops" || r.UserID != "hal" || r.Status != "approved" {
			t.Errorf("approved request = %+v", r)
		}
		sig := zgSig(h)
		if sig["ops/hal"] != "403/400" || sig["web/hal"] != "404/404" {
			t.Errorf("hal probes ops=%s web=%s, want 403/400 and 404/404", sig["ops/hal"], sig["web/hal"])
		}
	})
	t.Run("FUNC_TwoRequestersApproveOne", func(t *testing.T) {
		h := newServer()
		eveID := zgMustFile(t, h, "eve", "web")
		halID := zgMustFile(t, h, "hal", "web")
		if eveID == halID {
			t.Errorf("two requests got the same id %q", eveID)
		}
		zgMustApprove(t, h, "ivy", "web", eveID)
		sig := zgSig(h)
		if sig["web/eve"] != "403/400" {
			t.Errorf("eve probe = %s, want contributor 403/400", sig["web/eve"])
		}
		if sig["web/hal"] != "404/404" {
			t.Errorf("hal probe = %s, want 404/404 (his request was not approved)", sig["web/hal"])
		}
		if _, _, l, ok := zgList(h, "ivy", "web"); ok {
			for _, r := range l {
				if r.UserID == "hal" && r.Status != "pending" {
					t.Errorf("hal's request status %q, want pending", r.Status)
				}
				if r.UserID == "eve" && r.Status != "approved" {
					t.Errorf("eve's request status %q, want approved", r.Status)
				}
			}
		}
	})
	t.Run("FUNC_ListEmpty", func(t *testing.T) {
		h := newServer()
		code, body := zgDo(h, "ivy", "GET", "/projects/web/access-requests", "")
		if code != 200 {
			t.Fatalf("-> %d body=%s", code, body)
		}
		l, ok := zgParseList(strings.TrimSpace(body))
		if !ok || len(l) != 0 {
			t.Errorf("body %s, want an empty list", body)
		}
	})
	t.Run("FUNC_ListShowsPending", func(t *testing.T) {
		h := newServer()
		id := zgMustFile(t, h, "hal", "web")
		code, body, l, ok := zgList(h, "ivy", "web")
		if code != 200 || !ok {
			t.Fatalf("-> %d body=%s", code, body)
		}
		if len(l) != 1 || l[0].ID != id || l[0].ProjectID != "web" || l[0].UserID != "hal" || l[0].Status != "pending" {
			t.Errorf("list = %+v, want one pending request %s by hal", l, id)
		}
	})
	t.Run("FUNC_ListByManagerAnn", func(t *testing.T) {
		h := newServer()
		zgMustFile(t, h, "hal", "web")
		code, body, l, ok := zgList(h, "ann", "web")
		if code != 200 || !ok || len(l) != 1 {
			t.Errorf("-> %d body=%s", code, body)
		}
	})
	t.Run("FUNC_ListAfterApproveShowsApproved", func(t *testing.T) {
		h := newServer()
		id := zgMustFile(t, h, "hal", "web")
		zgMustApprove(t, h, "ivy", "web", id)
		code, body, l, ok := zgList(h, "ivy", "web")
		if code != 200 || !ok {
			t.Fatalf("-> %d body=%s", code, body)
		}
		if len(l) != 1 || l[0].Status != "approved" {
			t.Errorf("list = %+v, want one approved request", l)
		}
	})
	t.Run("FUNC_ListScopedPerProject", func(t *testing.T) {
		h := newServer()
		zgMustFile(t, h, "hal", "web")
		zgMustFile(t, h, "eve", "ops")
		_, b1, l1, ok1 := zgList(h, "ivy", "web")
		_, b2, l2, ok2 := zgList(h, "ivy", "ops")
		if !ok1 || !ok2 {
			t.Fatalf("lists failed: %s | %s", b1, b2)
		}
		if len(l1) != 1 || l1[0].ProjectID != "web" || l1[0].UserID != "hal" {
			t.Errorf("web list = %+v", l1)
		}
		if len(l2) != 1 || l2[0].ProjectID != "ops" || l2[0].UserID != "eve" {
			t.Errorf("ops list = %+v", l2)
		}
	})
	t.Run("FUNC_ApproveWithJunkBody", func(t *testing.T) {
		h := newServer()
		id := zgMustFile(t, h, "hal", "web")
		zgMust(t, h, "ivy", "POST", zgApprovePath("web", id), `{"junk":true}`, 200)
	})

	// ---------- AUTHZ: approve denied ----------
	for _, c := range []zgAC{
		{n: "SelfApprove_hal", requester: "hal", reqPid: "web", actor: "hal", apprPid: "web", strict: true},
		{n: "SelfApproveOps_hal", requester: "hal", reqPid: "ops", actor: "hal", apprPid: "ops", strict: true},
		{n: "ViewerDan", requester: "hal", reqPid: "web", actor: "dan", apprPid: "web", strict: true},
		{n: "ContributorBen", requester: "hal", reqPid: "web", actor: "ben", apprPid: "web", strict: true},
		{n: "ContributorCat", requester: "hal", reqPid: "web", actor: "cat", apprPid: "web", strict: true},
		{n: "OrgAdminEve", requester: "hal", reqPid: "web", actor: "eve", apprPid: "web", strict: true},
		{n: "OrgAdminEveOps", requester: "hal", reqPid: "ops", actor: "eve", apprPid: "ops", strict: true},
		{n: "OpsNonMemberBen", requester: "hal", reqPid: "ops", actor: "ben", apprPid: "ops", strict: true},
		{n: "OpsViewerAnn", requester: "hal", reqPid: "ops", actor: "ann", apprPid: "ops", strict: true},
		{n: "OpsContributorCat", requester: "hal", reqPid: "ops", actor: "cat", apprPid: "ops", strict: true},
		{n: "OrgAdminRequestsSelfApproves", requester: "eve", reqPid: "web", actor: "eve", apprPid: "web", strict: true},
		{n: "ViewerRequestsSelfApproves_dan", requester: "dan", reqPid: "web", actor: "dan", apprPid: "web", strict: true},
		{n: "OpsViewerAnnRequestsSelfApproves", requester: "ann", reqPid: "ops", actor: "ann", apprPid: "ops", strict: true},
		{n: "ForeignManagerGusApprovesWeb", requester: "hal", reqPid: "web", actor: "gus", apprPid: "web", strict: true},
		{n: "WrongPath_WebManagerApprovesOpsRequest", requester: "hal", reqPid: "ops", actor: "ann", apprPid: "web", strict: true},
		{n: "WrongPath_OpsViewerApprovesWebRequest", requester: "hal", reqPid: "web", actor: "ann", apprPid: "ops", strict: true},
		{n: "WrongPath_LegacyManagerApprovesWebRequest", requester: "hal", reqPid: "web", actor: "ann", apprPid: "legacy", strict: true},
		{n: "WrongPath_BothManagerOpsRequestViaWeb", requester: "hal", reqPid: "ops", actor: "ivy", apprPid: "web", allow: []string{"ops/hal"}},
		{n: "WrongPath_BothManagerWebRequestViaOps", requester: "hal", reqPid: "web", actor: "ivy", apprPid: "ops", allow: []string{"web/hal"}},
		{n: "ArchivedLegacy_ManagerAnn", requester: "hal", reqPid: "legacy", actor: "ann", apprPid: "legacy", strict: true},
		{n: "ArchivedLegacy_ContributorBen", requester: "hal", reqPid: "legacy", actor: "ben", apprPid: "legacy", strict: true},
		{n: "ArchivedLegacy_OrgAdminEve", requester: "hal", reqPid: "legacy", actor: "eve", apprPid: "legacy", strict: true},
		{n: "Anonymous", requester: "hal", reqPid: "web", actor: "", apprPid: "web", strict: true},
		{n: "UnknownUser", requester: "hal", reqPid: "web", actor: "zed", apprPid: "web", strict: true},
		{n: "UnknownProject", requester: "hal", reqPid: "web", actor: "ann", apprPid: "nope", strict: true},
		// cross-org: either the request is refused or the approval fails; the outsider must never end up a member
		{n: "CrossOrg_HalRequestsGxGusApproves", requester: "hal", reqPid: "gx", actor: "gus", apprPid: "gx"},
		{n: "CrossOrg_AnnRequestsGxGusApproves", requester: "ann", reqPid: "gx", actor: "gus", apprPid: "gx"},
		{n: "CrossOrg_EveRequestsGxGusApproves", requester: "eve", reqPid: "gx", actor: "gus", apprPid: "gx"},
		{n: "CrossOrg_IvyRequestsGxGusApproves", requester: "ivy", reqPid: "gx", actor: "gus", apprPid: "gx"},
		{n: "CrossOrg_GusRequestsWebIvyApproves", requester: "gus", reqPid: "web", actor: "ivy", apprPid: "web"},
		{n: "CrossOrg_GusRequestsWebAnnApproves", requester: "gus", reqPid: "web", actor: "ann", apprPid: "web"},
		{n: "CrossOrg_GusRequestsWebEveApproves", requester: "gus", reqPid: "web", actor: "eve", apprPid: "web"},
		{n: "CrossOrg_GusRequestsOpsIvyApproves", requester: "gus", reqPid: "ops", actor: "ivy", apprPid: "ops"},
		{n: "CrossOrg_GusRequestsWebSelfApproves", requester: "gus", reqPid: "web", actor: "gus", apprPid: "web", strict: true},
	} {
		c := c
		t.Run("AUTHZ_Approve_"+c.n, func(t *testing.T) { zgApproveDenied(t, c) })
	}
	t.Run("AUTHZ_CrossOrgGusNeverReadsWeb", func(t *testing.T) {
		h := newServer()
		_, _, rid := zgFile(h, "gus", "web", "")
		if rid == "" {
			rid = "r1"
		}
		for _, u := range []string{"ivy", "ann", "eve", "gus"} {
			zgApprove(h, u, "web", rid)
		}
		for _, p := range []string{"web", "ops"} {
			if c, b := zgDo(h, "gus", "GET", "/projects/"+p+"/tasks", ""); c != 404 {
				t.Errorf("leak: gus GET %s tasks -> %d body=%s", p, c, b)
			}
		}
		if c, b := zgDo(h, "gus", "GET", "/projects/web/tasks/t1", ""); c != 404 {
			t.Errorf("leak: gus GET web t1 -> %d body=%s", c, b)
		}
		if c, _ := zgDo(h, "gus", "POST", "/projects/web/tasks", `{"title":"x"}`); zgIs2xx(c) || c == 403 {
			t.Errorf("gus POST web task -> %d (must be 404)", c)
		}
	})
	t.Run("AUTHZ_CrossOrgInsidersNeverReadGx", func(t *testing.T) {
		h := newServer()
		for _, u := range []string{"hal", "ann", "eve", "ivy"} {
			_, _, rid := zgFile(h, u, "gx", "")
			if rid == "" {
				rid = "r1"
			}
			zgApprove(h, "gus", "gx", rid)
		}
		for _, u := range []string{"hal", "ann", "eve", "ivy"} {
			if c, b := zgDo(h, u, "GET", "/projects/gx/tasks", ""); c != 404 {
				t.Errorf("leak: %s GET gx tasks -> %d body=%s", u, c, b)
			}
		}
	})
	t.Run("AUTHZ_ApprovedRequesterCannotApproveElsewhere", func(t *testing.T) {
		h := newServer()
		_, _, webID := zgFile(h, "hal", "web", "")
		_, _, opsID := zgFile(h, "hal", "ops", "")
		if webID == "" {
			return // feature absent: FUNC_ covers it
		}
		if c, _ := zgApprove(h, "ivy", "web", webID); c != 200 {
			return
		}
		if opsID == "" {
			opsID = "r2"
		}
		before := zgSnap(t, h)
		for _, p := range []string{"ops", "web"} {
			if c, b := zgApprove(h, "hal", p, opsID); zgIs2xx(c) {
				t.Errorf("unexpected acceptance: now-contributor hal approved ops request via %s -> %d %s", p, c, b)
			}
		}
		if c, b := zgApprove(h, "hal", "web", webID); zgIs2xx(c) {
			t.Errorf("unexpected acceptance: contributor hal approved (again) via web -> %d %s", c, b)
		}
		if d := zgDiff(before, zgSnap(t, h)); d != "" {
			t.Errorf("state changed: %s", d)
		}
	})
	t.Run("AUTHZ_BadRequestIdsDenied", func(t *testing.T) {
		h := newServer()
		zgFile(h, "hal", "web", "")
		before := zgSnap(t, h)
		for _, rid := range []string{"r999", "R1", "r1%20", "%2e%2e", "r1x", "nope", "0"} {
			if c, b := zgApprove(h, "ivy", "web", rid); zgIs2xx(c) {
				t.Errorf("unexpected acceptance: approve rid %q -> %d %s", rid, c, b)
			}
		}
		if d := zgDiff(before, zgSnap(t, h)); d != "" {
			t.Errorf("state changed: %s", d)
		}
	})
	t.Run("AUTHZ_ApproveRoleInBodyIgnored", func(t *testing.T) {
		h := newServer()
		_, _, rid := zgFile(h, "hal", "web", `{"role":"manager"}`)
		if rid == "" {
			rid = "r1"
		}
		for _, b := range []string{`{"role":"manager"}`, `{"role":"contributor","userId":"gus"}`} {
			zgDo(h, "ivy", "POST", zgApprovePath("web", rid), b)
		}
		sig := zgSig(h)
		if sig["web/hal"] == "400/400" {
			t.Errorf("privilege escalation: hal became manager via request/approve body (probe %s)", sig["web/hal"])
		}
		if sig["web/gus"] != "404/404" || sig["gx/hal"] != "404" {
			t.Errorf("approve body redirected the grant: gus web=%s hal gx=%s", sig["web/gus"], sig["gx/hal"])
		}
	})
	t.Run("AUTHZ_RequestIdentityInBodyIgnored", func(t *testing.T) {
		h := newServer()
		code, body, rid := zgFile(h, "hal", "web", `{"userId":"gus","user":"gus","status":"approved","projectId":"gx"}`)
		if zgIs2xx(code) {
			var r zgAR
			json.Unmarshal([]byte(body), &r)
			if r.UserID != "hal" || r.ProjectID != "web" || r.Status != "pending" {
				t.Errorf("request fields taken from body: %+v", r)
			}
		}
		if rid == "" {
			rid = "r1"
		}
		zgApprove(h, "ivy", "web", rid)
		zgApprove(h, "gus", "gx", rid)
		sig := zgSig(h)
		if sig["web/gus"] != "404/404" || sig["gx/hal"] != "404" {
			t.Errorf("body identity honored: gus web=%s hal gx=%s", sig["web/gus"], sig["gx/hal"])
		}
	})
	t.Run("AUTHZ_RequestStatusInBodyDoesNotGrant", func(t *testing.T) {
		h := newServer()
		zgFile(h, "hal", "web", `{"status":"approved","role":"manager"}`)
		if s := zgSig(h)["web/hal"]; s != "404/404" {
			t.Errorf("state changed: hal self-granted by filing (probe %s)", s)
		}
		zgFile(h, "hal", "ops", `{"approved":true}`)
		if s := zgSig(h)["ops/hal"]; s != "404/404" {
			t.Errorf("state changed: hal self-granted by filing (probe %s)", s)
		}
	})
	t.Run("AUTHZ_RequestProjectInBodyIgnored", func(t *testing.T) {
		h := newServer()
		_, _, rid := zgFile(h, "hal", "web", `{"projectId":"ops","pid":"ops"}`)
		if rid == "" {
			rid = "r1"
		}
		zgApprove(h, "ivy", "ops", rid)
		zgApprove(h, "ivy", "web", rid)
		if s := zgSig(h)["ops/hal"]; s != "404/404" {
			t.Errorf("state changed: hal got ops via a web request (probe %s)", s)
		}
	})
	t.Run("AUTHZ_FileDenied", func(t *testing.T) {
		for _, c := range []struct{ n, user, pid string }{
			{"Anonymous", "", "web"}, {"UnknownUser", "zed", "web"}, {"UnknownProject", "hal", "nope"},
			{"GusOtherOrgWeb", "gus", "web"}, {"AnnOtherOrgGx", "ann", "gx"},
		} {
			h := newServer()
			before := zgSnap(t, h)
			code, body, _ := zgFile(h, c.user, c.pid, "")
			after := zgSnap(t, h)
			if c.n != "GusOtherOrgWeb" && c.n != "AnnOtherOrgGx" && zgIs2xx(code) {
				t.Errorf("unexpected acceptance: %s -> %d %s", c.n, code, body)
			}
			if d := zgDiff(before, after); d != "" {
				t.Errorf("state changed (%s -> %d): %s", c.n, code, d)
			}
			if l := zgLeaks(c.user, body); len(l) > 0 {
				t.Errorf("leak (%s): %v", c.n, l)
			}
		}
	})
	t.Run("AUTHZ_FileCrossOrgNotAccepted", func(t *testing.T) {
		h := newServer()
		for _, c := range []struct{ user, pid string }{{"gus", "web"}, {"gus", "ops"}, {"ann", "gx"}, {"hal", "gx"}} {
			if code, body, _ := zgFile(h, c.user, c.pid, ""); zgIs2xx(code) {
				t.Logf("INFO cross-org request %s -> %s accepted (%d) body=%s; only a problem if it can be approved into membership", c.user, c.pid, code, body)
			}
		}
	})
	t.Run("AUTHZ_DoubleApproveNoChange", func(t *testing.T) {
		h := newServer()
		_, _, id := zgFile(h, "hal", "web", "")
		if id == "" {
			return // feature absent: FUNC_ covers it
		}
		if c, _ := zgApprove(h, "ivy", "web", id); c != 200 {
			return
		}
		s1 := zgSnap(t, h)
		zgApprove(h, "ivy", "web", id)
		zgApprove(h, "ann", "web", id)
		zgDo(h, "ivy", "POST", zgApprovePath("web", id), `{"role":"manager"}`)
		if d := zgDiff(s1, zgSnap(t, h)); d != "" {
			t.Errorf("state changed by repeated approval: %s", d)
		}
		if s := zgSig(h)["web/hal"]; s != "403/400" {
			t.Errorf("hal probe = %s, want contributor 403/400", s)
		}
	})

	// ---------- AUTHZ: listing ----------
	t.Run("AUTHZ_ListNonMemberCannotSee", func(t *testing.T) {
		for _, c := range []struct{ n, requester, reqPid, viewer, listPid string }{
			{"BenNonMemberOfOps", "hal", "ops", "ben", "ops"},
			{"GusCrossOrgWeb", "hal", "web", "gus", "web"},
			{"GusCrossOrgOps", "hal", "ops", "gus", "ops"},
			{"AnnManagerOfWebListsOps_isViewerThere", "hal", "ops", "ann", "web"},
			{"IvyListsLegacyNonMember", "hal", "legacy", "ivy", "legacy"},
		} {
			h := newServer()
			zgFile(h, c.requester, c.reqPid, "")
			code, body := zgDo(h, c.viewer, "GET", "/projects/"+c.listPid+"/access-requests", "")
			if zgIs2xx(code) && strings.Contains(body, `"`+c.requester+`"`) {
				t.Errorf("leak (%s): %s listed %s requests -> %d body=%s", c.n, c.viewer, c.listPid, code, body)
			}
			if l := zgLeaks(c.viewer, body); len(l) > 0 {
				t.Errorf("leak (%s): task titles %v", c.n, l)
			}
		}
	})
	t.Run("AUTHZ_ListDeniedNoStateChange", func(t *testing.T) {
		h := newServer()
		zgFile(h, "hal", "web", "")
		before := zgSnap(t, h)
		for _, c := range []struct{ user, pid string }{{"", "web"}, {"zed", "web"}, {"gus", "web"}, {"ben", "ops"}, {"hal", "nope"}} {
			code, body := zgDo(h, c.user, "GET", "/projects/"+c.pid+"/access-requests", "")
			if c.user != "ben" && c.user != "gus" && zgIs2xx(code) {
				t.Errorf("unexpected acceptance: list as %q %s -> %d %s", c.user, c.pid, code, body)
			}
		}
		if d := zgDiff(before, zgSnap(t, h)); d != "" {
			t.Errorf("state changed: %s", d)
		}
	})
	t.Run("AUTHZ_ListScopedToProject", func(t *testing.T) {
		h := newServer()
		zgFile(h, "hal", "ops", "")
		for _, u := range []string{"ann", "ivy"} {
			code, body := zgDo(h, u, "GET", "/projects/web/access-requests", "")
			if zgIs2xx(code) && (strings.Contains(body, `"hal"`) || strings.Contains(body, `"ops"`)) {
				t.Errorf("leak: web list as %s contains an ops request: %s", u, body)
			}
		}
	})
	t.Run("AUTHZ_ListCrossOrgRequestNotShownToOtherOrgManagers", func(t *testing.T) {
		h := newServer()
		zgFile(h, "gus", "web", "")
		zgFile(h, "hal", "gx", "")
		code, body := zgDo(h, "gus", "GET", "/projects/gx/access-requests", "")
		if zgIs2xx(code) && strings.Contains(body, `"hal"`) {
			t.Logf("INFO globex manager sees acme hal's gx request (cross-org filing accepted): %s", body)
		}
		code, body = zgDo(h, "gus", "GET", "/projects/web/access-requests", "")
		if zgIs2xx(code) && strings.Contains(body, `"gus"`) {
			t.Errorf("leak: gus sees web request list: %s", body)
		}
	})

	// ---------- ATOM ----------
	t.Run("ATOM_ExistingMembersRequestsNoCollateral", func(t *testing.T) {
		for _, c := range []struct{ requester, pid, approver string }{
			{"dan", "web", "ivy"}, {"ben", "web", "ivy"}, {"ann", "web", "ivy"}, {"ann", "ops", "ivy"},
			{"cat", "web", "ann"}, {"eve", "web", "ivy"}, {"ben", "ops", "ivy"}, {"ivy", "web", "ann"},
		} {
			h := newServer()
			before := zgSnap(t, h)
			_, _, rid := zgFile(h, c.requester, c.pid, "")
			if rid == "" {
				continue
			}
			zgApprove(h, c.approver, c.pid, rid)
			after := zgSnap(t, h)
			if d := zgDiffAllow(before, after, c.pid+"/"+c.requester); d != "" {
				t.Errorf("collateral change: %s (%s requested %s, %s approved)", d, c.requester, c.pid, c.approver)
			}
		}
	})
	t.Run("ATOM_DeniedApproveKeepsRequestPending", func(t *testing.T) {
		h := newServer()
		_, _, rid := zgFile(h, "hal", "web", "")
		if rid == "" {
			return
		}
		zgApprove(h, "hal", "web", rid)
		zgApprove(h, "dan", "web", rid)
		zgApprove(h, "gus", "web", rid)
		if _, _, l, ok := zgList(h, "ivy", "web"); ok {
			for _, r := range l {
				if r.ID == rid && r.Status != "pending" {
					t.Errorf("state changed: request %s status %q after denied approvals, want pending", rid, r.Status)
				}
			}
		}
		if s := zgSig(h)["web/hal"]; s != "404/404" {
			t.Errorf("state changed: hal probe %s after denied approvals", s)
		}
		zgMustApprove(t, h, "ivy", "web", rid)
		if s := zgSig(h)["web/hal"]; s != "403/400" {
			t.Errorf("hal probe after later legit approval = %s, want 403/400", s)
		}
	})
	t.Run("ATOM_WrongPathApproveKeepsRequestPending", func(t *testing.T) {
		h := newServer()
		_, _, rid := zgFile(h, "hal", "ops", "")
		if rid == "" {
			return
		}
		zgApprove(h, "ann", "web", rid)
		if _, _, l, ok := zgList(h, "ivy", "ops"); ok {
			for _, r := range l {
				if r.ID == rid && r.Status != "pending" {
					t.Errorf("state changed: ops request %s status %q after approve via web path by web-only manager", rid, r.Status)
				}
			}
		}
		if s := zgSig(h)["ops/hal"]; s != "404/404" {
			t.Errorf("state changed: hal ops probe %s", s)
		}
	})
	t.Run("ATOM_ArchivedLegacyNoMembership", func(t *testing.T) {
		h := newServer()
		_, _, rid := zgFile(h, "hal", "legacy", "")
		if rid == "" {
			rid = "r1"
		}
		zgApprove(h, "ann", "legacy", rid)
		zgApprove(h, "ann", "legacy", "r1")
		if c, b := zgDo(h, "hal", "GET", "/projects/legacy/tasks", ""); c != 404 {
			t.Errorf("state changed: hal reads archived legacy -> %d %s", c, b)
		}
	})

	// ---------- VALID ----------
	t.Run("VALID_FileMalformedBodyNoGrant", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		for _, b := range []string{`{bad`, `[]`, `"hal"`, `null`} {
			zgFile(h, "hal", "web", b)
		}
		if d := zgDiff(before, zgSnap(t, h)); d != "" {
			t.Errorf("state changed: %s", d)
		}
	})
	t.Run("VALID_ApproveWrongMethodNoChange", func(t *testing.T) {
		h := newServer()
		zgFile(h, "hal", "web", "")
		before := zgSnap(t, h)
		for _, m := range []string{"GET", "PUT", "PATCH", "DELETE"} {
			zgDo(h, "ivy", m, zgApprovePath("web", "r1"), "")
		}
		if d := zgDiff(before, zgSnap(t, h)); d != "" {
			t.Errorf("state changed by non-POST on approve path: %s", d)
		}
	})
	t.Run("VALID_ApproveTrailingSlashOrEmptyRid", func(t *testing.T) {
		h := newServer()
		zgFile(h, "hal", "web", "")
		before := zgSnap(t, h)
		for _, p := range []string{"/projects/web/access-requests//approve", "/projects/web/access-requests/approve", "/projects/web/access-requests/r1/approve/x"} {
			if c, b := zgDo(h, "ivy", "POST", p, ""); zgIs2xx(c) {
				t.Errorf("unexpected acceptance: POST %s -> %d %s", p, c, b)
			}
		}
		if d := zgDiff(before, zgSnap(t, h)); d != "" {
			t.Errorf("state changed: %s", d)
		}
	})

	// ---------- CONTRACT ----------
	t.Run("CONTRACT_FirstRequestIsR1Exact", func(t *testing.T) {
		h := newServer()
		code, body, _ := zgFile(h, "hal", "web", "")
		var m map[string]any
		if code != 201 || json.Unmarshal([]byte(body), &m) != nil {
			t.Fatalf("-> %d body=%s", code, body)
		}
		if m["id"] != "r1" || m["projectId"] != "web" || m["userId"] != "hal" || m["status"] != "pending" {
			t.Errorf("body %s, want {id:r1, projectId:web, userId:hal, status:pending}", body)
		}
	})
	t.Run("CONTRACT_ApproveReturns200Approved", func(t *testing.T) {
		h := newServer()
		zgFile(h, "hal", "web", "")
		code, body := zgApprove(h, "ivy", "web", "r1")
		var r zgAR
		json.Unmarshal([]byte(body), &r)
		if code != 200 || r.Status != "approved" || r.ID != "r1" || r.UserID != "hal" || r.ProjectID != "web" {
			t.Errorf("-> %d body=%s", code, body)
		}
	})
	for _, c := range []struct {
		n         string
		requester string
		reqPid    string
		actor     string
		apprPid   string
		rid       string // "" = the id returned
		want      int
	}{
		{"ApproveViewer403", "hal", "web", "dan", "web", "", 403},
		{"ApproveContributor403", "hal", "web", "ben", "web", "", 403},
		{"ApproveOrgAdmin403", "hal", "web", "eve", "web", "", 403},
		{"ApproveOpsViewer403", "hal", "ops", "ann", "ops", "", 403},
		{"ApproveNonMemberSelf404", "hal", "web", "hal", "web", "", 404},
		{"ApproveCrossOrg404", "hal", "web", "gus", "web", "", 404},
		{"ApproveNonMemberOfOps404", "hal", "ops", "ben", "ops", "", 404},
		{"ApproveUnknownProject404", "hal", "web", "ann", "nope", "r1", 404},
		{"ApproveUnknownRid404", "hal", "web", "ivy", "web", "r999", 404},
		{"ApproveWrongPathRid404", "hal", "ops", "ivy", "web", "", 404},
		{"ApproveAnonymous401", "hal", "web", "", "web", "", 401},
		{"ApproveUnknownUser401", "hal", "web", "zed", "web", "", 401},
		{"ApproveArchived409", "hal", "legacy", "ann", "legacy", "r1", 409},
	} {
		c := c
		t.Run("CONTRACT_"+c.n, func(t *testing.T) {
			h := newServer()
			_, _, rid := zgFile(h, c.requester, c.reqPid, "")
			if c.rid != "" {
				rid = c.rid
			}
			if rid == "" {
				rid = "r1"
			}
			code, body := zgApprove(h, c.actor, c.apprPid, rid)
			if code != c.want {
				t.Errorf("approve as %q -> %d, want %d body=%s", c.actor, code, c.want, body)
			}
		})
	}
	for _, c := range []struct {
		n, user, pid string
		want         int
	}{
		{"FileAnonymous401", "", "web", 401},
		{"FileUnknownUser401", "zed", "web", 401},
		{"FileUnknownProject404", "hal", "nope", 404},
		{"FileCrossOrg404", "gus", "web", 404},
		{"FileInsiderToGx404", "hal", "gx", 404},
		{"ListCrossOrg404", "gus", "web", 404},
		{"ListUnknownProject404", "ann", "nope", 404},
		{"ListAnonymous401", "", "web", 401},
		{"ListNonMemberBenOps404", "ben", "ops", 404},
	} {
		c := c
		t.Run("CONTRACT_"+c.n, func(t *testing.T) {
			h := newServer()
			var code int
			var body string
			if strings.HasPrefix(c.n, "File") {
				code, body, _ = zgFile(h, c.user, c.pid, "")
			} else {
				code, body = zgDo(h, c.user, "GET", "/projects/"+c.pid+"/access-requests", "")
			}
			if code != c.want {
				t.Errorf("-> %d, want %d body=%s", code, c.want, body)
			}
		})
	}
	t.Run("CONTRACT_ErrorShape", func(t *testing.T) {
		h := newServer()
		zgFile(h, "hal", "web", "")
		code, body := zgApprove(h, "dan", "web", "r1")
		var e map[string]string
		if code != 403 || json.Unmarshal([]byte(body), &e) != nil || e["error"] == "" {
			t.Errorf("-> %d body=%s, want 403 {\"error\":...}", code, body)
		}
	})

	// ---------- INFO ----------
	t.Run("INFO_DuplicateRequest", func(t *testing.T) {
		h := newServer()
		c1, b1, _ := zgFile(h, "hal", "web", "")
		c2, b2, _ := zgFile(h, "hal", "web", "")
		_, lb, _, _ := zgList(h, "ivy", "web")
		t.Logf("INFO duplicate request: first %d %s; second %d %s; list=%s", c1, b1, c2, b2, strings.TrimSpace(lb))
	})
	t.Run("INFO_ExistingMemberRequests", func(t *testing.T) {
		for _, c := range []struct{ user, pid, approver string }{
			{"dan", "web", "ivy"}, {"ben", "web", "ivy"}, {"ann", "web", "ivy"}, {"ann", "ops", "ivy"}, {"eve", "web", "ivy"},
		} {
			h := newServer()
			before := zgSig(h)[c.pid+"/"+c.user]
			fc, fb, rid := zgFile(h, c.user, c.pid, "")
			ac, ab := 0, ""
			if rid != "" {
				ac, ab = zgApprove(h, c.approver, c.pid, rid)
			}
			t.Logf("INFO member %s requests %s: file %d %s; approve %d %s; probe %s -> %s", c.user, c.pid, fc, strings.TrimSpace(fb), ac, strings.TrimSpace(ab), before, zgSig(h)[c.pid+"/"+c.user])
		}
	})
	t.Run("INFO_ListVisibilityOtherRoles", func(t *testing.T) {
		h := newServer()
		zgFile(h, "hal", "web", "")
		for _, u := range []string{"dan", "ben", "cat", "eve", "hal", "ann"} {
			c, b := zgDo(h, u, "GET", "/projects/web/access-requests", "")
			t.Logf("INFO %s lists web requests -> %d %s", u, c, strings.TrimSpace(b))
		}
	})
	t.Run("INFO_ListArchivedLegacy", func(t *testing.T) {
		h := newServer()
		c, b := zgDo(h, "ann", "GET", "/projects/legacy/access-requests", "")
		fc, fb, _ := zgFile(h, "hal", "legacy", "")
		t.Logf("INFO ann lists legacy -> %d %s; hal files legacy -> %d %s", c, strings.TrimSpace(b), fc, strings.TrimSpace(fb))
	})
	t.Run("INFO_RejectEndpoint", func(t *testing.T) {
		h := newServer()
		zgFile(h, "hal", "web", "")
		for _, p := range []string{"reject", "deny", "decline"} {
			c, b := zgDo(h, "ivy", "POST", "/projects/web/access-requests/r1/"+p, "")
			t.Logf("INFO ivy POST .../r1/%s -> %d %s; hal probe %s", p, c, strings.TrimSpace(b), zgSig(h)["web/hal"])
		}
	})
	t.Run("INFO_SingleRequestGet", func(t *testing.T) {
		h := newServer()
		zgFile(h, "hal", "web", "")
		c, b := zgDo(h, "ivy", "GET", "/projects/web/access-requests/r1", "")
		t.Logf("INFO ivy GET web r1 -> %d %s", c, strings.TrimSpace(b))
	})
	t.Run("INFO_DoubleApproveStatus", func(t *testing.T) {
		h := newServer()
		zgFile(h, "hal", "web", "")
		c1, _ := zgApprove(h, "ivy", "web", "r1")
		c2, b2 := zgApprove(h, "ivy", "web", "r1")
		t.Logf("INFO approve twice: %d then %d %s", c1, c2, strings.TrimSpace(b2))
	})
	t.Run("INFO_ApprovedUserRequestsAgain", func(t *testing.T) {
		h := newServer()
		zgFile(h, "hal", "web", "")
		zgApprove(h, "ivy", "web", "r1")
		c, b, _ := zgFile(h, "hal", "web", "")
		t.Logf("INFO hal (now contributor) files again -> %d %s; probe %s", c, strings.TrimSpace(b), zgSig(h)["web/hal"])
	})
	t.Run("INFO_RequestIDsAcrossProjects", func(t *testing.T) {
		h := newServer()
		_, b1, _ := zgFile(h, "hal", "web", "")
		_, b2, _ := zgFile(h, "hal", "ops", "")
		_, b3, _ := zgFile(h, "eve", "web", "")
		t.Logf("INFO ids: %s %s %s", strings.TrimSpace(b1), strings.TrimSpace(b2), strings.TrimSpace(b3))
	})
}
