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

// Hidden grader for task 021 (POST /projects/{pid}/tasks with a non-member assignee adds
// that user as contributor). Uses only newServer() and HTTP. Roles are not readable over
// HTTP, so they are inferred with non-mutating probes (see zgSig).

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

func zgBody(title, assignee string) string {
	b, _ := json.Marshal(map[string]string{"title": title, "assignee": assignee})
	return string(b)
}

func zgCreate(h http.Handler, user, pid, title, assignee string) (int, string) {
	return zgDo(h, user, "POST", "/projects/"+pid+"/tasks", zgBody(title, assignee))
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

// zgTasks reads every task as users never affected by these tests (eve: org admin of acme,
// gus: manager of gx).
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

// zgSig observes every user's role in web/ops/legacy/gx without mutating anything:
//
//	PATCH <task> {"assignee":"zzz"}: 404 non-member, 403 viewer/org admin/contributor, 400 manager (409 archived, 403 viewer)
//	POST  tasks  {}               : 404 non-member, 403 viewer/org admin, 400 contributor/manager (409 archived)
func zgSig(h http.Handler) map[string]string {
	out := map[string]string{}
	for _, p := range []struct{ pid, tid string }{{"web", "t3"}, {"ops", "t4"}, {"legacy", "t5"}, {"gx", "t6"}} {
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
			d = append(d, fmt.Sprintf("task appeared %+v", y))
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

type zgReq struct{ user, pid, title, assignee string }

// zgDenied: non-2xx, no state change (tasks and role probes of all projects), no leak;
// want!=0 additionally checks the exact status.
func zgDenied(t *testing.T, r zgReq, want int) {
	t.Helper()
	h := newServer()
	before := zgSnap(t, h)
	code, resp := zgCreate(h, r.user, r.pid, r.title, r.assignee)
	after := zgSnap(t, h)
	if code >= 200 && code < 300 {
		t.Errorf("unexpected acceptance: POST %s title=%q assignee=%q as %q -> %d body=%s", r.pid, r.title, r.assignee, r.user, code, resp)
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

func zgCreated(t *testing.T, h http.Handler, user, pid, title, assignee string) zgTask {
	t.Helper()
	code, body := zgCreate(h, user, pid, title, assignee)
	if code != 201 {
		t.Fatalf("POST %s assignee=%s as %s -> %d, want 201 body=%s", pid, assignee, user, code, body)
	}
	var tk zgTask
	if err := json.Unmarshal([]byte(body), &tk); err != nil || tk.ID == "" {
		t.Fatalf("decode created task: %v body=%s", err, body)
	}
	return tk
}

// zgOnlyChange asserts that, compared with before, exactly the given probe keys changed to
// the given values and exactly one new task (with the given project/title/assignee) exists.
func zgOnlyChange(t *testing.T, before, after zgState, pid, title, assignee string, probes map[string]string) {
	t.Helper()
	for k, x := range before.sig {
		want := x
		if v, ok := probes[k]; ok {
			want = v
		}
		if after.sig[k] != want {
			t.Errorf("role-probe %s = %s, want %s", k, after.sig[k], want)
		}
	}
	n := 0
	for id, y := range after.tasks {
		if x, ok := before.tasks[id]; ok {
			if x != y {
				t.Errorf("existing task changed: %+v -> %+v", x, y)
			}
			continue
		}
		n++
		if y.ProjectID != pid || y.Title != title || y.Assignee != assignee {
			t.Errorf("new task = %+v, want project=%s title=%q assignee=%s", y, pid, title, assignee)
		}
	}
	if n != 1 {
		t.Errorf("%d new tasks, want exactly 1", n)
	}
}

func TestGrader(t *testing.T) {
	// ---------- REG ----------
	for _, c := range []struct {
		n, user, method, path, body string
		want                        int
	}{
		{"ListWeb", "ann", "GET", "/projects/web/tasks", "", 200},
		{"ListWebNonMember404", "hal", "GET", "/projects/web/tasks", "", 404},
		{"GetTask", "dan", "GET", "/projects/web/tasks/t1", "", 200},
		{"CreateViewer403", "dan", "POST", "/projects/web/tasks", zgBody("x", ""), 403},
		{"CreateOrgAdmin403", "eve", "POST", "/projects/web/tasks", zgBody("x", ""), 403},
		{"CreateNonMember404", "hal", "POST", "/projects/web/tasks", zgBody("x", ""), 404},
		{"CreateOtherOrg404", "gus", "POST", "/projects/web/tasks", zgBody("x", ""), 404},
		{"CreateAnon401", "", "POST", "/projects/web/tasks", zgBody("x", ""), 401},
		{"CreateArchived409", "ann", "POST", "/projects/legacy/tasks", zgBody("x", ""), 409},
		{"CreateNoTitle400", "ann", "POST", "/projects/web/tasks", `{}`, 400},
		{"CreateUnassigned", "ben", "POST", "/projects/web/tasks", zgBody("x", ""), 201},
		{"CreateManagerAssignsContributor", "ann", "POST", "/projects/web/tasks", zgBody("x", "ben"), 201},
		{"CreateManagerAssignsManager", "ann", "POST", "/projects/web/tasks", zgBody("x", "ivy"), 201},
		{"CreateContributorAssignsSelf", "ben", "POST", "/projects/web/tasks", zgBody("x", "ben"), 201},
		{"CreateContributorAssignsMember403", "ben", "POST", "/projects/web/tasks", zgBody("x", "cat"), 403},
		{"CreateAssignViewer400", "ivy", "POST", "/projects/web/tasks", zgBody("x", "dan"), 400},
		{"CreateOpsAssignViewerAnn400", "ivy", "POST", "/projects/ops/tasks", zgBody("x", "ann"), 400},
		{"PatchContributorOthers403", "ben", "PATCH", "/projects/web/tasks/t2", `{"status":"done"}`, 403},
		{"PatchContributorOwn", "ben", "PATCH", "/projects/web/tasks/t1", `{"status":"doing"}`, 200},
		{"PatchManagerReassign", "ann", "PATCH", "/projects/web/tasks/t1", `{"assignee":"cat"}`, 200},
		{"PatchReassignToViewer400", "ann", "PATCH", "/projects/web/tasks/t1", `{"assignee":"dan"}`, 400},
		{"PatchReassignToNonMember400", "ann", "PATCH", "/projects/web/tasks/t1", `{"assignee":"hal"}`, 400},
		{"DeleteContributor403", "ben", "DELETE", "/projects/web/tasks/t1", "", 403},
		{"DeleteManager", "ann", "DELETE", "/projects/web/tasks/t3", "", 204},
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
			"ops/ann": "403/403", "ops/cat": "403/400", "ops/ivy": "400/400", "ops/ben": "404/404", "ops/hal": "404/404",
			"ops/eve": "403/403", "legacy/ann": "409/409", "legacy/ben": "409/409", "legacy/cat": "404/404",
			"gx/gus": "400/400", "gx/ann": "404/404", "gx/hal": "404/404",
		}
		for k, v := range want {
			if sig[k] != v {
				t.Errorf("probe %s = %s, want %s", k, sig[k], v)
			}
		}
	})
	t.Run("REG_ProbeNonMutating", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		zgSig(h)
		if d := zgDiff(before, zgSnap(t, h)); d != "" {
			t.Errorf("probe mutated state: %s", d)
		}
	})

	// ---------- FUNC ----------
	for _, c := range []struct{ n, user string }{{"ManagerIvy", "ivy"}, {"ManagerAnn", "ann"}} {
		c := c
		t.Run("FUNC_InviteHalToWeb_"+c.n, func(t *testing.T) {
			h := newServer()
			before := zgSnap(t, h)
			tk := zgCreated(t, h, c.user, "web", "Invite hal", "hal")
			if tk.Assignee != "hal" || tk.ProjectID != "web" || tk.Title != "Invite hal" || tk.Status != "open" {
				t.Errorf("created task = %+v", tk)
			}
			after := zgSnap(t, h)
			// hal becomes web contributor, and nothing else changes (not ops, not gx, nobody else)
			zgOnlyChange(t, before, after, "web", "Invite hal", "hal", map[string]string{"web/hal": "403/400"})
		})
	}
	t.Run("FUNC_InvitedHalCanReadAndEditTask", func(t *testing.T) {
		h := newServer()
		zgMust(t, h, "hal", "GET", "/projects/web/tasks", "", 404)
		tk := zgCreated(t, h, "ivy", "web", "Invite hal", "hal")
		zgMust(t, h, "hal", "GET", "/projects/web/tasks", "", 200)
		zgMust(t, h, "hal", "GET", "/projects/web/tasks/"+tk.ID, "", 200)
		zgMust(t, h, "hal", "PATCH", "/projects/web/tasks/"+tk.ID, `{"status":"doing"}`, 200)
		// contributor semantics: cannot touch others' tasks, cannot delete, can create for self
		zgMust(t, h, "hal", "PATCH", "/projects/web/tasks/t1", `{"status":"done"}`, 403)
		zgMust(t, h, "hal", "DELETE", "/projects/web/tasks/"+tk.ID, "", 403)
		zgMust(t, h, "hal", "POST", "/projects/web/tasks", zgBody("mine", "hal"), 201)
	})
	t.Run("FUNC_InvitedHalIsContributorNotManager", func(t *testing.T) {
		h := newServer()
		zgCreated(t, h, "ivy", "web", "Invite hal", "hal")
		zgMust(t, h, "hal", "POST", "/projects/web/tasks", zgBody("x", "ben"), 403)
		zgMust(t, h, "hal", "PATCH", "/projects/web/tasks/t3", `{"assignee":"ben"}`, 403)
	})
	t.Run("FUNC_InviteBenToOps", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		tk := zgCreated(t, h, "ivy", "ops", "Ops for ben", "ben")
		after := zgSnap(t, h)
		zgOnlyChange(t, before, after, "ops", "Ops for ben", "ben", map[string]string{"ops/ben": "403/400"})
		zgMust(t, h, "ben", "PATCH", "/projects/ops/tasks/"+tk.ID, `{"status":"done"}`, 200)
		zgMust(t, h, "ben", "PATCH", "/projects/ops/tasks/t4", `{"status":"done"}`, 403)
	})
	t.Run("FUNC_InviteTwice_SecondIsExistingMember", func(t *testing.T) {
		h := newServer()
		zgCreated(t, h, "ivy", "web", "first", "hal")
		zgCreated(t, h, "ann", "web", "second", "hal")
		zgCreated(t, h, "hal", "web", "third", "hal")
		sig := zgSig(h)
		if sig["web/hal"] != "403/400" {
			t.Errorf("hal probe = %s, want contributor 403/400", sig["web/hal"])
		}
	})
	t.Run("FUNC_InviteViewerOfOtherProjectIndependently", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		zgCreated(t, h, "ivy", "web", "for hal", "hal")
		zgCreated(t, h, "ivy", "ops", "for dan", "dan")
		sig := zgSig(h)
		// dan is a web viewer only; he is not in ops, so he is invited there as contributor.
		if s := sig["ops/dan"]; s != "403/400" {
			t.Errorf("dan ops probe = %s, want contributor 403/400", s)
		}
		if s := sig["web/dan"]; s != before.sig["web/dan"] {
			t.Errorf("dan web role changed: %s -> %s", before.sig["web/dan"], s)
		}
	})
	t.Run("FUNC_MemberAssigneesUnchanged", func(t *testing.T) {
		for _, c := range []struct{ user, pid, assignee string }{
			{"ann", "web", "cat"}, {"ann", "web", "ivy"}, {"ivy", "web", "ben"}, {"ben", "web", "ben"}, {"cat", "ops", "cat"}, {"ivy", "ops", "cat"},
		} {
			h := newServer()
			before := zgSnap(t, h)
			zgCreated(t, h, c.user, c.pid, "member", c.assignee)
			zgOnlyChange(t, before, zgSnap(t, h), c.pid, "member", c.assignee, nil)
		}
	})
	t.Run("FUNC_CreatedTaskVisibleInList", func(t *testing.T) {
		h := newServer()
		tk := zgCreated(t, h, "ivy", "web", "listed", "hal")
		got := zgTasks(t, h)[tk.ID]
		if got.Assignee != "hal" || got.Title != "listed" || got.ProjectID != "web" {
			t.Errorf("listed task = %+v", got)
		}
	})

	// ---------- AUTHZ ----------
	for _, c := range []struct {
		n string
		r zgReq
	}{
		// adding a member is manager-level: contributors must not do it
		{"ContributorInvitesHal_ben", zgReq{"ben", "web", "x", "hal"}},
		{"ContributorInvitesHal_cat", zgReq{"cat", "web", "x", "hal"}},
		{"ContributorInvitesBenToOps_cat", zgReq{"cat", "ops", "x", "ben"}},
		{"ContributorInvitesGus_cat", zgReq{"cat", "web", "x", "gus"}},
		{"ContributorInvitesEve_ben", zgReq{"ben", "web", "x", "eve"}},
		{"ContributorInvitesUnknown_ben", zgReq{"ben", "web", "x", "zed"}},
		{"ContributorInvitesViewer_ben", zgReq{"ben", "web", "x", "dan"}},
		{"ContributorInvitesHalArchived_ben", zgReq{"ben", "legacy", "x", "hal"}},
		// roles that cannot create at all
		{"ViewerInvitesHal_dan", zgReq{"dan", "web", "x", "hal"}},
		{"ViewerAnnOpsInvitesHal", zgReq{"ann", "ops", "x", "hal"}},
		{"ViewerAnnOpsInvitesSelfNoop", zgReq{"ann", "ops", "x", "ann"}},
		{"OrgAdminInvitesHal_eve", zgReq{"eve", "web", "x", "hal"}},
		{"OrgAdminInvitesSelf_eve", zgReq{"eve", "web", "x", "eve"}},
		{"NonMemberInvitesSelf_hal", zgReq{"hal", "web", "x", "hal"}},
		{"NonMemberInvitesOther_hal", zgReq{"hal", "web", "x", "ben"}},
		{"NonMemberBenInOps", zgReq{"ben", "ops", "x", "hal"}},
		{"OtherOrgGusInvitesHalToWeb", zgReq{"gus", "web", "x", "hal"}},
		{"OtherOrgGusInvitesSelfToWeb", zgReq{"gus", "web", "x", "gus"}},
		{"Anonymous", zgReq{"", "web", "x", "hal"}},
		{"UnknownCaller", zgReq{"zed", "web", "x", "hal"}},
		{"UnknownProject", zgReq{"ann", "nope", "x", "hal"}},
		// only users of the project's organization
		{"CrossOrgGusToWeb_ivy", zgReq{"ivy", "web", "x", "gus"}},
		{"CrossOrgGusToWeb_ann", zgReq{"ann", "web", "x", "gus"}},
		{"CrossOrgGusToOps_ivy", zgReq{"ivy", "ops", "x", "gus"}},
		{"CrossOrgAnnToGx_gus", zgReq{"gus", "gx", "x", "ann"}},
		{"CrossOrgHalToGx_gus", zgReq{"gus", "gx", "x", "hal"}},
		{"CrossOrgAnnToGx_ivy", zgReq{"ivy", "gx", "x", "ann"}},
		// unknown assignees
		{"UnknownAssignee_zed", zgReq{"ivy", "web", "x", "zed"}},
		{"UnknownAssignee_ANN", zgReq{"ivy", "web", "x", "ANN"}},
		{"UnknownAssignee_spaced", zgReq{"ivy", "web", "x", " hal"}},
		{"UnknownAssignee_nobody", zgReq{"ann", "ops", "x", "nobody"}},
		// existing members keep existing semantics: viewers are not upgraded
		{"ViewerNotUpgraded_dan", zgReq{"ivy", "web", "x", "dan"}},
		{"ViewerNotUpgraded_danByAnn", zgReq{"ann", "web", "x", "dan"}},
		{"ViewerNotUpgraded_annInOps", zgReq{"ivy", "ops", "x", "ann"}},
		// archived: nobody added
		{"ArchivedInviteHal_ann", zgReq{"ann", "legacy", "x", "hal"}},
		{"ArchivedInviteCat_ann", zgReq{"ann", "legacy", "x", "cat"}},
		{"ArchivedInviteGus_ann", zgReq{"ann", "legacy", "x", "gus"}},
	} {
		c := c
		t.Run("AUTHZ_"+c.n, func(t *testing.T) { zgDenied(t, c.r, 0) })
	}
	t.Run("AUTHZ_ContributorCannotInviteViaAnyNonMember", func(t *testing.T) {
		for _, u := range []string{"hal", "gus", "eve", "zed"} {
			h := newServer()
			before := zgSnap(t, h)
			code, body := zgCreate(h, "ben", "web", "x", u)
			if code >= 200 && code < 300 {
				t.Errorf("unexpected acceptance: ben invites %s -> %d %s", u, code, body)
			}
			if d := zgDiff(before, zgSnap(t, h)); d != "" {
				t.Errorf("state changed (assignee %s): %s", u, d)
			}
		}
	})
	t.Run("AUTHZ_CrossOrgNotAdded_gusCannotReadWeb", func(t *testing.T) {
		h := newServer()
		code, body := zgCreate(h, "ivy", "web", "x", "gus")
		if code >= 200 && code < 300 {
			t.Errorf("unexpected acceptance: ivy invites gus (globex) to web -> %d body=%s", code, body)
		}
		if c, _ := zgDo(h, "gus", "GET", "/projects/web/tasks", ""); c != 404 {
			t.Errorf("gus can now read web: %d (cross-org membership created)", c)
		}
		if c, b := zgDo(h, "gus", "GET", "/projects/web/tasks/t1", ""); c == 200 {
			t.Errorf("leak: gus reads web task: %s", b)
		}
		if c, b := zgCreate(h, "gus", "web", "pwn", "gus"); c >= 200 && c < 300 {
			t.Errorf("gus can write in web: %d %s", c, b)
		}
	})
	t.Run("AUTHZ_ContributorCannotInvite_halStillOutside", func(t *testing.T) {
		h := newServer()
		zgCreate(h, "ben", "web", "x", "hal")
		if c, _ := zgDo(h, "hal", "GET", "/projects/web/tasks", ""); c != 404 {
			t.Errorf("hal can now read web: %d", c)
		}
		if c, b := zgCreate(h, "hal", "web", "x", "hal"); c >= 200 && c < 300 {
			t.Errorf("hal can now create in web: %d %s", c, b)
		}
	})
	t.Run("AUTHZ_ViewerNotUpgradedCannotCreate", func(t *testing.T) {
		h := newServer()
		zgCreate(h, "ivy", "web", "x", "dan")
		zgMust(t, h, "dan", "POST", "/projects/web/tasks", zgBody("y", "dan"), 403)
		zgMust(t, h, "dan", "POST", "/projects/web/tasks", zgBody("y", ""), 403)
	})
	t.Run("AUTHZ_DeniedNoLeakBodies", func(t *testing.T) {
		h := newServer()
		for _, r := range []zgReq{{"hal", "web", "x", "hal"}, {"gus", "web", "x", "hal"}, {"", "web", "x", "hal"}, {"ann", "gx", "x", "gus"}, {"ivy", "web", "x", "gus"}, {"ben", "web", "x", "hal"}} {
			_, body := zgCreate(h, r.user, r.pid, r.title, r.assignee)
			if l := zgLeaks(r.user, body); len(l) > 0 {
				t.Errorf("leak: %v in %s", l, body)
			}
		}
	})

	// ---------- ATOM ----------
	t.Run("ATOM_SequenceOfDeniedRequestsChangesNothing", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		for _, r := range []zgReq{
			{"ben", "web", "a", "hal"}, {"cat", "ops", "a", "ben"}, {"ivy", "web", "a", "gus"}, {"ivy", "web", "a", "zed"},
			{"ivy", "web", "a", "dan"}, {"ann", "legacy", "a", "hal"}, {"dan", "web", "a", "hal"}, {"eve", "web", "a", "hal"},
			{"hal", "web", "a", "hal"}, {"gus", "gx", "a", "ann"}, {"ivy", "ops", "a", "ann"}, {"ivy", "web", "", "hal"},
		} {
			code, body := zgCreate(h, r.user, r.pid, r.title, r.assignee)
			if code >= 200 && code < 300 {
				t.Errorf("unexpected acceptance: %+v -> %d %s", r, code, body)
			}
		}
		if d := zgDiff(before, zgSnap(t, h)); d != "" {
			t.Errorf("state changed: %s", d)
		}
	})
	t.Run("ATOM_DeniedThenAllowed_OnlyAllowedTakesEffect", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		zgCreate(h, "cat", "web", "denied", "hal")
		zgCreate(h, "ivy", "web", "denied", "gus")
		zgCreated(t, h, "ivy", "web", "allowed", "hal")
		zgOnlyChange(t, before, zgSnap(t, h), "web", "allowed", "hal", map[string]string{"web/hal": "403/400"})
	})
	t.Run("ATOM_InvalidBodyDoesNotInvite", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		for _, body := range []string{`{"assignee":"hal"}`, `{"title":"","assignee":"hal"}`, `{"title":5,"assignee":"hal"}`, `{"title":"x","assignee":"hal"`, `not json`, `{"title":null,"assignee":"hal"}`} {
			code, resp := zgDo(h, "ivy", "POST", "/projects/web/tasks", body)
			if code >= 200 && code < 300 {
				t.Errorf("unexpected acceptance: body %q -> %d %s", body, code, resp)
			}
		}
		if d := zgDiff(before, zgSnap(t, h)); d != "" {
			t.Errorf("state changed: %s", d)
		}
	})
	t.Run("ATOM_ArchivedInviteAddsNoMember", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		for _, a := range []string{"hal", "dan", "cat", "eve"} {
			zgCreate(h, "ann", "legacy", "x", a)
		}
		if d := zgDiff(before, zgSnap(t, h)); d != "" {
			t.Errorf("state changed: %s", d)
		}
	})

	// ---------- VALID ----------
	for _, c := range []struct{ n, body string }{
		{"NoTitleWithNonMember", `{"assignee":"hal"}`},
		{"EmptyTitleWithNonMember", `{"title":"","assignee":"hal"}`},
		{"BadJSONWithNonMember", `{"title":"x","assignee":"hal"`},
		{"TitleWrongType", `{"title":5,"assignee":"hal"}`},
	} {
		c := c
		t.Run("VALID_"+c.n, func(t *testing.T) {
			h := newServer()
			before := zgSnap(t, h)
			code, resp := zgDo(h, "ivy", "POST", "/projects/web/tasks", c.body)
			if code >= 200 && code < 300 {
				t.Errorf("unexpected acceptance: %s -> %d %s", c.body, code, resp)
			}
			if d := zgDiff(before, zgSnap(t, h)); d != "" {
				t.Errorf("state changed: %s", d)
			}
		})
	}
	t.Run("VALID_EmptyAssigneeStillUnassignedNoInvite", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		zgCreated(t, h, "ivy", "web", "nobody", "")
		zgOnlyChange(t, before, zgSnap(t, h), "web", "nobody", "", nil)
	})

	// ---------- CONTRACT ----------
	for _, c := range []struct {
		n    string
		r    zgReq
		want int
	}{
		{"ContributorInvites403", zgReq{"ben", "web", "x", "hal"}, 403},
		{"ContributorInvitesOps403", zgReq{"cat", "ops", "x", "ben"}, 403},
		{"Viewer403", zgReq{"dan", "web", "x", "hal"}, 403},
		{"OrgAdmin403", zgReq{"eve", "web", "x", "hal"}, 403},
		{"NonMember404", zgReq{"hal", "web", "x", "hal"}, 404},
		{"CrossOrgCaller404", zgReq{"gus", "web", "x", "hal"}, 404},
		{"Anonymous401", zgReq{"", "web", "x", "hal"}, 401},
		{"UnknownCaller401", zgReq{"zed", "web", "x", "hal"}, 401},
		{"UnknownProject404", zgReq{"ann", "nope", "x", "hal"}, 404},
		{"Archived409", zgReq{"ann", "legacy", "x", "hal"}, 409},
		{"CrossOrgAssignee400", zgReq{"ivy", "web", "x", "gus"}, 400},
		{"UnknownAssignee400", zgReq{"ivy", "web", "x", "zed"}, 400},
		{"ViewerAssignee400", zgReq{"ivy", "web", "x", "dan"}, 400},
		{"EmptyTitle400", zgReq{"ivy", "web", "", "hal"}, 400},
	} {
		c := c
		t.Run("CONTRACT_"+c.n, func(t *testing.T) {
			h := newServer()
			code, body := zgCreate(h, c.r.user, c.r.pid, c.r.title, c.r.assignee)
			if code != c.want {
				t.Errorf("POST %s as %q assignee=%q -> %d, want %d body=%s", c.r.pid, c.r.user, c.r.assignee, code, c.want, body)
			}
		})
	}
	t.Run("CONTRACT_InviteResponseShapeAndContentType", func(t *testing.T) {
		h := newServer()
		req := httptest.NewRequest("POST", "/projects/web/tasks", strings.NewReader(zgBody("shape", "hal")))
		req.Header.Set("X-User", "ivy")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 201 {
			t.Fatalf("-> %d %s", rec.Code, rec.Body.String())
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Errorf("Content-Type = %q", ct)
		}
		var m map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if m["id"] == nil || m["projectId"] != "web" || m["title"] != "shape" || m["status"] != "open" || m["assignee"] != "hal" {
			t.Errorf("body = %s, want task {id, projectId:web, title, status:open, assignee:hal}", rec.Body.String())
		}
	})
	t.Run("CONTRACT_ErrorShape", func(t *testing.T) {
		h := newServer()
		code, body := zgCreate(h, "ben", "web", "x", "hal")
		var e map[string]string
		if code != 403 || json.Unmarshal([]byte(body), &e) != nil || e["error"] == "" {
			t.Errorf("-> %d body=%s, want 403 {\"error\":...}", code, body)
		}
	})

	// ---------- INFO ----------
	t.Run("INFO_AssignOrgAdminEve", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		code, body := zgCreate(h, "ivy", "web", "eve task", "eve")
		after := zgSnap(t, h)
		t.Logf("INFO ivy assigns web task to eve (org admin, non-member) -> %d body=%s; eve probe %s -> %s", code, strings.TrimSpace(body), before.sig["web/eve"], after.sig["web/eve"])
	})
	t.Run("INFO_InviteResponseBody", func(t *testing.T) {
		h := newServer()
		code, body := zgCreate(h, "ivy", "web", "x", "hal")
		t.Logf("INFO ivy invites hal -> %d body=%s", code, strings.TrimSpace(body))
	})
	t.Run("INFO_CheckOrder", func(t *testing.T) {
		h := newServer()
		for _, r := range []zgReq{
			{"ben", "web", "x", "hal"}, {"ben", "web", "x", "gus"}, {"ben", "web", "x", "zed"}, {"ben", "web", "x", "dan"},
			{"ben", "legacy", "x", "hal"}, {"ann", "legacy", "x", "gus"}, {"ann", "legacy", "x", "dan"}, {"cat", "ops", "x", "ben"},
			{"ivy", "web", "", "hal"}, {"dan", "web", "x", "gus"},
		} {
			code, body := zgCreate(h, r.user, r.pid, r.title, r.assignee)
			t.Logf("INFO %s %s title=%q assignee=%s -> %d %s", r.user, r.pid, r.title, r.assignee, code, strings.TrimSpace(body))
		}
	})
	t.Run("INFO_ProbeAfterInvite", func(t *testing.T) {
		h := newServer()
		zgCreate(h, "ivy", "web", "x", "hal")
		sig := zgSig(h)
		t.Logf("INFO after invite: web/hal=%s ops/hal=%s gx/hal=%s", sig["web/hal"], sig["ops/hal"], sig["gx/hal"])
	})
}
