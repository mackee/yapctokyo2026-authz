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

// Hidden grader for task 006 (GET /me/tasks). Uses only newServer() and HTTP.

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

// zgDoH is like zgDo but with extra raw headers.
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

var zgUsers = []string{"ann", "ben", "cat", "dan", "eve", "hal", "ivy", "gus"}

// expected seed ids per user
var zgWant = map[string][]string{
	"ann": {}, "ben": {"t1", "t5"}, "cat": {"t2", "t4"}, "dan": {}, "eve": {},
	"hal": {}, "ivy": {}, "gus": {"t6"},
}

// zgMe fetches /me/tasks (with a raw query) and decodes it strictly as an array.
func zgMe(t *testing.T, h http.Handler, user, query string) (int, []zgTask, string) {
	t.Helper()
	p := "/me/tasks"
	if query != "" {
		p += "?" + query
	}
	code, body := zgDo(h, user, "GET", p, "")
	var ts []zgTask
	if code == 200 {
		if err := json.Unmarshal([]byte(body), &ts); err != nil {
			t.Errorf("200 body is not a JSON array of tasks: %v body=%s", err, body)
			return code, nil, body
		}
	}
	return code, ts, body
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
	sort.Strings(a)
	sort.Strings(b)
	return strings.Join(a, ",") == strings.Join(b, ",")
}

// zgExpectIDs: 200, exactly these ids, no duplicates, fields match the task state.
func zgExpectIDs(t *testing.T, h http.Handler, user, query string, want []string, state map[string]zgTask) {
	t.Helper()
	code, ts, body := zgMe(t, h, user, query)
	if code != 200 {
		t.Errorf("GET /me/tasks?%s as %s -> %d, want 200 body=%s", query, user, code, body)
		return
	}
	got := zgIDs(ts)
	if !zgEq(append([]string{}, got...), append([]string{}, want...)) {
		t.Errorf("as %s ?%s: got ids %v, want %v (body=%s)", user, query, got, want, body)
	}
	seen := map[string]bool{}
	for _, x := range ts {
		if seen[x.ID] {
			t.Errorf("duplicate task %s in response", x.ID)
		}
		seen[x.ID] = true
		if s, ok := state[x.ID]; ok && x != s {
			t.Errorf("task %s fields %+v, want %+v", x.ID, x, s)
		}
	}
}

// zgLeakTitles: titles of seed tasks not assigned to user that appear in body.
func zgLeakTitles(user, body string) []string {
	var l []string
	for _, tk := range zgSeed {
		if tk.Assignee != user && strings.Contains(body, tk.Title) {
			l = append(l, tk.Title)
		}
	}
	return l
}

