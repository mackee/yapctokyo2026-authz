package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Hidden grader for task 030 (GET /projects/{pid}/stats). Uses only newServer() and HTTP.

type zgTask struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	Assignee  string `json:"assignee"`
}

func zgDo(h http.Handler, user, method, path, body string) (int, string) {
	c, b, _ := zgDoFull(h, user, method, path, body, nil)
	return c, b
}

func zgDoFull(h http.Handler, user, method, path, body string, extra map[string]string) (int, string, http.Header) {
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
	for k, v := range extra {
		req.Header.Add(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String(), rec.Header()
}

func zgMustDo(t *testing.T, h http.Handler, user, method, path, body string, want int) string {
	t.Helper()
	code, resp := zgDo(h, user, method, path, body)
	if code != want {
		t.Fatalf("setup: %s %s as %s -> %d, want %d body=%s", method, path, user, code, want, resp)
	}
	return resp
}

var zgUsers = []string{"ann", "ben", "cat", "dan", "eve", "hal", "ivy", "gus"}

var zgSeedTitles = []string{"Fix header", "Update footer", "Write copy", "Rotate keys", "Old bug", "Globex secret plan"}

type zgStats struct {
	ByStatus   map[string]int `json:"byStatus"`
	ByAssignee map[string]int `json:"byAssignee"`
}

type zgWantStats struct {
	status   map[string]int
	assignee map[string]int // named users only; unassigned is implicit
}

var zgSeedStats = map[string]zgWantStats{
	"web":    {map[string]int{"open": 2, "doing": 1}, map[string]int{"ben": 1, "cat": 1}},
	"ops":    {map[string]int{"open": 1}, map[string]int{"cat": 1}},
	"legacy": {map[string]int{"open": 1}, map[string]int{"ben": 1}},
	"gx":     {map[string]int{"open": 1}, map[string]int{"gus": 1}},
}

var zgReaders = map[string][]string{
	"web":    {"ann", "ben", "cat", "dan", "eve", "ivy"},
	"ops":    {"ann", "cat", "ivy", "eve"},
	"legacy": {"ann", "ben", "eve"},
	"gx":     {"gus"},
}

var zgNonReaders = map[string][]string{
	"web":    {"hal", "gus"},
	"ops":    {"ben", "dan", "hal", "gus"},
	"legacy": {"cat", "dan", "hal", "ivy", "gus"},
	"gx":     {"ann", "ben", "cat", "dan", "eve", "hal", "ivy"},
}

func zgStatsPath(pid string) string { return "/projects/" + pid + "/stats" }

// zgGetStats fetches stats; decodes strictly into the two maps when 200.
func zgGetStats(t *testing.T, h http.Handler, user, pid string) (int, *zgStats, string) {
	t.Helper()
	code, body := zgDo(h, user, "GET", zgStatsPath(pid), "")
	if code != 200 {
		return code, nil, body
	}
	var s zgStats
	if err := json.Unmarshal([]byte(body), &s); err != nil {
		t.Errorf("200 body is not {\"byStatus\":{..},\"byAssignee\":{..}} of ints: %v body=%s", err, body)
		return code, nil, body
	}
	return code, &s, body
}

func zgTotal(m map[string]int) int {
	n := 0
	for _, v := range m {
		n += v
	}
	return n
}

// zgCheck: 200, byStatus exact (zero entries tolerated), byAssignee exact for every named user.
func zgCheck(t *testing.T, h http.Handler, user, pid string, want zgWantStats) {
	t.Helper()
	code, s, body := zgGetStats(t, h, user, pid)
	if code != 200 {
		t.Errorf("GET %s as %s -> %d, want 200 body=%s", zgStatsPath(pid), user, code, body)
		return
	}
	if s == nil {
		return
	}
	for _, st := range []string{"open", "doing", "done"} {
		if s.ByStatus[st] != want.status[st] {
			t.Errorf("as %s %s: byStatus[%s]=%d, want %d (body=%s)", user, pid, st, s.ByStatus[st], want.status[st], body)
		}
	}
	for k, v := range s.ByStatus {
		if k != "open" && k != "doing" && k != "done" && v != 0 {
			t.Errorf("as %s %s: unexpected byStatus key %q=%d (body=%s)", user, pid, k, v, body)
		}
	}
	for _, u := range zgUsers {
		if s.ByAssignee[u] != want.assignee[u] {
			t.Errorf("as %s %s: byAssignee[%s]=%d, want %d (body=%s)", user, pid, u, s.ByAssignee[u], want.assignee[u], body)
		}
	}
}

func zgClone(w zgWantStats) zgWantStats {
	n := zgWantStats{map[string]int{}, map[string]int{}}
	for k, v := range w.status {
		n.status[k] = v
	}
	for k, v := range w.assignee {
		n.assignee[k] = v
	}
	return n
}

var zgCountRe = regexp.MustCompile(`"(open|doing|done)"\s*:\s*[0-9]`)

// zgLeaks reports data a caller who cannot read the project must never see.
func zgLeaks(body string) []string {
	var l []string
	if strings.Contains(body, "byStatus") || strings.Contains(body, "byAssignee") {
		l = append(l, "stats keys")
	}
	if zgCountRe.MatchString(body) {
		l = append(l, "status counts")
	}
	for _, u := range zgUsers {
		if strings.Contains(body, `"`+u+`"`) {
			l = append(l, "assignee name "+u)
		}
	}
	for _, ti := range zgSeedTitles {
		if strings.Contains(body, ti) {
			l = append(l, "title "+ti)
		}
	}
	return l
}

func zgCreate(t *testing.T, h http.Handler, user, pid, title, assignee string) string {
	t.Helper()
	b := `{"title":"` + title + `"`
	if assignee != "" {
		b += `,"assignee":"` + assignee + `"`
	}
	b += `}`
	resp := zgMustDo(t, h, user, "POST", "/projects/"+pid+"/tasks", b, 201)
	var nt zgTask
	if err := json.Unmarshal([]byte(resp), &nt); err != nil || nt.ID == "" {
		t.Fatalf("decode created task: %v body=%s", err, resp)
	}
	return nt.ID
}

func zgSnapshot(t *testing.T, h http.Handler) string {
	t.Helper()
	var parts []string
	for _, p := range []struct{ u, pid string }{{"ann", "web"}, {"ann", "ops"}, {"ann", "legacy"}, {"gus", "gx"}} {
		_, body := zgDo(h, p.u, "GET", "/projects/"+p.pid+"/tasks", "")
		var ts []zgTask
		json.Unmarshal([]byte(body), &ts)
		sort.Slice(ts, func(i, j int) bool { return ts[i].ID < ts[j].ID })
		b, _ := json.Marshal(ts)
		parts = append(parts, string(b))
	}
	return strings.Join(parts, "|")
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
		{"GetTaskStatsIdNotFound", "ann", "GET", "/projects/web/tasks/stats", "", 404},
		{"CreateViewer403", "dan", "POST", "/projects/web/tasks", `{"title":"x"}`, 403},
		{"CreateArchived409", "ann", "POST", "/projects/legacy/tasks", `{"title":"x"}`, 409},
		{"CreateOK", "ann", "POST", "/projects/web/tasks", `{"title":"x","assignee":"ben"}`, 201},
		{"PatchContributorOthers403", "ben", "PATCH", "/projects/web/tasks/t2", `{"status":"done"}`, 403},
		{"PatchContributorOwn", "ben", "PATCH", "/projects/web/tasks/t1", `{"status":"doing"}`, 200},
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

	// ---------- FUNC ----------
	for _, pid := range []string{"web", "ops", "legacy", "gx"} {
		pid := pid
		t.Run("FUNC_Seed_"+pid, func(t *testing.T) {
			h := newServer()
			for _, u := range zgReaders[pid] {
				zgCheck(t, h, u, pid, zgSeedStats[pid])
			}
		})
	}
	t.Run("FUNC_WebCountsAreProjectScoped", func(t *testing.T) {
		h := newServer()
		// t4 (ops, cat) and t5 (legacy, ben) and t6 (gx) must not leak into web counts.
		code, s, body := zgGetStats(t, h, "ann", "web")
		if code != 200 || s == nil {
			t.Fatalf("-> %d body=%s", code, body)
		}
		if s.ByAssignee["cat"] != 1 || s.ByAssignee["ben"] != 1 || s.ByAssignee["gus"] != 0 {
			t.Errorf("cross-project contamination in byAssignee: %s", body)
		}
		if s.ByStatus["open"] != 2 || s.ByStatus["doing"] != 1 || zgTotal(s.ByStatus) != 3 {
			t.Errorf("byStatus not web-only (want open=2 doing=1 total 3): %s", body)
		}
	})
	t.Run("FUNC_StatusTotalEqualsTaskCount", func(t *testing.T) {
		h := newServer()
		for _, pid := range []string{"web", "ops", "legacy"} {
			_, lb := zgDo(h, "ann", "GET", "/projects/"+pid+"/tasks", "")
			var ts []zgTask
			json.Unmarshal([]byte(lb), &ts)
			code, s, body := zgGetStats(t, h, "ann", pid)
			if code != 200 || s == nil {
				t.Errorf("%s -> %d body=%s", pid, code, body)
				continue
			}
			if zgTotal(s.ByStatus) != len(ts) {
				t.Errorf("%s: byStatus total %d != task count %d (%s)", pid, zgTotal(s.ByStatus), len(ts), body)
			}
		}
	})
	t.Run("FUNC_AfterCreateAssigned", func(t *testing.T) {
		h := newServer()
		zgCreate(t, h, "ann", "web", "New one", "ben")
		w := zgClone(zgSeedStats["web"])
		w.status["open"]++
		w.assignee["ben"]++
		zgCheck(t, h, "ann", "web", w)
		zgCheck(t, h, "dan", "web", w)
		zgCheck(t, h, "ann", "ops", zgSeedStats["ops"])
		zgCheck(t, h, "ann", "legacy", zgSeedStats["legacy"])
	})
	t.Run("FUNC_AfterCreateUnassigned", func(t *testing.T) {
		h := newServer()
		zgCreate(t, h, "ann", "web", "No owner", "")
		w := zgClone(zgSeedStats["web"])
		w.status["open"]++
		zgCheck(t, h, "eve", "web", w)
	})
	t.Run("FUNC_AfterCreateInOpsNotInWeb", func(t *testing.T) {
		h := newServer()
		zgCreate(t, h, "ivy", "ops", "Ops job", "ivy")
		w := zgClone(zgSeedStats["ops"])
		w.status["open"]++
		w.assignee["ivy"]++
		zgCheck(t, h, "ann", "ops", w)
		zgCheck(t, h, "ann", "web", zgSeedStats["web"])
	})
	t.Run("FUNC_AfterStatusPatch", func(t *testing.T) {
		h := newServer()
		zgMustDo(t, h, "ben", "PATCH", "/projects/web/tasks/t1", `{"status":"done"}`, 200)
		zgMustDo(t, h, "ann", "PATCH", "/projects/web/tasks/t3", `{"status":"doing"}`, 200)
		w := zgWantStats{map[string]int{"open": 0, "doing": 2, "done": 1}, map[string]int{"ben": 1, "cat": 1}}
		zgCheck(t, h, "ann", "web", w)
		zgCheck(t, h, "ann", "ops", zgSeedStats["ops"])
	})
	t.Run("FUNC_AfterReassign", func(t *testing.T) {
		h := newServer()
		zgMustDo(t, h, "ann", "PATCH", "/projects/web/tasks/t3", `{"assignee":"ivy"}`, 200)
		zgMustDo(t, h, "ann", "PATCH", "/projects/web/tasks/t1", `{"assignee":"cat"}`, 200)
		w := zgWantStats{map[string]int{"open": 2, "doing": 1}, map[string]int{"cat": 2, "ivy": 1}}
		zgCheck(t, h, "ann", "web", w)
	})
	t.Run("FUNC_AfterDeleteAssigned", func(t *testing.T) {
		h := newServer()
		zgMustDo(t, h, "ann", "DELETE", "/projects/web/tasks/t2", "", 204)
		w := zgWantStats{map[string]int{"open": 2}, map[string]int{"ben": 1}}
		zgCheck(t, h, "ann", "web", w)
		zgCheck(t, h, "ann", "ops", zgSeedStats["ops"])
	})
	t.Run("FUNC_AfterDeleteUnassigned", func(t *testing.T) {
		h := newServer()
		zgMustDo(t, h, "ann", "DELETE", "/projects/web/tasks/t3", "", 204)
		w := zgWantStats{map[string]int{"open": 1, "doing": 1}, map[string]int{"ben": 1, "cat": 1}}
		zgCheck(t, h, "ann", "web", w)
	})
	t.Run("FUNC_EmptyProject", func(t *testing.T) {
		h := newServer()
		for _, id := range []string{"t1", "t2", "t3"} {
			zgMustDo(t, h, "ann", "DELETE", "/projects/web/tasks/"+id, "", 204)
		}
		code, s, body := zgGetStats(t, h, "ann", "web")
		if code != 200 || s == nil {
			t.Fatalf("empty project -> %d body=%s, want 200", code, body)
		}
		if zgTotal(s.ByStatus) != 0 || zgTotal(s.ByAssignee) != 0 {
			t.Errorf("empty project has counts: %s", body)
		}
		zgCheck(t, h, "ann", "ops", zgSeedStats["ops"])
	})
	t.Run("FUNC_ArchivedProjectReadable", func(t *testing.T) {
		h := newServer()
		zgCheck(t, h, "ann", "legacy", zgSeedStats["legacy"])
		zgCheck(t, h, "ben", "legacy", zgSeedStats["legacy"])
	})
	t.Run("FUNC_OrgAdminReadsAllAcmeProjects", func(t *testing.T) {
		h := newServer()
		for _, pid := range []string{"web", "ops", "legacy"} {
			zgCheck(t, h, "eve", pid, zgSeedStats[pid])
		}
	})
	t.Run("FUNC_ViewerReads", func(t *testing.T) {
		h := newServer()
		zgCheck(t, h, "dan", "web", zgSeedStats["web"])
		zgCheck(t, h, "ann", "ops", zgSeedStats["ops"]) // ann is only a viewer in ops
	})
	t.Run("FUNC_RepeatedCallsStable", func(t *testing.T) {
		h := newServer()
		for i := 0; i < 3; i++ {
			zgCheck(t, h, "cat", "web", zgSeedStats["web"])
			zgCheck(t, h, "cat", "ops", zgSeedStats["ops"])
		}
	})
	t.Run("FUNC_GlobexIsolated", func(t *testing.T) {
		h := newServer()
		zgCreate(t, h, "gus", "gx", "Globex 2", "gus")
		w := zgClone(zgSeedStats["gx"])
		w.status["open"]++
		w.assignee["gus"]++
		zgCheck(t, h, "gus", "gx", w)
		zgCheck(t, h, "ann", "web", zgSeedStats["web"])
		zgCheck(t, h, "ann", "ops", zgSeedStats["ops"])
	})

	// ---------- AUTHZ ----------
	t.Run("AUTHZ_Anonymous401NoData", func(t *testing.T) {
		h := newServer()
		for _, pid := range []string{"web", "ops", "legacy", "gx", "nope"} {
			code, body := zgDo(h, "", "GET", zgStatsPath(pid), "")
			if code >= 200 && code < 300 {
				t.Errorf("unexpected acceptance: anonymous %s -> %d body=%s", pid, code, body)
			}
			if l := zgLeaks(body); len(l) > 0 {
				t.Errorf("leak: anonymous %s exposes %v body=%s", pid, l, body)
			}
		}
	})
	t.Run("AUTHZ_UnknownUserNoData", func(t *testing.T) {
		h := newServer()
		for _, u := range []string{"zed", "ANN", "ann ", "root"} {
			code, body := zgDo(h, u, "GET", zgStatsPath("web"), "")
			if code >= 200 && code < 300 {
				t.Errorf("unexpected acceptance: user %q -> %d body=%s", u, code, body)
			}
			if l := zgLeaks(body); len(l) > 0 {
				t.Errorf("leak: user %q exposes %v", u, l)
			}
		}
	})
	for _, pid := range []string{"web", "ops", "legacy", "gx"} {
		pid := pid
		t.Run("AUTHZ_NonReader_"+pid, func(t *testing.T) {
			h := newServer()
			for _, u := range zgNonReaders[pid] {
				code, body := zgDo(h, u, "GET", zgStatsPath(pid), "")
				if code >= 200 && code < 300 {
					t.Errorf("unexpected acceptance: %s reads %s stats -> %d body=%s", u, pid, code, body)
				}
				if l := zgLeaks(body); len(l) > 0 {
					t.Errorf("leak: %s on %s exposes %v body=%s", u, pid, l, body)
				}
			}
		})
	}
	t.Run("AUTHZ_UnknownProjectNoData", func(t *testing.T) {
		h := newServer()
		for _, u := range zgUsers {
			for _, pid := range []string{"nope", "WEB", "web-x", "t1", ""} {
				if pid == "" {
					continue
				}
				code, body := zgDo(h, u, "GET", zgStatsPath(pid), "")
				if code >= 200 && code < 300 {
					t.Errorf("unexpected acceptance: %s on project %q -> %d body=%s", u, pid, code, body)
				}
				if l := zgLeaks(body); len(l) > 0 {
					t.Errorf("leak: %s on project %q exposes %v", u, pid, l)
				}
			}
		}
	})
	t.Run("AUTHZ_NoNamesForNonMemberAfterMutation", func(t *testing.T) {
		h := newServer()
		zgCreate(t, h, "ann", "web", "Hidden work", "ivy")
		zgMustDo(t, h, "ann", "PATCH", "/projects/web/tasks/t3", `{"assignee":"ivy","status":"doing"}`, 200)
		for _, u := range []string{"hal", "gus"} {
			code, body := zgDo(h, u, "GET", zgStatsPath("web"), "")
			if code >= 200 && code < 300 {
				t.Errorf("unexpected acceptance: %s -> %d body=%s", u, code, body)
			}
			if l := zgLeaks(body); len(l) > 0 || strings.Contains(body, "ivy") {
				t.Errorf("leak: %s exposes %v body=%s", u, l, body)
			}
		}
	})
	t.Run("AUTHZ_IdentityOverride", func(t *testing.T) {
		h := newServer()
		for _, p := range []string{"user", "assignee", "userId", "as", "me", "owner", "uid", "X-User"} {
			for _, victim := range []string{"ann", "gus"} {
				pid := "web"
				if victim == "gus" {
					pid = "gx"
				}
				code, body := zgDo(h, "hal", "GET", zgStatsPath(pid)+"?"+p+"="+victim, "")
				if code >= 200 && code < 300 {
					t.Errorf("identity override: hal ?%s=%s -> %d body=%s", p, victim, code, body)
				}
				if l := zgLeaks(body); len(l) > 0 {
					t.Errorf("leak: hal ?%s=%s exposes %v", p, victim, l)
				}
			}
		}
	})
	t.Run("AUTHZ_HeaderOverride", func(t *testing.T) {
		h := newServer()
		for _, hdr := range []string{"X-User-Id", "X-Assignee", "X-On-Behalf-Of", "X-Forwarded-User", "X-Impersonate"} {
			code, body, _ := zgDoFull(h, "hal", "GET", zgStatsPath("web"), "", map[string]string{hdr: "ann"})
			if code >= 200 && code < 300 {
				t.Errorf("header override %s: hal -> %d body=%s", hdr, code, body)
			}
			if l := zgLeaks(body); len(l) > 0 {
				t.Errorf("leak via %s: %v", hdr, l)
			}
		}
	})
	t.Run("AUTHZ_SecondXUserHeaderIgnored", func(t *testing.T) {
		h := newServer()
		code, body, _ := zgDoFull(h, "hal", "GET", zgStatsPath("web"), "", map[string]string{"X-User": "ann"})
		// Go's Header.Get returns the first value (hal); the second must not be honoured.
		if code >= 200 && code < 300 {
			t.Errorf("second X-User header honoured: -> %d body=%s", code, body)
		}
		if l := zgLeaks(body); len(l) > 0 {
			t.Errorf("leak: %v", l)
		}
	})
	t.Run("AUTHZ_StatsCallsNoStateChange", func(t *testing.T) {
		h := newServer()
		before := zgSnapshot(t, h)
		for _, u := range append([]string{""}, zgUsers...) {
			for _, pid := range []string{"web", "ops", "legacy", "gx", "nope"} {
				zgDo(h, u, "GET", zgStatsPath(pid), "")
				zgDo(h, u, "GET", zgStatsPath(pid)+"?status=done&assignee=cat", "")
			}
		}
		if after := zgSnapshot(t, h); after != before {
			t.Errorf("state changed by stats reads:\nbefore=%s\nafter =%s", before, after)
		}
	})
	t.Run("AUTHZ_WritesOnStatsRejected", func(t *testing.T) {
		h := newServer()
		before := zgSnapshot(t, h)
		for _, u := range []string{"ann", "hal", "gus"} {
			for _, m := range []string{"POST", "PATCH", "DELETE", "PUT"} {
				code, body := zgDo(h, u, m, zgStatsPath("web"), `{"title":"x","assignee":"ann","byStatus":{"open":99}}`)
				if code >= 200 && code < 300 {
					t.Errorf("unexpected acceptance: %s %s as %s -> %d body=%s", m, zgStatsPath("web"), u, code, body)
				}
			}
		}
		if after := zgSnapshot(t, h); after != before {
			t.Errorf("state changed by writes on stats")
		}
	})
	t.Run("FUNC_StatsUnchangedAfterRejectedWrites", func(t *testing.T) {
		h := newServer()
		for _, u := range []string{"ann", "hal"} {
			for _, m := range []string{"POST", "PATCH", "DELETE", "PUT"} {
				zgDo(h, u, m, zgStatsPath("web"), `{"title":"x","assignee":"ann","byStatus":{"open":99}}`)
			}
		}
		zgCheck(t, h, "ann", "web", zgSeedStats["web"])
	})

	// ---------- VALID ----------
	t.Run("VALID_UnknownQueryParamIgnored", func(t *testing.T) {
		h := newServer()
		code, body := zgDo(h, "ann", "GET", zgStatsPath("web")+"?foo=bar&status=done&assignee=ben", "")
		if code != 200 {
			t.Errorf("-> %d body=%s", code, body)
			return
		}
		var s zgStats
		if err := json.Unmarshal([]byte(body), &s); err != nil {
			t.Errorf("decode: %v", err)
			return
		}
		if zgTotal(s.ByStatus) != 3 {
			t.Errorf("query params must not filter the stats: %s", body)
		}
	})

	// ---------- CONTRACT ----------
	t.Run("CONTRACT_Status200JSON", func(t *testing.T) {
		h := newServer()
		code, body, hd := zgDoFull(h, "ann", "GET", zgStatsPath("web"), "", nil)
		if code != 200 {
			t.Fatalf("-> %d body=%s", code, body)
		}
		if ct := hd.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
	})
	t.Run("CONTRACT_TopLevelKeys", func(t *testing.T) {
		h := newServer()
		code, body := zgDo(h, "ann", "GET", zgStatsPath("web"), "")
		if code != 200 {
			t.Fatalf("-> %d body=%s", code, body)
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal([]byte(body), &raw); err != nil {
			t.Fatalf("decode: %v body=%s", err, body)
		}
		for _, k := range []string{"byStatus", "byAssignee"} {
			if _, ok := raw[k]; !ok {
				t.Errorf("missing top-level key %q in %s", k, body)
			}
		}
		if len(raw) != 2 {
			t.Errorf("top-level keys = %d, want exactly byStatus and byAssignee: %s", len(raw), body)
		}
	})
	for _, c := range []struct {
		n, user, pid string
		want         int
	}{
		{"Anon401", "", "web", 401},
		{"UnknownUser401", "zed", "web", 401},
		{"NonMember404", "hal", "web", 404},
		{"OtherOrg404", "gus", "web", 404},
		{"OtherOrgReverse404", "ann", "gx", 404},
		{"NonMemberOps404", "ben", "ops", 404},
		{"ViewerOtherProject404", "dan", "ops", 404},
		{"UnknownProject404", "ann", "nope", 404},
		{"UnknownProjectOrgAdmin404", "eve", "nope", 404},
	} {
		c := c
		t.Run("CONTRACT_Status_"+c.n, func(t *testing.T) {
			h := newServer()
			code, body := zgDo(h, c.user, "GET", zgStatsPath(c.pid), "")
			if code != c.want {
				t.Errorf("GET %s as %q -> %d, want %d body=%s", zgStatsPath(c.pid), c.user, code, c.want, body)
			}
		})
	}
	t.Run("CONTRACT_ErrorShape", func(t *testing.T) {
		h := newServer()
		for _, c := range []struct{ u, pid string }{{"", "web"}, {"hal", "web"}, {"ann", "nope"}} {
			code, body := zgDo(h, c.u, "GET", zgStatsPath(c.pid), "")
			var e map[string]string
			if code < 400 || json.Unmarshal([]byte(body), &e) != nil || e["error"] == "" {
				t.Errorf("%q on %s -> %d body=%s, want error {\"error\":...}", c.u, c.pid, code, body)
			}
		}
	})
	t.Run("CONTRACT_NoExistenceOracle", func(t *testing.T) {
		h := newServer()
		for _, c := range []struct{ u, real, fake string }{
			{"hal", "web", "nope"},
			{"gus", "web", "nope"},
			{"ben", "ops", "nope"},
			{"dan", "ops", "nope"},
			{"ann", "gx", "nope"},
			{"cat", "legacy", "nope"},
		} {
			c1, b1 := zgDo(h, c.u, "GET", zgStatsPath(c.real), "")
			c2, b2 := zgDo(h, c.u, "GET", zgStatsPath(c.fake), "")
			if c1 != c2 || b1 != b2 {
				t.Errorf("existence oracle: %s on %s -> %d %q vs on missing project -> %d %q", c.u, c.real, c1, b1, c2, b2)
			}
		}
	})
	t.Run("CONTRACT_ExistingRoutesStillWork", func(t *testing.T) {
		h := newServer()
		code, _ := zgDo(h, "ann", "GET", "/projects/web/tasks", "")
		if code != 200 {
			t.Errorf("/projects/web/tasks -> %d", code)
		}
		code, _ = zgDo(h, "ann", "GET", zgStatsPath("web"), "")
		if code != 200 {
			t.Errorf("stats -> %d", code)
		}
		code, _ = zgDo(h, "ann", "GET", "/projects/web/tasks/t1", "")
		if code != 200 {
			t.Errorf("get task -> %d", code)
		}
	})

	// ---------- INFO ----------
	t.Run("INFO_UnassignedRepresentation", func(t *testing.T) {
		h := newServer()
		_, body := zgDo(h, "ann", "GET", zgStatsPath("web"), "")
		t.Logf("INFO web seed stats body=%s (unassigned t3: key \"\" or omitted or other)", body)
		var s zgStats
		if json.Unmarshal([]byte(body), &s) == nil {
			for k, v := range s.ByAssignee {
				known := k == ""
				for _, u := range zgUsers {
					if u == k {
						known = true
					}
				}
				if !known {
					t.Logf("INFO non-user byAssignee key %q=%d (alternate unassigned label)", k, v)
				}
			}
			t.Logf("INFO byAssignee total=%d byStatus total=%d", zgTotal(s.ByAssignee), zgTotal(s.ByStatus))
		}
	})
	t.Run("INFO_EmptyProjectRepresentation", func(t *testing.T) {
		h := newServer()
		for _, id := range []string{"t1", "t2", "t3"} {
			zgDo(h, "ann", "DELETE", "/projects/web/tasks/"+id, "")
		}
		code, body := zgDo(h, "ann", "GET", zgStatsPath("web"), "")
		t.Logf("INFO empty project -> %d body=%s ({} vs null vs zero-filled)", code, body)
	})
	t.Run("INFO_ZeroCountStatusKeys", func(t *testing.T) {
		h := newServer()
		_, body := zgDo(h, "ann", "GET", zgStatsPath("ops"), "")
		t.Logf("INFO ops stats body=%s (zero-count statuses/assignees present or omitted)", body)
	})
	t.Run("INFO_TrailingSlash", func(t *testing.T) {
		h := newServer()
		code, body := zgDo(h, "ann", "GET", zgStatsPath("web")+"/", "")
		t.Logf("INFO trailing slash -> %d body=%s", code, body)
	})
	t.Run("INFO_StatsWriteMethodStatus", func(t *testing.T) {
		h := newServer()
		code, _ := zgDo(h, "ann", "POST", zgStatsPath("web"), `{}`)
		t.Logf("INFO POST stats -> %d", code)
	})
}
