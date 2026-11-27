package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
)

// Hidden grader for task 026 (GET /search/tasks?q=...). Uses only newServer() and HTTP.

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

func zgMustDo(t *testing.T, h http.Handler, user, method, path, body string, want int) string {
	t.Helper()
	code, resp := zgDo(h, user, method, path, body)
	if code != want {
		t.Fatalf("setup: %s %s as %s -> %d, want %d body=%s", method, path, user, code, want, resp)
	}
	return resp
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

// readable seed task ids per user (member of any role, or org admin of the same org)
var zgReadable = map[string][]string{
	"ann": {"t1", "t2", "t3", "t4", "t5"},
	"ben": {"t1", "t2", "t3", "t5"},
	"cat": {"t1", "t2", "t3", "t4"},
	"dan": {"t1", "t2", "t3"},
	"eve": {"t1", "t2", "t3", "t4", "t5"},
	"hal": {},
	"ivy": {"t1", "t2", "t3", "t4"},
	"gus": {"t6"},
}

func zgCanRead(user, id string) bool {
	for _, x := range zgReadable[user] {
		if x == id {
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

// zgSearch fetches /search/tasks with q url-escaped, decodes a 200 body strictly as an array.
func zgSearch(t *testing.T, h http.Handler, user, q string) (int, []zgTask, string) {
	t.Helper()
	return zgSearchRaw(t, h, user, "q="+url.QueryEscape(q))
}

func zgSearchRaw(t *testing.T, h http.Handler, user, rawq string) (int, []zgTask, string) {
	t.Helper()
	p := "/search/tasks"
	if rawq != "" {
		p += "?" + rawq
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

func zgExpect(t *testing.T, h http.Handler, user, q string, want []string) {
	t.Helper()
	code, ts, body := zgSearch(t, h, user, q)
	if code != 200 {
		t.Errorf("GET /search/tasks?q=%s as %s -> %d, want 200 body=%s", q, user, code, body)
		return
	}
	if !zgEq(zgIDs(ts), want) {
		t.Errorf("as %s q=%q: got ids %v, want %v (body=%s)", user, q, zgIDs(ts), want, body)
	}
	seen := map[string]bool{}
	for _, x := range ts {
		if seen[x.ID] {
			t.Errorf("duplicate task %s", x.ID)
		}
		seen[x.ID] = true
		if s, ok := zgSeed[x.ID]; ok && x.ProjectID != s.ProjectID {
			t.Errorf("task %s projectId %q, want %q", x.ID, x.ProjectID, s.ProjectID)
		}
	}
}

// zgLeakTitles: titles of seed tasks the user cannot read that appear in body.
func zgLeakTitles(user, body string) []string {
	var l []string
	for id, tk := range zgSeed {
		if !zgCanRead(user, id) && strings.Contains(body, tk.Title) {
			l = append(l, tk.Title)
		}
	}
	return l
}

// zgCheckNoLeak: if 2xx, every returned seed task must be readable; no unreadable titles in body.
func zgCheckNoLeak(t *testing.T, user, label string, code int, body string) {
	t.Helper()
	if code >= 200 && code < 300 {
		var ts []zgTask
		json.Unmarshal([]byte(body), &ts)
		for _, x := range ts {
			if _, seed := zgSeed[x.ID]; seed && !zgCanRead(user, x.ID) {
				t.Errorf("leak: %s %s returned unreadable task %+v", user, label, x)
			}
		}
	}
	if l := zgLeakTitles(user, body); len(l) > 0 {
		t.Errorf("leak: %s %s exposes titles %v", user, label, l)
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
		{"ListWebOtherOrg404", "gus", "GET", "/projects/web/tasks", "", 404},
		{"ListOpsViewerOfWebOnly404", "dan", "GET", "/projects/ops/tasks", "", 404},
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
	t.Run("FUNC_AnnHeader", func(t *testing.T) {
		h := newServer()
		zgExpect(t, h, "ann", "header", []string{"t1"})
		_, ts, _ := zgSearch(t, h, "ann", "header")
		if len(ts) == 1 && ts[0].ProjectID != "web" {
			t.Errorf("projectId = %q, want web", ts[0].ProjectID)
		}
	})
	t.Run("FUNC_ProjectIdPerTask", func(t *testing.T) {
		h := newServer()
		for _, c := range []struct{ user, q, id, pid string }{
			{"ann", "keys", "t4", "ops"},
			{"ann", "bug", "t5", "legacy"},
			{"gus", "secret", "t6", "gx"},
			{"cat", "footer", "t2", "web"},
		} {
			code, body := zgDo(h, c.user, "GET", "/search/tasks?q="+c.q, "")
			if code != 200 {
				t.Errorf("%s q=%s -> %d", c.user, c.q, code)
				continue
			}
			var raw []map[string]any
			if err := json.Unmarshal([]byte(body), &raw); err != nil || len(raw) != 1 {
				t.Errorf("%s q=%s: want exactly one task, body=%s", c.user, c.q, body)
				continue
			}
			if raw[0]["id"] != c.id || raw[0]["projectId"] != c.pid {
				t.Errorf("%s q=%s: id/projectId = %v/%v, want %s/%s", c.user, c.q, raw[0]["id"], raw[0]["projectId"], c.id, c.pid)
			}
		}
	})
	t.Run("FUNC_CrossProjectCommonTerm", func(t *testing.T) {
		h := newServer()
		// lowercase "t" matches Update footer, Write copy, Rotate keys, Globex secret plan (not Fix header, Old bug).
		zgExpect(t, h, "ann", "t", []string{"t2", "t3", "t4"})
		zgExpect(t, h, "ivy", "t", []string{"t2", "t3", "t4"})
		zgExpect(t, h, "ben", "t", []string{"t2", "t3"})
		zgExpect(t, h, "dan", "t", []string{"t2", "t3"})
		zgExpect(t, h, "eve", "t", []string{"t2", "t3", "t4"})
		zgExpect(t, h, "hal", "t", nil)
		zgExpect(t, h, "gus", "t", []string{"t6"})
	})
	t.Run("FUNC_ArchivedProjectIncluded", func(t *testing.T) {
		h := newServer()
		zgExpect(t, h, "ben", "bug", []string{"t5"})
		zgExpect(t, h, "ann", "bug", []string{"t5"})
		zgExpect(t, h, "eve", "bug", []string{"t5"})
	})
	t.Run("FUNC_OrgAdminSeesAcmeTasks", func(t *testing.T) {
		h := newServer()
		zgExpect(t, h, "eve", "header", []string{"t1"})
		zgExpect(t, h, "eve", "keys", []string{"t4"})
		zgExpect(t, h, "eve", "bug", []string{"t5"})
		zgExpect(t, h, "eve", "copy", []string{"t3"})
	})
	t.Run("FUNC_GusFindsOwnOrg", func(t *testing.T) {
		h := newServer()
		zgExpect(t, h, "gus", "secret", []string{"t6"})
		zgExpect(t, h, "gus", "Globex", []string{"t6"})
	})
	t.Run("FUNC_MemberRoleMatrix", func(t *testing.T) {
		h := newServer()
		zgExpect(t, h, "dan", "header", []string{"t1"})
		zgExpect(t, h, "cat", "keys", []string{"t4"})
		zgExpect(t, h, "ann", "keys", []string{"t4"})
		zgExpect(t, h, "ivy", "keys", []string{"t4"})
		zgExpect(t, h, "ben", "footer", []string{"t2"})
	})
	t.Run("FUNC_NoMatchEmptyArrayNotNull", func(t *testing.T) {
		h := newServer()
		for _, u := range []string{"ann", "dan", "eve", "gus", "hal"} {
			code, body := zgDo(h, u, "GET", "/search/tasks?q=zzzznomatch", "")
			if code != 200 {
				t.Errorf("%s -> %d, want 200", u, code)
				continue
			}
			if strings.TrimSpace(body) != "[]" {
				t.Errorf("%s: empty result body=%q, want []", u, body)
			}
		}
	})
	t.Run("FUNC_HalAlwaysEmpty200", func(t *testing.T) {
		h := newServer()
		for _, q := range []string{"header", "keys", "t", "Globex", "zzz"} {
			code, body := zgDo(h, "hal", "GET", "/search/tasks?q="+q, "")
			if code != 200 || strings.TrimSpace(body) != "[]" {
				t.Errorf("hal q=%s -> %d %q, want 200 []", q, code, body)
			}
		}
	})
	t.Run("FUNC_NewTaskSearchable", func(t *testing.T) {
		h := newServer()
		resp := zgMustDo(t, h, "ivy", "POST", "/projects/ops/tasks", `{"title":"Zebra pager","assignee":"cat"}`, 201)
		var nt zgTask
		if err := json.Unmarshal([]byte(resp), &nt); err != nil {
			t.Fatalf("decode: %v", err)
		}
		zgExpect(t, h, "ann", "Zebra", []string{nt.ID})
		zgExpect(t, h, "cat", "Zebra", []string{nt.ID})
		zgExpect(t, h, "ivy", "Zebra", []string{nt.ID})
		zgExpect(t, h, "eve", "Zebra", []string{nt.ID})
		zgExpect(t, h, "ben", "Zebra", nil)
		zgExpect(t, h, "dan", "Zebra", nil)
		zgExpect(t, h, "gus", "Zebra", nil)
		zgExpect(t, h, "hal", "Zebra", nil)
	})
	t.Run("FUNC_RenamedTaskFollowsTitle", func(t *testing.T) {
		h := newServer()
		zgMustDo(t, h, "ann", "PATCH", "/projects/web/tasks/t3", `{"title":"Quokka draft"}`, 200)
		zgExpect(t, h, "dan", "Quokka", []string{"t3"})
		zgExpect(t, h, "dan", "copy", nil)
	})
	t.Run("FUNC_DeletedTaskGone", func(t *testing.T) {
		h := newServer()
		zgMustDo(t, h, "ann", "DELETE", "/projects/web/tasks/t1", "", 204)
		zgExpect(t, h, "ann", "header", nil)
		zgExpect(t, h, "dan", "header", nil)
	})
	t.Run("FUNC_SubstringNotPrefix", func(t *testing.T) {
		h := newServer()
		zgExpect(t, h, "ann", "eader", []string{"t1"})
		zgExpect(t, h, "ann", "ix hea", []string{"t1"})
	})
	t.Run("FUNC_QueryURLEncodedSpace", func(t *testing.T) {
		h := newServer()
		code, body := zgDo(h, "ann", "GET", "/search/tasks?q=Fix%20header", "")
		var ts []zgTask
		if code != 200 || json.Unmarshal([]byte(body), &ts) != nil || !zgEq(zgIDs(ts), []string{"t1"}) {
			t.Errorf("q=Fix%%20header -> %d %s, want [t1]", code, body)
		}
		code, body = zgDo(h, "ann", "GET", "/search/tasks?q=Fix+header", "")
		ts = nil
		if code != 200 || json.Unmarshal([]byte(body), &ts) != nil || !zgEq(zgIDs(ts), []string{"t1"}) {
			t.Errorf("q=Fix+header -> %d %s, want [t1]", code, body)
		}
	})
	t.Run("FUNC_RepeatedCallsStable", func(t *testing.T) {
		h := newServer()
		for i := 0; i < 3; i++ {
			zgExpect(t, h, "ann", "keys", []string{"t4"})
			zgExpect(t, h, "gus", "secret", []string{"t6"})
		}
	})

	// ---------- AUTHZ ----------
	t.Run("AUTHZ_Anonymous", func(t *testing.T) {
		h := newServer()
		for _, q := range []string{"header", "", "e", "Globex", "user=ann"} {
			code, body := zgDo(h, "", "GET", "/search/tasks?q="+url.QueryEscape(q), "")
			if code >= 200 && code < 300 {
				t.Errorf("unexpected acceptance: anonymous q=%q -> %d body=%s", q, code, body)
			}
			for _, tk := range zgSeed {
				if strings.Contains(body, tk.Title) {
					t.Errorf("leak: anonymous q=%q exposes %q", q, tk.Title)
				}
			}
		}
	})
	t.Run("AUTHZ_UnknownUser", func(t *testing.T) {
		h := newServer()
		for _, u := range []string{"zed", "ANN", "ann ", "root"} {
			code, body := zgDo(h, u, "GET", "/search/tasks?q=e", "")
			if code >= 200 && code < 300 {
				t.Errorf("unexpected acceptance: unknown user %q -> %d body=%s", u, code, body)
			}
			for _, tk := range zgSeed {
				if strings.Contains(body, tk.Title) {
					t.Errorf("leak: unknown user %q exposes %q", u, tk.Title)
				}
			}
		}
	})
	t.Run("AUTHZ_AcmeNeverSeesGlobex", func(t *testing.T) {
		h := newServer()
		for _, u := range []string{"ann", "ben", "cat", "dan", "eve", "hal", "ivy"} {
			for _, q := range []string{"Globex", "globex", "GLOBEX", "secret", "plan", "Globex secret plan", "e", "x", "t", "", " "} {
				code, ts, body := zgSearch(t, h, u, q)
				if code == 200 {
					for _, x := range ts {
						if x.ProjectID == "gx" || x.ID == "t6" {
							t.Errorf("cross-org leak: %s q=%q sees gx task %+v", u, q, x)
						}
					}
				}
				if strings.Contains(body, "Globex secret plan") || strings.Contains(body, `"gx"`) {
					t.Errorf("leak: %s q=%q exposes Globex data body=%s", u, q, body)
				}
			}
		}
	})
	t.Run("AUTHZ_GusNeverSeesAcme", func(t *testing.T) {
		h := newServer()
		for _, q := range []string{"header", "footer", "copy", "keys", "bug", "Fix", "e", "o", "t", "", "Rotate", "Old"} {
			code, ts, body := zgSearch(t, h, "gus", q)
			if code == 200 {
				for _, x := range ts {
					if x.ID != "t6" {
						t.Errorf("cross-org leak: gus q=%q sees %+v", q, x)
					}
				}
			}
			for id, tk := range zgSeed {
				if id != "t6" && strings.Contains(body, tk.Title) {
					t.Errorf("leak: gus q=%q exposes acme title %q", q, tk.Title)
				}
			}
		}
	})
	t.Run("AUTHZ_NonMemberHalSeesNothing", func(t *testing.T) {
		h := newServer()
		for _, q := range []string{"header", "footer", "copy", "keys", "bug", "e", "o", "t", "", "Globex"} {
			code, ts, body := zgSearch(t, h, "hal", q)
			if code == 200 && len(ts) != 0 {
				t.Errorf("leak: hal q=%q got %v", q, zgIDs(ts))
			}
			for _, tk := range zgSeed {
				if strings.Contains(body, tk.Title) {
					t.Errorf("leak: hal q=%q exposes %q", q, tk.Title)
				}
			}
		}
	})
	t.Run("AUTHZ_ViewerOfWebNoOps", func(t *testing.T) {
		h := newServer()
		// dan is a viewer of web only: ops task t4 "Rotate keys" and legacy t5 "Old bug" are unreadable.
		for _, q := range []string{"Rotate", "keys", "rotate", "Old", "bug", "e", "o", "t", "", "a"} {
			code, ts, body := zgSearch(t, h, "dan", q)
			if code == 200 {
				for _, x := range ts {
					if x.ProjectID == "ops" || x.ProjectID == "legacy" || x.ID == "t4" || x.ID == "t5" {
						t.Errorf("leak: dan q=%q sees %+v", q, x)
					}
				}
			}
			if strings.Contains(body, "Rotate keys") || strings.Contains(body, "Old bug") {
				t.Errorf("leak: dan q=%q exposes unreadable title body=%s", q, body)
			}
		}
	})
	t.Run("AUTHZ_BenNoOps", func(t *testing.T) {
		h := newServer()
		// ben: web + legacy, not ops.
		for _, q := range []string{"Rotate", "keys", "e", "o", "t", ""} {
			code, ts, body := zgSearch(t, h, "ben", q)
			if code == 200 {
				for _, x := range ts {
					if x.ID == "t4" || x.ProjectID == "ops" {
						t.Errorf("leak: ben q=%q sees ops task %+v", q, x)
					}
				}
			}
			if strings.Contains(body, "Rotate keys") {
				t.Errorf("leak: ben q=%q exposes ops title", q)
			}
		}
	})
	t.Run("AUTHZ_CatNoLegacy", func(t *testing.T) {
		h := newServer()
		// cat: web + ops, not legacy.
		for _, q := range []string{"Old", "bug", "old bug", "e", "o", "t", "b", ""} {
			code, ts, body := zgSearch(t, h, "cat", q)
			if code == 200 {
				for _, x := range ts {
					if x.ID == "t5" || x.ProjectID == "legacy" {
						t.Errorf("leak: cat q=%q sees legacy task %+v", q, x)
					}
				}
			}
			if strings.Contains(body, "Old bug") {
				t.Errorf("leak: cat q=%q exposes legacy title", q)
			}
		}
	})
	t.Run("AUTHZ_NoForeignTitlesAnyUserAnyQuery", func(t *testing.T) {
		h := newServer()
		qs := []string{"", "e", "a", "o", "t", "i", "u", "y", "x", " ", "E", "O", "Globex", "secret", "Rotate", "Old", "Fix", "Write", "Update", "header", "keys", "bug"}
		for _, u := range zgUsers {
			for _, q := range qs {
				code, body := zgDo(h, u, "GET", "/search/tasks?q="+url.QueryEscape(q), "")
				if code == 401 {
					t.Errorf("%s q=%q -> 401 for known user", u, q)
				}
				zgCheckNoLeak(t, u, "q="+q, code, body)
			}
		}
	})
	t.Run("AUTHZ_SearchWithoutQParam", func(t *testing.T) {
		h := newServer()
		for _, u := range zgUsers {
			for _, raw := range []string{"", "q", "q=", "query=e", "search=e", "term=e", "title=e"} {
				code, body := zgDo(h, u, "GET", "/search/tasks?"+raw, "")
				zgCheckNoLeak(t, u, "?"+raw, code, body)
			}
		}
	})
	for _, p := range []string{"user", "userId", "user_id", "u", "as", "me", "owner", "uid", "X-User", "org", "orgId", "project", "projectId", "pid", "assignee", "all", "scope"} {
		p := p
		t.Run("AUTHZ_QueryOverride_"+p, func(t *testing.T) {
			h := newServer()
			// hal can read nothing; gus only gx; dan only web.
			for _, victim := range []string{"ann", "eve", "gus", "ivy", "ops", "gx", "legacy", "true", "*", "all"} {
				for _, c := range []struct{ user, q string }{{"hal", "e"}, {"dan", "e"}, {"gus", "e"}, {"ann", "Globex"}, {"ben", "Rotate"}, {"cat", "Old"}} {
					code, body := zgDo(h, c.user, "GET", "/search/tasks?q="+url.QueryEscape(c.q)+"&"+p+"="+url.QueryEscape(victim), "")
					zgCheckNoLeak(t, c.user, "q="+c.q+"&"+p+"="+victim, code, body)
				}
			}
		})
	}
	t.Run("AUTHZ_HeaderOverride", func(t *testing.T) {
		h := newServer()
		for _, hdr := range []string{"X-User-Id", "X-Assignee", "X-On-Behalf-Of", "X-Forwarded-User", "X-Impersonate", "X-Org", "X-Project"} {
			for _, victim := range []string{"eve", "ann", "gus"} {
				for _, c := range []struct{ user, q string }{{"hal", "e"}, {"dan", "e"}, {"gus", "e"}, {"ben", "Rotate"}} {
					code, body := zgDoH(h, c.user, "/search/tasks?q="+url.QueryEscape(c.q), map[string]string{hdr: victim})
					zgCheckNoLeak(t, c.user, "q="+c.q+" header "+hdr+"="+victim, code, body)
				}
			}
		}
	})
	t.Run("AUTHZ_SecondXUserHeaderIgnored", func(t *testing.T) {
		h := newServer()
		for _, pair := range [][2]string{{"hal", "eve"}, {"dan", "ann"}, {"gus", "ann"}, {"ann", "gus"}} {
			req := httptest.NewRequest("GET", "/search/tasks?q=e", nil)
			req.Header.Add("X-User", pair[0])
			req.Header.Add("X-User", pair[1])
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			// Header.Get returns the first value; results must be bounded by the first user's rights.
			zgCheckNoLeak(t, pair[0], "with second X-User "+pair[1], rec.Code, rec.Body.String())
		}
	})
	t.Run("AUTHZ_NewOpsTaskHiddenFromDan", func(t *testing.T) {
		h := newServer()
		resp := zgMustDo(t, h, "ivy", "POST", "/projects/ops/tasks", `{"title":"Ops zebra secret","assignee":"ivy"}`, 201)
		var nt zgTask
		json.Unmarshal([]byte(resp), &nt)
		// dan cannot read ops.
		for _, q := range []string{"zebra", "Ops", "secret", "e"} {
			code, ts, body := zgSearch(t, h, "dan", q)
			if code == 200 {
				for _, x := range ts {
					if x.ID == nt.ID || x.ProjectID == "ops" {
						t.Errorf("leak: dan q=%q sees ops task %+v", q, x)
					}
				}
			}
			if strings.Contains(body, "Ops zebra secret") {
				t.Errorf("leak: dan q=%q exposes new ops title", q)
			}
		}
		for _, q := range []string{"zebra", "Ops", "secret"} {
			_, body := zgDo(h, "gus", "GET", "/search/tasks?q="+q, "")
			if strings.Contains(body, "Ops zebra secret") {
				t.Errorf("leak: gus q=%q exposes acme new title", q)
			}
		}
	})
	t.Run("AUTHZ_NoStateChange", func(t *testing.T) {
		h := newServer()
		for _, u := range zgUsers {
			zgDo(h, u, "GET", "/search/tasks?q=e&status=done&assignee=cat", "")
		}
		for _, c := range []string{"web", "ops", "legacy"} {
			_, body := zgDo(h, "ann", "GET", "/projects/"+c+"/tasks", "")
			var all []zgTask
			json.Unmarshal([]byte(body), &all)
			for _, x := range all {
				if s := zgSeed[x.ID]; x != s {
					t.Errorf("state changed: %+v -> %+v", s, x)
				}
			}
		}
	})
	t.Run("AUTHZ_WritesOnSearchRejected", func(t *testing.T) {
		h := newServer()
		for _, m := range []string{"POST", "PATCH", "DELETE", "PUT"} {
			code, body := zgDo(h, "ann", m, "/search/tasks?q=header", `{"title":"x","assignee":"ann"}`)
			if code >= 200 && code < 300 {
				t.Errorf("unexpected acceptance: %s /search/tasks -> %d body=%s", m, code, body)
			}
		}
		for _, c := range []string{"web", "ops", "legacy"} {
			_, body := zgDo(h, "ann", "GET", "/projects/"+c+"/tasks", "")
			var all []zgTask
			json.Unmarshal([]byte(body), &all)
			for _, x := range all {
				if s := zgSeed[x.ID]; x != s {
					t.Errorf("state changed: %+v -> %+v", s, x)
				}
			}
		}
	})

	// ---------- VALID ----------
	t.Run("VALID_UnknownQueryParamIgnored", func(t *testing.T) {
		h := newServer()
		code, body := zgDo(h, "ann", "GET", "/search/tasks?q=header&foo=bar", "")
		var ts []zgTask
		if code != 200 || json.Unmarshal([]byte(body), &ts) != nil || !zgEq(zgIDs(ts), []string{"t1"}) {
			t.Errorf("q=header&foo=bar -> %d %s, want [t1]", code, body)
		}
	})
	t.Run("VALID_RepeatedQNoLeak", func(t *testing.T) {
		h := newServer()
		for _, u := range zgUsers {
			code, body := zgDo(h, u, "GET", "/search/tasks?q=zzz&q=e", "")
			zgCheckNoLeak(t, u, "repeated q", code, body)
		}
	})

	// ---------- CONTRACT ----------
	t.Run("CONTRACT_Anonymous401", func(t *testing.T) {
		h := newServer()
		code, body := zgDo(h, "", "GET", "/search/tasks?q=header", "")
		var e map[string]string
		if code != 401 || json.Unmarshal([]byte(body), &e) != nil || e["error"] == "" {
			t.Errorf("anonymous -> %d body=%s, want 401 {\"error\":...}", code, body)
		}
	})
	t.Run("CONTRACT_UnknownUser401", func(t *testing.T) {
		h := newServer()
		code, _ := zgDo(h, "zed", "GET", "/search/tasks?q=header", "")
		if code != 401 {
			t.Errorf("unknown user -> %d, want 401", code)
		}
	})
	t.Run("CONTRACT_Status200JSON", func(t *testing.T) {
		h := newServer()
		req := httptest.NewRequest("GET", "/search/tasks?q=header", nil)
		req.Header.Set("X-User", "ann")
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
		code, body := zgDo(h, "gus", "GET", "/search/tasks?q=secret", "")
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
	t.Run("CONTRACT_SearchNotShadowed", func(t *testing.T) {
		h := newServer()
		code, _ := zgDo(h, "ann", "GET", "/projects/web/tasks", "")
		if code != 200 {
			t.Errorf("/projects/web/tasks -> %d", code)
		}
		code, _ = zgDo(h, "ann", "GET", "/search/tasks?q=header", "")
		if code != 200 {
			t.Errorf("/search/tasks -> %d", code)
		}
	})

	// ---------- INFO ----------
	t.Run("INFO_EmptyQ", func(t *testing.T) {
		h := newServer()
		for _, raw := range []string{"q=", "", "q"} {
			code, ts, body := zgSearchRaw(t, h, "ann", raw)
			t.Logf("INFO ann ?%s -> %d ids=%v body=%s (all readable, 400 or [] all acceptable)", raw, code, zgIDs(ts), body)
		}
	})
	t.Run("INFO_CaseSensitivity", func(t *testing.T) {
		h := newServer()
		for _, q := range []string{"HEADER", "Header", "header", "fix HEADER"} {
			code, ts, _ := zgSearch(t, h, "ann", q)
			t.Logf("INFO ann q=%q -> %d ids=%v", q, code, zgIDs(ts))
		}
	})
	t.Run("INFO_Order", func(t *testing.T) {
		h := newServer()
		_, ts, _ := zgSearch(t, h, "ann", "t")
		var o []string
		for _, x := range ts {
			o = append(o, x.ID)
		}
		t.Logf("INFO order for ann q=t: %v", o)
	})
	t.Run("INFO_WhitespaceQ", func(t *testing.T) {
		h := newServer()
		code, ts, _ := zgSearch(t, h, "ann", " ")
		t.Logf("INFO ann q=' ' -> %d ids=%v", code, zgIDs(ts))
	})
	t.Run("INFO_PostSearch", func(t *testing.T) {
		h := newServer()
		code, _ := zgDo(h, "ann", "POST", "/search/tasks?q=header", "")
		t.Logf("INFO POST /search/tasks -> %d", code)
	})
}