func zgMustDo(t *testing.T, h http.Handler, user, method, path, body string, want int) string {
	t.Helper()
	code, resp := zgDo(h, user, method, path, body)
	if code != want {
		t.Fatalf("setup: %s %s as %s -> %d, want %d body=%s", method, path, user, code, want, resp)
	}
	return resp
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
		{"ListWebOtherOrg404", "gus", "GET", "/projects/web/tasks", "", 404},
		{"ListAnon401", "", "GET", "/projects/web/tasks", "", 401},
		{"GetTask", "dan", "GET", "/projects/web/tasks/t1", "", 200},
		{"GetTaskWrongProject404", "ann", "GET", "/projects/ops/tasks/t1", "", 404},
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

	// ---------- FUNC ----------
	for _, u := range zgUsers {
		u := u
		t.Run("FUNC_Seed_"+u, func(t *testing.T) {
			h := newServer()
			zgExpectIDs(t, h, u, "", zgWant[u], zgSeed)
		})
	}
	t.Run("FUNC_EmptyIsArrayNotNull", func(t *testing.T) {
		h := newServer()
		for _, u := range []string{"ann", "dan", "eve", "hal", "ivy"} {
			code, body := zgDo(h, u, "GET", "/me/tasks", "")
			if code != 200 {
				t.Errorf("%s -> %d, want 200", u, code)
				continue
			}
			if strings.TrimSpace(body) != "[]" {
				t.Errorf("%s: empty result body=%q, want []", u, body)
			}
		}
	})
	t.Run("FUNC_ProjectIdPresent", func(t *testing.T) {
		h := newServer()
		code, body := zgDo(h, "ben", "GET", "/me/tasks", "")
		if code != 200 {
			t.Fatalf("-> %d", code)
		}
		var raw []map[string]any
		if err := json.Unmarshal([]byte(body), &raw); err != nil {
			t.Fatalf("decode: %v body=%s", err, body)
		}
		got := map[string]any{}
		for _, m := range raw {
			got[fmt.Sprint(m["id"])] = m["projectId"]
		}
		if got["t1"] != "web" || got["t5"] != "legacy" {
			t.Errorf("projectId by task = %v, want t1=web t5=legacy (body=%s)", got, body)
		}
	})
	t.Run("FUNC_CrossProjectCat", func(t *testing.T) {
		h := newServer()
		_, ts, _ := zgMe(t, h, "cat", "")
		pids := map[string]string{}
		for _, x := range ts {
			pids[x.ID] = x.ProjectID
		}
		if pids["t2"] != "web" || pids["t4"] != "ops" || len(pids) != 2 {
			t.Errorf("cat tasks/projects = %v, want t2=web t4=ops", pids)
		}
	})
	t.Run("FUNC_ArchivedProjectIncluded", func(t *testing.T) {
		h := newServer()
		_, ts, body := zgMe(t, h, "ben", "")
		found := false
		for _, x := range ts {
			if x.ID == "t5" {
				found = true
			}
		}
		if !found {
			t.Errorf("ben's task t5 in archived project 'legacy' missing: %s", body)
		}
	})
	for _, c := range []struct {
		n, user, q string
		want       []string
	}{
		{"StatusOpenBen", "ben", "status=open", []string{"t1", "t5"}},
		{"StatusOpenCat", "cat", "status=open", []string{"t4"}},
		{"StatusDoingCat", "cat", "status=doing", []string{"t2"}},
		{"StatusDoneCat", "cat", "status=done", nil},
		{"StatusDoingBen", "ben", "status=doing", nil},
		{"StatusOpenGus", "gus", "status=open", []string{"t6"}},
		{"StatusOpenAnn", "ann", "status=open", nil},
	} {
		c := c
		t.Run("FUNC_Filter_"+c.n, func(t *testing.T) {
			h := newServer()
			zgExpectIDs(t, h, c.user, c.q, c.want, zgSeed)
		})
	}
	t.Run("FUNC_FilterAfterPatch", func(t *testing.T) {
		h := newServer()
		zgMustDo(t, h, "ben", "PATCH", "/projects/web/tasks/t1", `{"status":"doing"}`, 200)
		st := map[string]zgTask{"t1": {"t1", "web", "Fix header", "doing", "ben"}}
		zgExpectIDs(t, h, "ben", "status=doing", []string{"t1"}, st)
		zgExpectIDs(t, h, "ben", "status=open", []string{"t5"}, st)
		zgExpectIDs(t, h, "ben", "", []string{"t1", "t5"}, st)
	})
	t.Run("FUNC_FilterDoneAfterPatch", func(t *testing.T) {
		h := newServer()
		zgMustDo(t, h, "cat", "PATCH", "/projects/ops/tasks/t4", `{"status":"done"}`, 200)
		zgExpectIDs(t, h, "cat", "status=done", []string{"t4"}, nil)
		zgExpectIDs(t, h, "cat", "status=open", nil, nil)
		zgExpectIDs(t, h, "cat", "status=doing", []string{"t2"}, nil)
	})
	t.Run("FUNC_CreatedTaskForIvy", func(t *testing.T) {
		h := newServer()
		resp := zgMustDo(t, h, "ivy", "POST", "/projects/ops/tasks", `{"title":"New ops job","assignee":"ivy"}`, 201)
		var nt zgTask
		if err := json.Unmarshal([]byte(resp), &nt); err != nil {
			t.Fatalf("decode: %v", err)
		}
		zgExpectIDs(t, h, "ivy", "", []string{nt.ID}, map[string]zgTask{nt.ID: {nt.ID, "ops", "New ops job", "open", "ivy"}})
		zgExpectIDs(t, h, "ivy", "status=open", []string{nt.ID}, nil)
		zgExpectIDs(t, h, "ivy", "status=done", nil, nil)
		zgExpectIDs(t, h, "cat", "", []string{"t2", "t4"}, nil)
	})
	t.Run("FUNC_ReassignMoves", func(t *testing.T) {
		h := newServer()
		zgMustDo(t, h, "ann", "PATCH", "/projects/web/tasks/t3", `{"assignee":"ivy"}`, 200)
		zgMustDo(t, h, "ann", "PATCH", "/projects/web/tasks/t1", `{"assignee":"cat"}`, 200)
		zgExpectIDs(t, h, "ivy", "", []string{"t3"}, nil)
		zgExpectIDs(t, h, "cat", "", []string{"t1", "t2", "t4"}, nil)
		zgExpectIDs(t, h, "ben", "", []string{"t5"}, nil)
	})
	t.Run("FUNC_DeletedTaskGone", func(t *testing.T) {
		h := newServer()
		zgMustDo(t, h, "ann", "DELETE", "/projects/web/tasks/t2", "", 204)
		zgExpectIDs(t, h, "cat", "", []string{"t4"}, nil)
	})
	t.Run("FUNC_UnassignedTaskNobody", func(t *testing.T) {
		h := newServer()
		for _, u := range zgUsers {
			_, ts, _ := zgMe(t, h, u, "")
			for _, x := range ts {
				if x.ID == "t3" || x.Assignee == "" {
					t.Errorf("%s sees unassigned task %s", u, x.ID)
				}
			}
		}
	})
	t.Run("FUNC_RepeatedCallsStable", func(t *testing.T) {
		h := newServer()
		for i := 0; i < 3; i++ {
			zgExpectIDs(t, h, "cat", "", []string{"t2", "t4"}, zgSeed)
		}
		zgExpectIDs(t, h, "cat", "status=open", []string{"t4"}, zgSeed)
		zgExpectIDs(t, h, "cat", "", []string{"t2", "t4"}, zgSeed)
	})

	// ---------- AUTHZ ----------
	t.Run("AUTHZ_Anonymous401", func(t *testing.T) {
		h := newServer()
		code, body := zgDo(h, "", "GET", "/me/tasks", "")
		if code != 401 {
			t.Errorf("anonymous -> %d, want 401 body=%s", code, body)
		}
		if l := zgLeakTitles("", body); len(l) > 0 {
			t.Errorf("leak: %v", l)
		}
	})
	t.Run("AUTHZ_AnonymousWithQuery401", func(t *testing.T) {
		h := newServer()
		for _, q := range []string{"status=open", "assignee=cat", "user=cat"} {
			code, body := zgDo(h, "", "GET", "/me/tasks?"+q, "")
			if code != 401 {
				t.Errorf("anonymous ?%s -> %d, want 401 body=%s", q, code, body)
			}
			if l := zgLeakTitles("", body); len(l) > 0 {
				t.Errorf("leak: %v", l)
			}
		}
	})
	t.Run("AUTHZ_UnknownUser401", func(t *testing.T) {
		h := newServer()
		for _, u := range []string{"zed", "ANN", "cat ", "root"} {
			code, body := zgDo(h, u, "GET", "/me/tasks", "")
			if code != 401 {
				t.Errorf("unknown user %q -> %d, want 401 body=%s", u, code, body)
			}
			if l := zgLeakTitles("", body); len(l) > 0 {
				t.Errorf("leak: %v", l)
			}
		}
	})
	for _, p := range []string{"assignee", "user", "userId", "user_id", "u", "as", "me", "owner", "assigneeId", "X-User", "uid"} {
		p := p
		t.Run("AUTHZ_QueryOverride_"+p, func(t *testing.T) {
			h := newServer()
			// ann has no tasks; asking for cat's / ben's / gus's must not return them.
			for _, victim := range []string{"cat", "ben", "gus"} {
				code, body := zgDo(h, "ann", "GET", "/me/tasks?"+p+"="+victim, "")
				if code >= 200 && code < 300 {
					var ts []zgTask
					json.Unmarshal([]byte(body), &ts)
					if len(ts) != 0 {
						t.Errorf("identity override: ann ?%s=%s got %v", p, victim, zgIDs(ts))
					}
				}
				if l := zgLeakTitles("ann", body); len(l) > 0 {
					t.Errorf("leak: ann ?%s=%s exposes titles %v", p, victim, l)
				}
			}
		})
	}
	t.Run("AUTHZ_QueryOverrideKeepsOwn", func(t *testing.T) {
		h := newServer()
		// cat asking for ben's tasks must still get exactly cat's.
		for _, q := range []string{"assignee=ben", "user=ben", "userId=ben", "assignee=ben&status=open", "assignee=&user="} {
			code, ts, body := zgMe(t, h, "cat", q)
			if code != 200 {
				continue
			}
			for _, x := range ts {
				if x.Assignee != "cat" {
					t.Errorf("identity override: cat ?%s got foreign task %+v", q, x)
				}
			}
			if l := zgLeakTitles("cat", body); len(l) > 0 {
				t.Errorf("leak: cat ?%s titles %v", q, l)
			}
		}
	})
	t.Run("AUTHZ_HeaderOverride", func(t *testing.T) {
		h := newServer()
		for _, hdr := range []string{"X-User-Id", "X-Assignee", "X-On-Behalf-Of", "X-Forwarded-User", "X-Impersonate"} {
			code, body := zgDoH(h, "ann", "/me/tasks", map[string]string{hdr: "cat"})
			if code >= 200 && code < 300 {
				var ts []zgTask
				json.Unmarshal([]byte(body), &ts)
				if len(ts) != 0 {
					t.Errorf("header override %s: ann got %v", hdr, zgIDs(ts))
				}
			}
			if l := zgLeakTitles("ann", body); len(l) > 0 {
				t.Errorf("leak via %s: %v", hdr, l)
			}
		}
	})
	t.Run("AUTHZ_SecondXUserHeaderIgnored", func(t *testing.T) {
		h := newServer()
		req := httptest.NewRequest("GET", "/me/tasks", nil)
		req.Header.Add("X-User", "ann")
		req.Header.Add("X-User", "cat")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		// Go's Header.Get returns the first value (ann); any answer containing cat's tasks is wrong.
		if rec.Code == 200 {
			var ts []zgTask
			json.Unmarshal(rec.Body.Bytes(), &ts)
			for _, x := range ts {
				if x.Assignee != "ann" {
					t.Errorf("second X-User header honoured: got %+v", x)
				}
			}
		}
	})
	t.Run("AUTHZ_CrossOrgNeverMixes", func(t *testing.T) {
		h := newServer()
		acme := map[string]bool{}
		for _, u := range []string{"ann", "ben", "cat", "dan", "eve", "hal", "ivy"} {
			for _, q := range []string{"", "status=open", "status=doing", "assignee=gus", "user=gus"} {
				code, ts, body := zgMe(t, h, u, q)
				if code == 200 {
					for _, x := range ts {
						if x.ProjectID == "gx" || x.ID == "t6" {
							t.Errorf("cross-org leak: %s ?%s sees gx task %+v", u, q, x)
						}
					}
				}
				if strings.Contains(body, "Globex secret plan") {
					t.Errorf("leak: %s ?%s exposes Globex title", u, q)
				}
				acme[u] = true
			}
		}
		for _, q := range []string{"", "status=open", "assignee=cat", "user=ann"} {
			code, ts, body := zgMe(t, h, "gus", q)
			if code == 200 {
				for _, x := range ts {
					if x.ProjectID != "gx" {
						t.Errorf("cross-org leak: gus ?%s sees non-gx task %+v", q, x)
					}
				}
			}
			for _, tk := range zgSeed {
				if tk.ProjectID != "gx" && strings.Contains(body, tk.Title) {
					t.Errorf("leak: gus ?%s exposes acme title %q", q, tk.Title)
				}
			}
		}
	})
	t.Run("AUTHZ_OrgAdminSeesOnlyOwn", func(t *testing.T) {
		h := newServer()
		// eve can read every acme project but is assigned nothing.
		for _, q := range []string{"", "status=open", "status=doing"} {
			code, ts, body := zgMe(t, h, "eve", q)
			if code == 200 && len(ts) != 0 {
				t.Errorf("eve ?%s got non-assigned tasks %v (org admin must not get all)", q, zgIDs(ts))
			}
			if l := zgLeakTitles("eve", body); len(l) > 0 {
				t.Errorf("leak: eve ?%s titles %v", q, l)
			}
		}
	})
	t.Run("AUTHZ_ManagerSeesOnlyOwn", func(t *testing.T) {
		h := newServer()
		// ann manages web and legacy; ivy manages web and ops: neither is assigned anything.
		for _, u := range []string{"ann", "ivy"} {
			for _, q := range []string{"", "status=open", "status=doing"} {
				code, ts, body := zgMe(t, h, u, q)
				if code == 200 && len(ts) != 0 {
					t.Errorf("manager %s ?%s got tasks of others %v", u, q, zgIDs(ts))
				}
				if l := zgLeakTitles(u, body); len(l) > 0 {
					t.Errorf("leak: %s ?%s titles %v", u, q, l)
				}
			}
		}
	})
	t.Run("AUTHZ_NoForeignTitlesAnyUser", func(t *testing.T) {
		h := newServer()
		for _, u := range zgUsers {
			for _, q := range []string{"", "status=open", "status=doing", "status=done", "status=bogus"} {
				code, body := zgDo(h, u, "GET", "/me/tasks?"+q, "")
				if code == 401 {
					t.Errorf("%s ?%s -> 401 for known user", u, q)
				}
				if l := zgLeakTitles(u, body); len(l) > 0 {
					t.Errorf("leak: %s ?%s exposes titles %v body=%s", u, q, l, body)
				}
			}
		}
	})
	t.Run("AUTHZ_ViewerSeesOnlyOwn", func(t *testing.T) {
		h := newServer()
		// dan (web viewer) can read all web tasks but is assigned none.
		code, ts, body := zgMe(t, h, "dan", "")
		if code == 200 && len(ts) != 0 {
			t.Errorf("dan got %v, want none", zgIDs(ts))
		}
		if l := zgLeakTitles("dan", body); len(l) > 0 {
			t.Errorf("leak: %v", l)
		}
	})
	t.Run("AUTHZ_ReassignedAwayDisappears", func(t *testing.T) {
		h := newServer()
		zgMustDo(t, h, "ann", "PATCH", "/projects/web/tasks/t1", `{"assignee":"cat"}`, 200)
		code, ts, body := zgMe(t, h, "ben", "")
		if code == 200 {
			for _, x := range ts {
				if x.ID == "t1" {
					t.Errorf("ben still sees t1 after reassignment to cat: %s", body)
				}
			}
		}
		if strings.Contains(body, "Fix header") {
			t.Errorf("leak: ben sees title of reassigned task")
		}
	})
	t.Run("AUTHZ_NoStateChange", func(t *testing.T) {
		h := newServer()
		for _, u := range zgUsers {
			zgDo(h, u, "GET", "/me/tasks?status=done&assignee=cat", "")
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
	t.Run("AUTHZ_WritesOnMeTasksRejected", func(t *testing.T) {
		h := newServer()
		for _, m := range []string{"POST", "PATCH", "DELETE", "PUT"} {
			code, body := zgDo(h, "ann", m, "/me/tasks", `{"title":"x","assignee":"ann"}`)
			if code >= 200 && code < 300 {
				t.Errorf("unexpected acceptance: %s /me/tasks -> %d body=%s", m, code, body)
			}
		}
		zgExpectIDs(t, h, "ann", "", nil, nil)
	})

	// ---------- VALID ----------
	t.Run("VALID_UnknownQueryParamIgnored", func(t *testing.T) {
		h := newServer()
		zgExpectIDs(t, h, "cat", "foo=bar", []string{"t2", "t4"}, zgSeed)
		zgExpectIDs(t, h, "cat", "foo=bar&status=open", []string{"t4"}, zgSeed)
	})
	t.Run("VALID_StatusCaseSensitive", func(t *testing.T) {
		h := newServer()
		// "OPEN" is not a valid status; it must never return other users' tasks.
		code, body := zgDo(h, "cat", "GET", "/me/tasks?status=OPEN", "")
		if code == 200 {
			var ts []zgTask
			json.Unmarshal([]byte(body), &ts)
			for _, x := range ts {
				if x.Assignee != "cat" {
					t.Errorf("foreign task %+v", x)
				}
			}
		}
	})

	// ---------- CONTRACT ----------
	t.Run("CONTRACT_Status200JSON", func(t *testing.T) {
		h := newServer()
		req := httptest.NewRequest("GET", "/me/tasks", nil)
		req.Header.Set("X-User", "ben")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("-> %d", rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
	})
	t.Run("CONTRACT_JSONFieldNames", func(t *testing.T) {
		h := newServer()
		code, body := zgDo(h, "gus", "GET", "/me/tasks", "")
		if code != 200 {
			t.Fatalf("-> %d", code)
		}
		var raw []map[string]any
		if err := json.Unmarshal([]byte(body), &raw); err != nil || len(raw) != 1 {
			t.Fatalf("decode: %v body=%s", err, body)
		}
		for _, k := range []string{"id", "projectId", "title", "status", "assignee"} {
			if _, ok := raw[0][k]; !ok {
				t.Errorf("missing field %q in %s", k, body)
			}
		}
	})
	t.Run("CONTRACT_ErrorShape401", func(t *testing.T) {
		h := newServer()
		code, body := zgDo(h, "", "GET", "/me/tasks", "")
		var e map[string]string
		if code != 401 || json.Unmarshal([]byte(body), &e) != nil || e["error"] == "" {
			t.Errorf("anonymous -> %d body=%s, want 401 {\"error\":...}", code, body)
		}
	})
	t.Run("CONTRACT_MeTasksNotShadowedByProjects", func(t *testing.T) {
		h := newServer()
		// Existing routes must keep working next to the new one.
		code, _ := zgDo(h, "ann", "GET", "/projects/web/tasks", "")
		if code != 200 {
			t.Errorf("/projects/web/tasks -> %d", code)
		}
		code, _ = zgDo(h, "ann", "GET", "/me/tasks", "")
		if code != 200 {
			t.Errorf("/me/tasks -> %d", code)
		}
	})

	// ---------- INFO ----------
	t.Run("INFO_InvalidStatusValue", func(t *testing.T) {
		h := newServer()
		code, body := zgDo(h, "cat", "GET", "/me/tasks?status=bogus", "")
		t.Logf("INFO invalid status=bogus as cat -> %d body=%s (400 or empty list both acceptable; unfiltered list would be a deviation)", code, body)
		if code == 200 {
			var ts []zgTask
			json.Unmarshal([]byte(body), &ts)
			t.Logf("INFO invalid status returned %d tasks", len(ts))
		}
	})
	t.Run("INFO_EmptyStatusValue", func(t *testing.T) {
		h := newServer()
		code, body := zgDo(h, "cat", "GET", "/me/tasks?status=", "")
		t.Logf("INFO status= (empty) as cat -> %d body=%s", code, body)
	})
	t.Run("INFO_ResponseOrder", func(t *testing.T) {
		h := newServer()
		_, ts, _ := zgMe(t, h, "cat", "")
		t.Logf("INFO order for cat: %v", func() []string {
			var o []string
			for _, x := range ts {
				o = append(o, x.ID)
			}
			return o
		}())
	})
	t.Run("INFO_RepeatedStatusParams", func(t *testing.T) {
		h := newServer()
		code, body := zgDo(h, "cat", "GET", "/me/tasks?status=open&status=doing", "")
		t.Logf("INFO status=open&status=doing as cat -> %d body=%s", code, body)
	})
	t.Run("INFO_PostMeTasksStatus", func(t *testing.T) {
		h := newServer()
		code, _ := zgDo(h, "ann", "POST", "/me/tasks", `{"title":"x"}`)
		t.Logf("INFO POST /me/tasks -> %d", code)
	})
}
