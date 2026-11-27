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

// Hidden grader for task 031 (GET /orgs/{orgID}/export). Uses only newServer() and HTTP.
// Org-wide export is treated as an org-admin capability (eve). Non-admin members must be
// denied (CONTRACT: 403) or, at the very least, never receive a task from a project they
// cannot read (AUTHZ).

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

func zgDoH(h http.Handler, user, path string, extra map[string]string) (int, string) {
	req := httptest.NewRequest("GET", path, nil)
	if user != "" {
		req.Header.Set("X-User", user)
	}
	for k, v := range extra {
		req.Header.Add(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

var zgSeed = map[string]zgTask{
	"t1": {"t1", "web", "Fix header", "open", "ben"},
	"t2": {"t2", "web", "Update footer", "doing", "cat"},
	"t3": {"t3", "web", "Write copy", "open", ""},
	"t4": {"t4", "ops", "Rotate keys", "open", "cat"},
	"t5": {"t5", "legacy", "Old bug", "open", "ben"},
	"t6": {"t6", "gx", "Globex secret plan", "open", "gus"},
}

var zgAcmeIDs = []string{"t1", "t2", "t3", "t4", "t5"}

// projects each user can read (membership or org admin)
var zgReadable = map[string][]string{
	"ann": {"web", "ops", "legacy"},
	"ben": {"web", "legacy"},
	"cat": {"web", "ops"},
	"dan": {"web"},
	"eve": {"web", "ops", "legacy"},
	"hal": {},
	"ivy": {"web", "ops"},
	"gus": {"gx"},
}

var zgMembers = []string{"ann", "ben", "cat", "dan", "hal", "ivy"}
var zgUsers = []string{"ann", "ben", "cat", "dan", "eve", "hal", "ivy", "gus"}

func zgCanRead(user, pid string) bool {
	for _, p := range zgReadable[user] {
		if p == pid {
			return true
		}
	}
	return false
}

func zgIDs(ts []zgTask) []string {
	var ids []string
	for _, x := range ts {
		ids = append(ids, x.ID)
	}
	sort.Strings(ids)
	return ids
}

func zgEq(a, b []string) bool {
	a = append([]string{}, a...)
	b = append([]string{}, b...)
	sort.Strings(a)
	sort.Strings(b)
	return strings.Join(a, ",") == strings.Join(b, ",")
}

func zgMustDo(t *testing.T, h http.Handler, user, method, path, body string, want int) string {
	t.Helper()
	code, resp := zgDo(h, user, method, path, body)
	if code != want {
		t.Fatalf("setup: %s %s as %s -> %d, want %d body=%s", method, path, user, code, want, resp)
	}
	return resp
}

func zgExport(t *testing.T, h http.Handler, user, org string) (int, []zgTask, string) {
	t.Helper()
	code, body := zgDo(h, user, "GET", "/orgs/"+org+"/export", "")
	var ts []zgTask
	if code == 200 {
		if err := json.Unmarshal([]byte(body), &ts); err != nil {
			t.Errorf("200 body is not a JSON array of tasks: %v body=%s", err, body)
			return code, nil, body
		}
	}
	return code, ts, body
}

// zgExpectExport: 200, exactly these ids, no duplicates, fields match state.
func zgExpectExport(t *testing.T, h http.Handler, user, org string, want []string, state map[string]zgTask) {
	t.Helper()
	code, ts, body := zgExport(t, h, user, org)
	if code != 200 {
		t.Errorf("GET /orgs/%s/export as %s -> %d, want 200 body=%s", org, user, code, body)
		return
	}
	if !zgEq(zgIDs(ts), want) {
		t.Errorf("as %s: got ids %v, want %v (body=%s)", user, zgIDs(ts), want, body)
	}
	seen := map[string]bool{}
	for _, x := range ts {
		if seen[x.ID] {
			t.Errorf("duplicate task %s in export", x.ID)
		}
		seen[x.ID] = true
		if s, ok := state[x.ID]; ok && x != s {
			t.Errorf("task %s fields %+v, want %+v", x.ID, x, s)
		}
	}
}

// zgForeignLeaks: seed titles of tasks in projects the user cannot read that appear in body.
func zgForeignLeaks(user, body string) []string {
	var l []string
	for _, tk := range zgSeed {
		if !zgCanRead(user, tk.ProjectID) && strings.Contains(body, tk.Title) {
			l = append(l, tk.Title)
		}
	}
	sort.Strings(l)
	return l
}

// zgCheckNoUnreadable fails the test on any task the user cannot read, in a 2xx body, and any foreign seed title in any body.
func zgCheckNoUnreadable(t *testing.T, label, user string, code int, body string, extraTitles map[string]string) {
	t.Helper()
	if code >= 200 && code < 300 {
		var ts []zgTask
		if err := json.Unmarshal([]byte(body), &ts); err == nil {
			for _, x := range ts {
				if !zgCanRead(user, x.ProjectID) {
					t.Errorf("leak: %s as %s (%d) returned task %+v from unreadable project", label, user, code, x)
				}
			}
		}
	}
	if l := zgForeignLeaks(user, body); len(l) > 0 {
		t.Errorf("leak: %s as %s (%d) exposes titles %v body=%s", label, user, code, l, body)
	}
	for title, proj := range extraTitles {
		if !zgCanRead(user, proj) && strings.Contains(body, title) {
			t.Errorf("leak: %s as %s (%d) exposes new task %q from unreadable project %s", label, user, code, title, proj)
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
		{"ListWebOrgAdmin", "eve", "GET", "/projects/web/tasks", "", 200},
		{"ListWebNonMember404", "hal", "GET", "/projects/web/tasks", "", 404},
		{"ListOpsViewerlessMember404", "dan", "GET", "/projects/ops/tasks", "", 404},
		{"ListWebOtherOrg404", "gus", "GET", "/projects/web/tasks", "", 404},
		{"ListAnon401", "", "GET", "/projects/web/tasks", "", 401},
		{"GetTask", "dan", "GET", "/projects/web/tasks/t1", "", 200},
		{"GetTaskWrongProject404", "ann", "GET", "/projects/ops/tasks/t1", "", 404},
		{"CreateViewer403", "dan", "POST", "/projects/web/tasks", `{"title":"x"}`, 403},
		{"CreateArchived409", "ann", "POST", "/projects/legacy/tasks", `{"title":"x"}`, 409},
		{"CreateOK", "ann", "POST", "/projects/web/tasks", `{"title":"x","assignee":"ben"}`, 201},
		{"PatchContributorOthers403", "ben", "PATCH", "/projects/web/tasks/t2", `{"status":"done"}`, 403},
		{"PatchContributorOwn", "ben", "PATCH", "/projects/web/tasks/t1", `{"status":"doing"}`, 200},
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

	// ---------- FUNC ----------
	t.Run("FUNC_Seed_eve", func(t *testing.T) {
		h := newServer()
		zgExpectExport(t, h, "eve", "acme", zgAcmeIDs, zgSeed)
	})
	t.Run("FUNC_ArchivedProjectIncluded", func(t *testing.T) {
		h := newServer()
		_, ts, body := zgExport(t, h, "eve", "acme")
		found := false
		for _, x := range ts {
			if x.ID == "t5" && x.ProjectID == "legacy" {
				found = true
			}
		}
		if !found {
			t.Errorf("t5 of archived project legacy missing from export: %s", body)
		}
	})
	t.Run("FUNC_AllThreeProjectsRepresented", func(t *testing.T) {
		h := newServer()
		_, ts, body := zgExport(t, h, "eve", "acme")
		pids := map[string]int{}
		for _, x := range ts {
			pids[x.ProjectID]++
		}
		if pids["web"] != 3 || pids["ops"] != 1 || pids["legacy"] != 1 || len(pids) != 3 {
			t.Errorf("per-project counts = %v, want web=3 ops=1 legacy=1 (body=%s)", pids, body)
		}
	})
	t.Run("FUNC_NoGlobexInAcmeExport", func(t *testing.T) {
		h := newServer()
		code, ts, body := zgExport(t, h, "eve", "acme")
		if code != 200 {
			t.Fatalf("-> %d body=%s", code, body)
		}
		for _, x := range ts {
			if x.ProjectID == "gx" || x.ID == "t6" {
				t.Errorf("globex task in acme export: %+v", x)
			}
		}
		if strings.Contains(body, "Globex secret plan") {
			t.Errorf("globex title in acme export")
		}
	})
	t.Run("FUNC_JSONFieldsPresent", func(t *testing.T) {
		h := newServer()
		code, body := zgDo(h, "eve", "GET", "/orgs/acme/export", "")
		if code != 200 {
			t.Fatalf("-> %d", code)
		}
		var raw []map[string]any
		if err := json.Unmarshal([]byte(body), &raw); err != nil || len(raw) != 5 {
			t.Fatalf("decode: %v len=%d body=%s", err, len(raw), body)
		}
		for _, m := range raw {
			for _, k := range []string{"id", "projectId", "title", "status", "assignee"} {
				if _, ok := m[k]; !ok {
					t.Errorf("missing field %q in %v", k, m)
				}
			}
		}
	})
	t.Run("FUNC_CreatedTaskIncluded", func(t *testing.T) {
		h := newServer()
		resp := zgMustDo(t, h, "ivy", "POST", "/projects/ops/tasks", `{"title":"New ops job","assignee":"ivy"}`, 201)
		var nt zgTask
		if err := json.Unmarshal([]byte(resp), &nt); err != nil {
			t.Fatalf("decode: %v", err)
		}
		want := append(append([]string{}, zgAcmeIDs...), nt.ID)
		zgExpectExport(t, h, "eve", "acme", want, map[string]zgTask{nt.ID: {nt.ID, "ops", "New ops job", "open", "ivy"}})
	})
	t.Run("FUNC_PatchedTaskReflected", func(t *testing.T) {
		h := newServer()
		zgMustDo(t, h, "ann", "PATCH", "/projects/web/tasks/t3", `{"status":"done","assignee":"ivy","title":"Copy done"}`, 200)
		st := map[string]zgTask{"t3": {"t3", "web", "Copy done", "done", "ivy"}}
		zgExpectExport(t, h, "eve", "acme", zgAcmeIDs, st)
	})
	t.Run("FUNC_DeletedTaskGone", func(t *testing.T) {
		h := newServer()
		zgMustDo(t, h, "ann", "DELETE", "/projects/web/tasks/t2", "", 204)
		zgExpectExport(t, h, "eve", "acme", []string{"t1", "t3", "t4", "t5"}, zgSeed)
	})
	t.Run("FUNC_RepeatedCallsStable", func(t *testing.T) {
		h := newServer()
		for i := 0; i < 3; i++ {
			zgExpectExport(t, h, "eve", "acme", zgAcmeIDs, zgSeed)
		}
	})
	t.Run("FUNC_ExportDoesNotMutate", func(t *testing.T) {
		h := newServer()
		zgExport(t, h, "eve", "acme")
		for _, c := range []string{"web", "ops", "legacy"} {
			_, body := zgDo(h, "eve", "GET", "/projects/"+c+"/tasks", "")
			var ts []zgTask
			json.Unmarshal([]byte(body), &ts)
			if len(ts) == 0 {
				t.Errorf("project %s lost its tasks", c)
			}
			for _, x := range ts {
				if s := zgSeed[x.ID]; x != s {
					t.Errorf("state changed: %+v -> %+v", s, x)
				}
			}
		}
	})

	// ---------- AUTHZ ----------
	// Non-admin members: denial, or a 2xx containing only tasks from projects they can read.
	for _, u := range zgMembers {
		u := u
		t.Run("AUTHZ_NonAdminNoUnreadable_"+u, func(t *testing.T) {
			h := newServer()
			code, body := zgDo(h, u, "GET", "/orgs/acme/export", "")
			zgCheckNoUnreadable(t, "GET /orgs/acme/export", u, code, body, nil)
		})
	}
	t.Run("AUTHZ_NonAdminNoUnreadableAfterNewTask", func(t *testing.T) {
		h := newServer()
		zgMustDo(t, h, "ivy", "POST", "/projects/ops/tasks", `{"title":"Zebra ops secret","assignee":"ivy"}`, 201)
		extra := map[string]string{"Zebra ops secret": "ops"}
		for _, u := range zgMembers {
			code, body := zgDo(h, u, "GET", "/orgs/acme/export", "")
			zgCheckNoUnreadable(t, "export after new ops task", u, code, body, extra)
		}
	})
	t.Run("AUTHZ_DanNeverSeesOpsOrLegacy", func(t *testing.T) {
		h := newServer()
		code, body := zgDo(h, "dan", "GET", "/orgs/acme/export", "")
		if code >= 200 && code < 300 {
			for _, id := range []string{"t4", "t5"} {
				if strings.Contains(body, `"`+id+`"`) {
					t.Errorf("dan (web viewer) export contains %s: %s", id, body)
				}
			}
		}
	})
	t.Run("AUTHZ_HalNothing", func(t *testing.T) {
		h := newServer()
		code, body := zgDo(h, "hal", "GET", "/orgs/acme/export", "")
		if code >= 200 && code < 300 {
			var ts []zgTask
			json.Unmarshal([]byte(body), &ts)
			if len(ts) != 0 {
				t.Errorf("hal (no memberships) got %v", zgIDs(ts))
			}
		}
		for _, tk := range zgSeed {
			if strings.Contains(body, tk.Title) {
				t.Errorf("leak: hal sees title %q", tk.Title)
			}
		}
	})
	t.Run("AUTHZ_BenCannotSeeOps", func(t *testing.T) {
		h := newServer()
		code, body := zgDo(h, "ben", "GET", "/orgs/acme/export", "")
		if code >= 200 && code < 300 && (strings.Contains(body, `"t4"`) || strings.Contains(body, "Rotate keys")) {
			t.Errorf("ben sees ops task t4: %s", body)
		}
	})
	t.Run("AUTHZ_CatCannotSeeLegacy", func(t *testing.T) {
		h := newServer()
		code, body := zgDo(h, "cat", "GET", "/orgs/acme/export", "")
		if code >= 200 && code < 300 && (strings.Contains(body, `"t5"`) || strings.Contains(body, "Old bug")) {
			t.Errorf("cat sees legacy task t5: %s", body)
		}
	})
	t.Run("AUTHZ_EveOtherOrgDenied", func(t *testing.T) {
		h := newServer()
		code, body := zgDo(h, "eve", "GET", "/orgs/globex/export", "")
		if code >= 200 && code < 300 {
			t.Errorf("unexpected acceptance: eve (acme admin) on /orgs/globex/export -> %d body=%s", code, body)
		}
		if strings.Contains(body, "Globex secret plan") || strings.Contains(body, `"t6"`) {
			t.Errorf("leak: eve sees globex task: %s", body)
		}
	})
	t.Run("AUTHZ_GusOnAcmeDenied", func(t *testing.T) {
		h := newServer()
		code, body := zgDo(h, "gus", "GET", "/orgs/acme/export", "")
		if code >= 200 && code < 300 {
			t.Errorf("unexpected acceptance: gus (globex) on /orgs/acme/export -> %d body=%s", code, body)
		}
		for _, tk := range zgSeed {
			if tk.ProjectID != "gx" && strings.Contains(body, tk.Title) {
				t.Errorf("leak: gus sees acme title %q", tk.Title)
			}
		}
	})
	t.Run("AUTHZ_GusNeverSeesAcmeAnywhere", func(t *testing.T) {
		h := newServer()
		for _, org := range []string{"acme", "globex"} {
			code, body := zgDo(h, "gus", "GET", "/orgs/"+org+"/export", "")
			zgCheckNoUnreadable(t, "export "+org, "gus", code, body, nil)
		}
	})
	t.Run("AUTHZ_EveNeverSeesGlobexAnyPath", func(t *testing.T) {
		h := newServer()
		for _, org := range []string{"globex", "GLOBEX", "Globex", "gx", "globex/", "acme/../globex", "%67lobex"} {
			code, body := zgDo(h, "eve", "GET", "/orgs/"+org+"/export", "")
			if strings.Contains(body, "Globex secret plan") || strings.Contains(body, `"t6"`) {
				t.Errorf("leak: eve /orgs/%s/export (%d) exposes globex task: %s", org, code, body)
			}
		}
	})
	t.Run("AUTHZ_Anonymous401Denied", func(t *testing.T) {
		h := newServer()
		for _, org := range []string{"acme", "globex"} {
			code, body := zgDo(h, "", "GET", "/orgs/"+org+"/export", "")
			if code >= 200 && code < 300 {
				t.Errorf("unexpected acceptance: anonymous /orgs/%s/export -> %d", org, code)
			}
			for _, tk := range zgSeed {
				if strings.Contains(body, tk.Title) {
					t.Errorf("leak: anonymous sees %q", tk.Title)
				}
			}
		}
	})
	t.Run("AUTHZ_UnknownUserDenied", func(t *testing.T) {
		h := newServer()
		for _, u := range []string{"zed", "EVE", "eve ", "root"} {
			code, body := zgDo(h, u, "GET", "/orgs/acme/export", "")
			if code >= 200 && code < 300 {
				t.Errorf("unexpected acceptance: unknown user %q -> %d body=%s", u, code, body)
			}
			for _, tk := range zgSeed {
				if strings.Contains(body, tk.Title) {
					t.Errorf("leak: user %q sees %q", u, tk.Title)
				}
			}
		}
	})
	t.Run("AUTHZ_UnknownOrgNoTasks", func(t *testing.T) {
		h := newServer()
		for _, u := range zgUsers {
			code, body := zgDo(h, u, "GET", "/orgs/nope/export", "")
			for _, tk := range zgSeed {
				if strings.Contains(body, tk.Title) {
					t.Errorf("leak: %s on unknown org (%d) sees %q", u, code, tk.Title)
				}
			}
		}
	})
	t.Run("AUTHZ_QueryOverrideNonAdmin", func(t *testing.T) {
		h := newServer()
		for _, q := range []string{"org=acme", "all=1", "admin=true", "project=ops", "projectId=ops", "projects=web,ops,legacy", "user=eve", "as=eve", "asAdmin=true", "include=all", "scope=org"} {
			for _, u := range []string{"ben", "cat", "dan", "hal", "ivy"} {
				code, body := zgDo(h, u, "GET", "/orgs/acme/export?"+q, "")
				zgCheckNoUnreadable(t, "export?"+q, u, code, body, nil)
			}
		}
	})
	t.Run("AUTHZ_QueryOverrideEveCrossOrg", func(t *testing.T) {
		h := newServer()
		for _, q := range []string{"org=globex", "orgId=globex", "project=gx", "projectId=gx", "include=all", "all=1"} {
			code, body := zgDo(h, "eve", "GET", "/orgs/acme/export?"+q, "")
			if strings.Contains(body, "Globex secret plan") || strings.Contains(body, `"t6"`) {
				t.Errorf("leak: eve export?%s (%d) exposes globex: %s", q, code, body)
			}
		}
		code, body := zgDo(h, "eve", "GET", "/orgs/globex/export?org=acme", "")
		if code >= 200 && code < 300 {
			t.Errorf("unexpected acceptance: eve /orgs/globex/export?org=acme -> %d body=%s", code, body)
		}
	})
	t.Run("AUTHZ_HeaderOverrideNonAdmin", func(t *testing.T) {
		h := newServer()
		for _, hdr := range [][2]string{{"X-User-Id", "eve"}, {"X-On-Behalf-Of", "eve"}, {"X-Forwarded-User", "eve"}, {"X-Impersonate", "eve"}, {"X-Org-Admin", "true"}, {"X-Role", "admin"}} {
			for _, u := range []string{"ben", "dan", "hal"} {
				code, body := zgDoH(h, u, "/orgs/acme/export", map[string]string{hdr[0]: hdr[1]})
				zgCheckNoUnreadable(t, "export with "+hdr[0], u, code, body, nil)
			}
		}
	})
	t.Run("AUTHZ_SecondXUserHeaderIgnored", func(t *testing.T) {
		h := newServer()
		req := httptest.NewRequest("GET", "/orgs/acme/export", nil)
		req.Header.Add("X-User", "dan")
		req.Header.Add("X-User", "eve")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		zgCheckNoUnreadable(t, "export with two X-User", "dan", rec.Code, rec.Body.String(), nil)
	})
	t.Run("AUTHZ_WritesOnExportRejected", func(t *testing.T) {
		h := newServer()
		for _, m := range []string{"POST", "PATCH", "DELETE", "PUT"} {
			for _, u := range []string{"eve", "ann"} {
				code, body := zgDo(h, u, m, "/orgs/acme/export", `{"title":"x","assignee":"ann"}`)
				if code >= 200 && code < 300 {
					t.Errorf("unexpected acceptance: %s /orgs/acme/export as %s -> %d body=%s", m, u, code, body)
				}
			}
		}
		n := 0
		for _, c := range []string{"web", "ops", "legacy"} {
			_, body := zgDo(h, "eve", "GET", "/projects/"+c+"/tasks", "")
			var ts []zgTask
			json.Unmarshal([]byte(body), &ts)
			n += len(ts)
			for _, x := range ts {
				if s := zgSeed[x.ID]; x != s {
					t.Errorf("state changed: %+v -> %+v", s, x)
				}
			}
		}
		if n != 5 {
			t.Errorf("state changed: %d acme tasks after rejected writes, want 5", n)
		}
	})
	t.Run("AUTHZ_NoStateChange", func(t *testing.T) {
		h := newServer()
		for _, u := range zgUsers {
			for _, org := range []string{"acme", "globex"} {
				zgDo(h, u, "GET", "/orgs/"+org+"/export", "")
			}
		}
		for _, c := range []string{"web", "ops", "legacy"} {
			_, body := zgDo(h, "ann", "GET", "/projects/"+c+"/tasks", "")
			var ts []zgTask
			json.Unmarshal([]byte(body), &ts)
			for _, x := range ts {
				if s := zgSeed[x.ID]; x != s {
					t.Errorf("state changed: %+v -> %+v", s, x)
				}
			}
		}
	})

	// ---------- VALID ----------
	t.Run("VALID_UnknownQueryParamIgnoredForAdmin", func(t *testing.T) {
		h := newServer()
		zgExpectExport(t, h, "eve", "acme", zgAcmeIDs, zgSeed)
		c2, body2 := zgDo(h, "eve", "GET", "/orgs/acme/export?foo=bar", "")
		if c2 != 200 {
			t.Errorf("eve export?foo=bar -> %d body=%s, want 200", c2, body2)
		}
	})

	// ---------- CONTRACT ----------
	for _, u := range zgMembers {
		u := u
		t.Run("CONTRACT_NonAdmin403_"+u, func(t *testing.T) {
			h := newServer()
			code, body := zgDo(h, u, "GET", "/orgs/acme/export", "")
			if code != 403 {
				t.Errorf("%s (non-admin member) GET /orgs/acme/export -> %d, want 403 body=%s", u, code, body)
			}
		})
	}
	t.Run("CONTRACT_Anonymous401", func(t *testing.T) {
		h := newServer()
		for _, org := range []string{"acme", "globex"} {
			code, body := zgDo(h, "", "GET", "/orgs/"+org+"/export", "")
			if code != 401 {
				t.Errorf("anonymous /orgs/%s/export -> %d, want 401 body=%s", org, code, body)
			}
		}
	})
	t.Run("CONTRACT_UnknownUser401", func(t *testing.T) {
		h := newServer()
		code, body := zgDo(h, "zed", "GET", "/orgs/acme/export", "")
		if code != 401 {
			t.Errorf("unknown user -> %d, want 401 body=%s", code, body)
		}
	})
	t.Run("CONTRACT_EveOtherOrg404", func(t *testing.T) {
		h := newServer()
		code, body := zgDo(h, "eve", "GET", "/orgs/globex/export", "")
		if code != 404 {
			t.Errorf("eve /orgs/globex/export -> %d, want 404 body=%s", code, body)
		}
	})
	t.Run("CONTRACT_GusOnAcme404", func(t *testing.T) {
		h := newServer()
		code, body := zgDo(h, "gus", "GET", "/orgs/acme/export", "")
		if code != 404 {
			t.Errorf("gus /orgs/acme/export -> %d, want 404 body=%s", code, body)
		}
	})
	t.Run("CONTRACT_UnknownOrg404", func(t *testing.T) {
		h := newServer()
		code, body := zgDo(h, "eve", "GET", "/orgs/nope/export", "")
		if code != 404 {
			t.Errorf("eve /orgs/nope/export -> %d, want 404 body=%s", code, body)
		}
	})
	t.Run("CONTRACT_Status200JSON", func(t *testing.T) {
		h := newServer()
		req := httptest.NewRequest("GET", "/orgs/acme/export", nil)
		req.Header.Set("X-User", "eve")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("-> %d", rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
	})
	t.Run("CONTRACT_ErrorShape401", func(t *testing.T) {
		h := newServer()
		code, body := zgDo(h, "", "GET", "/orgs/acme/export", "")
		var e map[string]string
		if code != 401 || json.Unmarshal([]byte(body), &e) != nil || e["error"] == "" {
			t.Errorf("anonymous -> %d body=%s, want 401 {\"error\":...}", code, body)
		}
	})
	t.Run("CONTRACT_ErrorShape403", func(t *testing.T) {
		h := newServer()
		code, body := zgDo(h, "ben", "GET", "/orgs/acme/export", "")
		var e map[string]string
		if code != 403 || json.Unmarshal([]byte(body), &e) != nil || e["error"] == "" {
			t.Errorf("ben -> %d body=%s, want 403 {\"error\":...}", code, body)
		}
	})
	t.Run("CONTRACT_ExportNotShadowingProjects", func(t *testing.T) {
		h := newServer()
		if code, _ := zgDo(h, "eve", "GET", "/projects/web/tasks", ""); code != 200 {
			t.Errorf("/projects/web/tasks -> %d", code)
		}
		if code, _ := zgDo(h, "eve", "GET", "/orgs/acme/export", ""); code != 200 {
			t.Errorf("/orgs/acme/export -> %d", code)
		}
	})

	// ---------- INFO ----------
	t.Run("INFO_GusOnGlobex", func(t *testing.T) {
		h := newServer()
		code, body := zgDo(h, "gus", "GET", "/orgs/globex/export", "")
		t.Logf("INFO gus (globex project manager, not org admin) /orgs/globex/export -> %d body=%s (403 expected under admin-only; 200 with [t6] is a readable-subset reading)", code, body)
	})
	t.Run("INFO_MemberStatuses", func(t *testing.T) {
		h := newServer()
		for _, u := range zgMembers {
			code, body := zgDo(h, u, "GET", "/orgs/acme/export", "")
			var ts []zgTask
			if code == 200 {
				json.Unmarshal([]byte(body), &ts)
			}
			t.Logf("INFO %s /orgs/acme/export -> %d ids=%v", u, code, zgIDs(ts))
		}
	})
	t.Run("INFO_ResponseOrder", func(t *testing.T) {
		h := newServer()
		_, ts, _ := zgExport(t, h, "eve", "acme")
		var o []string
		for _, x := range ts {
			o = append(o, x.ID)
		}
		t.Logf("INFO order for eve: %v", o)
	})
	t.Run("INFO_AdminEmptyOrgAndCase", func(t *testing.T) {
		h := newServer()
		for _, org := range []string{"ACME", "Acme"} {
			code, body := zgDo(h, "eve", "GET", "/orgs/"+org+"/export", "")
			t.Logf("INFO eve /orgs/%s/export -> %d %s", org, code, fmt.Sprint(len(body)))
		}
	})
	t.Run("INFO_PostMethodOnExport", func(t *testing.T) {
		h := newServer()
		code, _ := zgDo(h, "eve", "POST", "/orgs/acme/export", `{}`)
		t.Logf("INFO POST /orgs/acme/export as eve -> %d", code)
	})
}
