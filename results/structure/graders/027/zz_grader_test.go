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

// Hidden grader for task 027 (task comments: POST/GET /projects/{pid}/tasks/{tid}/comments, GET /comments/{cid}).
// Uses only newServer() and HTTP. Comment ids are never assumed beyond "returned by POST"; enumeration
// of c1..c30 is used to catch IDOR on the global /comments/{cid} route.

type zgTask struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	Assignee  string `json:"assignee"`
}

type zgComment struct {
	ID     string `json:"id"`
	TaskID string `json:"taskId"`
	Author string `json:"author"`
	Body   string `json:"body"`
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

func zgJSONBody(text string) string {
	b, _ := json.Marshal(map[string]string{"body": text})
	return string(b)
}

func zgPost(h http.Handler, user, pid, tid, text string) (int, string) {
	return zgDo(h, user, "POST", "/projects/"+pid+"/tasks/"+tid+"/comments", zgJSONBody(text))
}

func zgList(h http.Handler, user, pid, tid string) (int, string) {
	return zgDo(h, user, "GET", "/projects/"+pid+"/tasks/"+tid+"/comments", "")
}

func zgGet(h http.Handler, user, cid string) (int, string) {
	return zgDo(h, user, "GET", "/comments/"+cid, "")
}

func zgIs2xx(c int) bool { return c >= 200 && c < 300 }

// zgReadable: which projects each user may read (README rules).
var zgReadable = map[string][]string{
	"ann": {"web", "ops", "legacy"}, "ben": {"web", "legacy"}, "cat": {"web", "ops"},
	"dan": {"web"}, "eve": {"web", "ops", "legacy"}, "hal": {}, "ivy": {"web", "ops"}, "gus": {"gx"},
	"": {}, "zed": {},
}

func zgCanRead(user, pid string) bool {
	for _, p := range zgReadable[user] {
		if p == pid {
			return true
		}
	}
	return false
}

// A planned comment. text is a unique marker so leaks are greppable.
type zgMark struct {
	user, pid, tid, text string
	id                   string // filled in when POST returned 201
}

func zgPlan() []*zgMark {
	return []*zgMark{
		{user: "ann", pid: "web", tid: "t1", text: "ZGMARK-ann-web-t1"},
		{user: "ben", pid: "web", tid: "t2", text: "ZGMARK-ben-web-t2"},
		{user: "dan", pid: "web", tid: "t3", text: "ZGMARK-dan-web-t3"},
		{user: "ivy", pid: "ops", tid: "t4", text: "ZGMARK-ivy-ops-t4"},
		{user: "ann", pid: "ops", tid: "t4", text: "ZGMARK-ann-ops-t4"},
		{user: "gus", pid: "gx", tid: "t6", text: "ZGMARK-gus-gx-t6"},
	}
}

// zgSeedMarks posts the plan; failures are tolerated (id stays "") so AUTHZ checks degrade to vacuous.
func zgSeedMarks(h http.Handler) []*zgMark {
	ms := zgPlan()
	for _, m := range ms {
		code, body := zgPost(h, m.user, m.pid, m.tid, m.text)
		if code == 201 {
			var c zgComment
			if json.Unmarshal([]byte(body), &c) == nil {
				m.id = c.ID
			}
		}
	}
	return ms
}

func zgMarkID(m *zgMark) string {
	if m.id == "" {
		return "c1"
	}
	return m.id
}

// zgLeaked returns marker texts present in body that user may not read.
func zgLeaked(user, body string, ms []*zgMark) []string {
	var l []string
	for _, m := range ms {
		if !zgCanRead(user, m.pid) && strings.Contains(body, m.text) {
			l = append(l, m.text)
		}
	}
	return l
}

var zgUsers = []string{"ann", "ben", "cat", "dan", "eve", "hal", "ivy", "gus", "", "zed"}

var zgPaths = []struct{ pid, tid string }{
	{"web", "t1"}, {"web", "t2"}, {"web", "t3"}, {"ops", "t4"}, {"legacy", "t5"}, {"gx", "t6"},
	{"web", "t4"}, {"ops", "t1"}, {"gx", "t1"}, {"web", "t6"}, {"ops", "t6"},
}

// zgSnap serialises everything observable about comments through eve (reads all acme) and gus (gx).
func zgSnap(h http.Handler) string {
	var b strings.Builder
	for _, u := range []string{"eve", "gus"} {
		for _, p := range zgPaths {
			c, body := zgList(h, u, p.pid, p.tid)
			fmt.Fprintf(&b, "L %s %s/%s %d %s\n", u, p.pid, p.tid, c, body)
		}
		for i := 1; i <= 30; i++ {
			c, body := zgGet(h, u, fmt.Sprintf("c%d", i))
			fmt.Fprintf(&b, "G %s c%d %d %s\n", u, i, c, body)
		}
	}
	return b.String()
}

func zgMust(t *testing.T, h http.Handler, user, method, path, body string, want int) string {
	t.Helper()
	code, resp := zgDo(h, user, method, path, body)
	if code != want {
		t.Fatalf("%s %s as %q -> %d, want %d body=%s", method, path, user, code, want, resp)
	}
	return resp
}

func zgPostOK(t *testing.T, h http.Handler, user, pid, tid, text string) zgComment {
	t.Helper()
	code, body := zgPost(h, user, pid, tid, text)
	if code != 201 {
		t.Fatalf("POST comment %s/%s as %q -> %d body=%s", pid, tid, user, code, body)
	}
	var c zgComment
	if err := json.Unmarshal([]byte(body), &c); err != nil {
		t.Fatalf("decode comment: %v body=%s", err, body)
	}
	return c
}

func zgDecodeList(t *testing.T, body string) []zgComment {
	t.Helper()
	var cs []zgComment
	if err := json.Unmarshal([]byte(body), &cs); err != nil {
		t.Fatalf("decode comment list: %v body=%s", err, body)
	}
	return cs
}

// zgDeniedPost: non-2xx, nothing observable changed, no leak; want != 0 additionally checks the status.
func zgDeniedPost(t *testing.T, user, pid, tid string, want int) {
	t.Helper()
	h := newServer()
	ms := zgSeedMarks(h)
	before := zgSnap(h)
	code, resp := zgPost(h, user, pid, tid, "ZGATTACK-post")
	after := zgSnap(h)
	if zgIs2xx(code) {
		t.Errorf("unexpected acceptance: POST %s/%s comment as %q -> %d body=%s", pid, tid, user, code, resp)
	}
	if before != after {
		t.Errorf("state changed: comment snapshot differs after denied POST %s/%s as %q (status %d)", pid, tid, user, code)
	}
	if strings.Contains(after, "ZGATTACK-post") {
		t.Errorf("state changed: attack comment visible after POST %s/%s as %q", pid, tid, user)
	}
	if l := zgLeaked(user, resp, ms); len(l) > 0 {
		t.Errorf("leak: response exposes %v: %s", l, resp)
	}
	if want != 0 && code != want {
		t.Errorf("status %d, want %d (body=%s)", code, want, resp)
	}
}

// zgDeniedRead: non-2xx (or at least no unauthorized data), and no leak of unreadable markers.
func zgDeniedRead(t *testing.T, h http.Handler, ms []*zgMark, user, path string, want int) {
	t.Helper()
	code, resp := zgDo(h, user, "GET", path, "")
	if zgIs2xx(code) {
		t.Errorf("unexpected acceptance: GET %s as %q -> %d body=%s", path, user, code, resp)
	}
	if l := zgLeaked(user, resp, ms); len(l) > 0 {
		t.Errorf("leak: GET %s as %q exposes %v: %s", path, user, l, resp)
	}
	if want != 0 && code != want {
		t.Errorf("GET %s as %q status %d, want %d", path, user, code, want)
	}
}

func zgTasksOf(t *testing.T, h http.Handler, user, pid string) []zgTask {
	t.Helper()
	body := zgMust(t, h, user, "GET", "/projects/"+pid+"/tasks", "", 200)
	var ts []zgTask
	if err := json.Unmarshal([]byte(body), &ts); err != nil {
		t.Fatalf("decode tasks: %v", err)
	}
	return ts
}

func TestGrader(t *testing.T) {
	// ---------------- REG ----------------
	t.Run("REG_list_tasks", func(t *testing.T) {
		h := newServer()
		if ts := zgTasksOf(t, h, "ann", "web"); len(ts) != 3 {
			t.Errorf("ann web tasks = %d, want 3", len(ts))
		}
		if ts := zgTasksOf(t, h, "eve", "ops"); len(ts) != 1 {
			t.Errorf("eve ops tasks = %d, want 1", len(ts))
		}
		zgMust(t, h, "hal", "GET", "/projects/web/tasks", "", 404)
		zgMust(t, h, "gus", "GET", "/projects/web/tasks", "", 404)
		zgMust(t, h, "", "GET", "/projects/web/tasks", "", 401)
	})
	t.Run("REG_task_writes_and_rules", func(t *testing.T) {
		h := newServer()
		zgMust(t, h, "dan", "POST", "/projects/web/tasks", `{"title":"x"}`, 403)
		zgMust(t, h, "ann", "POST", "/projects/legacy/tasks", `{"title":"x"}`, 409)
		zgMust(t, h, "hal", "POST", "/projects/web/tasks", `{"title":"x"}`, 404)
		zgMust(t, h, "ann", "GET", "/projects/web/tasks/t4", "", 404)
		zgMust(t, h, "ann", "POST", "/projects/web/tasks", `{"title":"new one"}`, 201)
		zgMust(t, h, "ben", "PATCH", "/projects/web/tasks/t1", `{"status":"doing"}`, 200)
		zgMust(t, h, "ben", "PATCH", "/projects/web/tasks/t2", `{"status":"doing"}`, 403)
		zgMust(t, h, "ben", "DELETE", "/projects/web/tasks/t1", "", 403)
		zgMust(t, h, "ann", "DELETE", "/projects/web/tasks/t3", "", 204)
	})
	t.Run("REG_tasks_unaffected_by_comment_attempts", func(t *testing.T) {
		h := newServer()
		zgSeedMarks(h)
		ts := zgTasksOf(t, h, "ann", "web")
		if len(ts) != 3 || ts[0].Title != "Fix header" || ts[0].Status != "open" || ts[0].Assignee != "ben" {
			t.Errorf("web tasks changed after comment attempts: %+v", ts)
		}
		for _, tk := range zgTasksOf(t, h, "eve", "ops") {
			if tk.Title != "Rotate keys" || tk.Status != "open" || tk.Assignee != "cat" {
				t.Errorf("ops task changed: %+v", tk)
			}
		}
	})

	// ---------------- FUNC ----------------
	t.Run("FUNC_post_manager_ann_shape", func(t *testing.T) {
		h := newServer()
		c := zgPostOK(t, h, "ann", "web", "t1", "hello world")
		if c.ID == "" || c.TaskID != "t1" || c.Author != "ann" || c.Body != "hello world" {
			t.Errorf("bad comment shape: %+v", c)
		}
		code, body := zgPost(h, "ann", "web", "t1", "again")
		var raw map[string]any
		_ = json.Unmarshal([]byte(body), &raw)
		for _, k := range []string{"id", "taskId", "author", "body"} {
			if _, ok := raw[k]; !ok || code != 201 {
				t.Errorf("response missing key %q (code %d): %s", k, code, body)
			}
		}
	})
	t.Run("FUNC_post_contributor_ben", func(t *testing.T) {
		h := newServer()
		c := zgPostOK(t, h, "ben", "web", "t2", "from ben")
		if c.Author != "ben" || c.TaskID != "t2" {
			t.Errorf("bad comment: %+v", c)
		}
	})
	t.Run("FUNC_post_viewer_dan", func(t *testing.T) {
		h := newServer()
		c := zgPostOK(t, h, "dan", "web", "t1", "viewer comment")
		if c.Author != "dan" || c.TaskID != "t1" || c.Body != "viewer comment" {
			t.Errorf("bad comment: %+v", c)
		}
	})
	t.Run("FUNC_post_viewer_ann_in_ops", func(t *testing.T) {
		h := newServer()
		c := zgPostOK(t, h, "ann", "ops", "t4", "ann is a viewer in ops")
		if c.Author != "ann" || c.TaskID != "t4" {
			t.Errorf("bad comment: %+v", c)
		}
	})
	t.Run("FUNC_post_other_org_gus_own_project", func(t *testing.T) {
		h := newServer()
		c := zgPostOK(t, h, "gus", "gx", "t6", "globex note")
		if c.Author != "gus" || c.TaskID != "t6" {
			t.Errorf("bad comment: %+v", c)
		}
	})
	t.Run("FUNC_author_and_ids_not_spoofable", func(t *testing.T) {
		h := newServer()
		code, body := zgDo(h, "dan", "POST", "/projects/web/tasks/t1/comments",
			`{"body":"spoof","author":"eve","taskId":"t4","id":"c99","projectId":"ops"}`)
		if code != 201 {
			t.Fatalf("POST -> %d %s", code, body)
		}
		var c zgComment
		_ = json.Unmarshal([]byte(body), &c)
		if c.Author != "dan" {
			t.Errorf("author spoofed: %+v", c)
		}
		if c.TaskID != "t1" {
			t.Errorf("taskId spoofed: %+v", c)
		}
		if c.ID == "c99" {
			t.Errorf("id spoofed: %+v", c)
		}
		// the comment must not have appeared under ops/t4
		_, l := zgList(h, "ann", "ops", "t4")
		if strings.Contains(l, "spoof") {
			t.Errorf("spoofed comment visible under ops/t4: %s", l)
		}
	})
	t.Run("FUNC_list_order_and_fields", func(t *testing.T) {
		h := newServer()
		zgPostOK(t, h, "ann", "web", "t1", "first")
		zgPostOK(t, h, "dan", "web", "t1", "second")
		zgPostOK(t, h, "ben", "web", "t1", "third")
		body := zgMust(t, h, "cat", "GET", "/projects/web/tasks/t1/comments", "", 200)
		cs := zgDecodeList(t, body)
		if len(cs) != 3 {
			t.Fatalf("got %d comments: %s", len(cs), body)
		}
		want := []struct{ a, b string }{{"ann", "first"}, {"dan", "second"}, {"ben", "third"}}
		for i, w := range want {
			if cs[i].Author != w.a || cs[i].Body != w.b || cs[i].TaskID != "t1" || cs[i].ID == "" {
				t.Errorf("comment %d = %+v, want author %s body %s", i, cs[i], w.a, w.b)
			}
		}
	})
	t.Run("FUNC_list_empty_is_array", func(t *testing.T) {
		h := newServer()
		body := zgMust(t, h, "eve", "GET", "/projects/web/tasks/t3/comments", "", 200)
		if !strings.HasPrefix(strings.TrimSpace(body), "[") {
			t.Errorf("empty list not a JSON array: %s", body)
		}
		if cs := zgDecodeList(t, body); len(cs) != 0 {
			t.Errorf("expected no comments: %s", body)
		}
	})
	t.Run("FUNC_list_scoped_to_task", func(t *testing.T) {
		h := newServer()
		ms := zgSeedMarks(h)
		_ = ms
		_, b1 := zgList(h, "ann", "web", "t1")
		_, b2 := zgList(h, "ann", "web", "t2")
		if !strings.Contains(b1, "ZGMARK-ann-web-t1") {
			t.Errorf("t1 list missing its comment: %s", b1)
		}
		if strings.Contains(b1, "ZGMARK-ben-web-t2") || strings.Contains(b1, "ZGMARK-dan-web-t3") || strings.Contains(b1, "ZGMARK-ivy-ops-t4") {
			t.Errorf("cross-task leakage: t1 list contains other task comments: %s", b1)
		}
		if !strings.Contains(b2, "ZGMARK-ben-web-t2") || strings.Contains(b2, "ZGMARK-ann-web-t1") {
			t.Errorf("t2 list wrong: %s", b2)
		}
		_, b4 := zgList(h, "ann", "ops", "t4")
		if !strings.Contains(b4, "ZGMARK-ivy-ops-t4") || !strings.Contains(b4, "ZGMARK-ann-ops-t4") || strings.Contains(b4, "ZGMARK-ann-web-t1") {
			t.Errorf("ops/t4 list wrong: %s", b4)
		}
	})
	t.Run("FUNC_list_readers_incl_viewer_and_org_admin", func(t *testing.T) {
		h := newServer()
		zgPostOK(t, h, "ann", "web", "t1", "visible to all readers")
		for _, u := range []string{"ann", "ben", "cat", "dan", "eve", "ivy"} {
			body := zgMust(t, h, u, "GET", "/projects/web/tasks/t1/comments", "", 200)
			if cs := zgDecodeList(t, body); len(cs) != 1 || cs[0].Body != "visible to all readers" {
				t.Errorf("%s sees %s", u, body)
			}
		}
	})
	t.Run("FUNC_ids_unique_across_projects", func(t *testing.T) {
		h := newServer()
		a := zgPostOK(t, h, "ann", "web", "t1", "a")
		b := zgPostOK(t, h, "ivy", "ops", "t4", "b")
		c := zgPostOK(t, h, "gus", "gx", "t6", "c")
		d := zgPostOK(t, h, "ben", "web", "t1", "d")
		ids := map[string]bool{a.ID: true, b.ID: true, c.ID: true, d.ID: true}
		if len(ids) != 4 {
			t.Errorf("comment ids not globally unique: %s %s %s %s", a.ID, b.ID, c.ID, d.ID)
		}
		for _, x := range []struct {
			u string
			c zgComment
		}{{"ann", a}, {"ivy", b}, {"gus", c}, {"ben", d}} {
			body := zgMust(t, h, x.u, "GET", "/comments/"+x.c.ID, "", 200)
			var got zgComment
			_ = json.Unmarshal([]byte(body), &got)
			if got != x.c {
				t.Errorf("GET /comments/%s as %s = %+v, want %+v", x.c.ID, x.u, got, x.c)
			}
		}
	})
	t.Run("FUNC_get_by_id_web_readers", func(t *testing.T) {
		h := newServer()
		ms := zgSeedMarks(h)
		m := ms[0]
		if m.id == "" {
			t.Fatalf("seed comment not created")
		}
		for _, u := range []string{"ann", "ben", "cat", "dan", "eve", "ivy"} {
			body := zgMust(t, h, u, "GET", "/comments/"+m.id, "", 200)
			var c zgComment
			_ = json.Unmarshal([]byte(body), &c)
			if c.ID != m.id || c.TaskID != "t1" || c.Author != "ann" || c.Body != m.text {
				t.Errorf("%s got %+v", u, c)
			}
		}
	})
	t.Run("FUNC_get_by_id_ops_readers_incl_viewer_and_admin", func(t *testing.T) {
		h := newServer()
		ms := zgSeedMarks(h)
		m := ms[3]
		if m.id == "" {
			t.Fatalf("seed comment not created")
		}
		for _, u := range []string{"ann", "cat", "ivy", "eve"} {
			body := zgMust(t, h, u, "GET", "/comments/"+m.id, "", 200)
			var c zgComment
			_ = json.Unmarshal([]byte(body), &c)
			if c.TaskID != "t4" || c.Author != "ivy" || c.Body != m.text {
				t.Errorf("%s got %+v", u, c)
			}
		}
	})
	t.Run("FUNC_get_by_id_gx_reader", func(t *testing.T) {
		h := newServer()
		ms := zgSeedMarks(h)
		m := ms[5]
		if m.id == "" {
			t.Fatalf("seed comment not created")
		}
		body := zgMust(t, h, "gus", "GET", "/comments/"+m.id, "", 200)
		var c zgComment
		_ = json.Unmarshal([]byte(body), &c)
		if c.TaskID != "t6" || c.Author != "gus" || c.Body != m.text {
			t.Errorf("got %+v", c)
		}
	})

	// ---------------- AUTHZ: reads ----------------
	for _, tc := range []struct {
		user string
		idx  int
	}{
		{"hal", 0}, {"gus", 0}, {"zed", 0}, {"", 0}, // web comment
		{"hal", 3}, {"gus", 3}, {"dan", 3}, {"ben", 3}, {"", 3}, // ops comment
		{"ann", 5}, {"eve", 5}, {"hal", 5}, // gx comment, acme users
	} {
		tc := tc
		t.Run(fmt.Sprintf("AUTHZ_get_by_id_%s_%s", zgName(tc.user), zgPlan()[tc.idx].pid), func(t *testing.T) {
			h := newServer()
			ms := zgSeedMarks(h)
			zgDeniedRead(t, h, ms, tc.user, "/comments/"+zgMarkID(ms[tc.idx]), 0)
		})
	}
	for _, user := range []string{"hal", "gus", "dan", "ben", "zed", ""} {
		user := user
		t.Run("AUTHZ_idor_enumerate_"+zgName(user), func(t *testing.T) {
			h := newServer()
			ms := zgSeedMarks(h)
			for i := 1; i <= 30; i++ {
				cid := fmt.Sprintf("c%d", i)
				code, body := zgGet(h, user, cid)
				if l := zgLeaked(user, body, ms); len(l) > 0 {
					t.Errorf("leak: GET /comments/%s as %q (%d) exposes %v", cid, user, code, l)
				}
			}
		})
	}
	t.Run("AUTHZ_idor_enumerate_crossorg_gx_comment", func(t *testing.T) {
		h := newServer()
		ms := zgSeedMarks(h)
		for _, user := range []string{"ann", "ben", "cat", "eve", "ivy"} {
			for i := 1; i <= 30; i++ {
				cid := fmt.Sprintf("c%d", i)
				code, body := zgGet(h, user, cid)
				if l := zgLeaked(user, body, ms); len(l) > 0 {
					t.Errorf("leak: GET /comments/%s as %q (%d) exposes %v", cid, user, code, l)
				}
			}
		}
	})
	for _, tc := range []struct{ user, pid, tid string }{
		{"hal", "web", "t1"}, {"gus", "web", "t1"}, {"zed", "web", "t1"}, {"", "web", "t1"},
		{"dan", "ops", "t4"}, {"ben", "ops", "t4"}, {"hal", "ops", "t4"}, {"gus", "ops", "t4"},
		{"ann", "gx", "t6"}, {"eve", "gx", "t6"}, {"hal", "gx", "t6"},
		// task id of a project the caller cannot read, via a project the caller can read
		{"ben", "web", "t4"}, {"dan", "web", "t4"}, {"hal", "web", "t4"},
		{"gus", "gx", "t4"}, {"gus", "gx", "t1"}, {"ann", "gx", "t4"},
	} {
		tc := tc
		t.Run(fmt.Sprintf("AUTHZ_list_%s_%s_%s", zgName(tc.user), tc.pid, tc.tid), func(t *testing.T) {
			h := newServer()
			ms := zgSeedMarks(h)
			zgDeniedRead(t, h, ms, tc.user, "/projects/"+tc.pid+"/tasks/"+tc.tid+"/comments", 0)
		})
	}

	// ---------------- AUTHZ: writes ----------------
	for _, tc := range []struct{ user, pid, tid string }{
		{"hal", "web", "t1"}, {"gus", "web", "t1"}, {"zed", "web", "t1"}, {"", "web", "t1"},
		{"dan", "ops", "t4"}, {"ben", "ops", "t4"}, {"hal", "ops", "t4"}, {"gus", "ops", "t4"},
		{"ann", "gx", "t6"}, {"eve", "gx", "t6"}, {"ann", "nosuch", "t1"},
		{"ann", "web", "t4"}, {"dan", "web", "t4"}, {"ben", "web", "t4"}, {"ann", "web", "t6"},
		{"gus", "gx", "t1"}, {"gus", "gx", "t4"}, {"ann", "ops", "t1"},
		{"ann", "web", "nosuch"},
		{"ann", "legacy", "t5"}, {"ben", "legacy", "t5"},
	} {
		tc := tc
		t.Run(fmt.Sprintf("AUTHZ_post_%s_%s_%s", zgName(tc.user), tc.pid, tc.tid), func(t *testing.T) {
			zgDeniedPost(t, tc.user, tc.pid, tc.tid, 0)
		})
	}

	// ---------------- VALID ----------------
	for _, tc := range []struct{ name, body string }{
		{"malformed_json", `{"body":`},
		{"not_json", `hello`},
		{"body_wrong_type", `{"body":5}`},
		{"body_null", `{"body":null}`},
	} {
		tc := tc
		t.Run("VALID_post_"+tc.name, func(t *testing.T) {
			h := newServer()
			zgSeedMarks(h)
			before := zgSnap(h)
			code, resp := zgDo(h, "ann", "POST", "/projects/web/tasks/t1/comments", tc.body)
			if zgIs2xx(code) {
				t.Errorf("unexpected acceptance: %s -> %d %s", tc.body, code, resp)
			}
			if before != zgSnap(h) {
				t.Errorf("state changed after invalid POST %s", tc.body)
			}
		})
	}

	// ---------------- CONTRACT ----------------
	t.Run("CONTRACT_get_by_id_denied_is_404", func(t *testing.T) {
		h := newServer()
		ms := zgSeedMarks(h)
		for _, c := range []struct {
			user string
			idx  int
		}{{"hal", 0}, {"gus", 0}, {"dan", 3}, {"ben", 3}, {"ann", 5}, {"eve", 5}} {
			code, _ := zgGet(h, c.user, zgMarkID(ms[c.idx]))
			if code != 404 {
				t.Errorf("GET /comments/%s as %s -> %d, want 404", zgMarkID(ms[c.idx]), c.user, code)
			}
		}
	})
	t.Run("CONTRACT_get_by_id_unknown_is_404", func(t *testing.T) {
		h := newServer()
		zgSeedMarks(h)
		zgMust(t, h, "ann", "GET", "/comments/c9999", "", 404)
		zgMust(t, h, "eve", "GET", "/comments/nosuch", "", 404)
	})
	t.Run("CONTRACT_get_by_id_unauthenticated_is_401", func(t *testing.T) {
		h := newServer()
		ms := zgSeedMarks(h)
		zgMust(t, h, "", "GET", "/comments/"+zgMarkID(ms[0]), "", 401)
		zgMust(t, h, "zed", "GET", "/comments/"+zgMarkID(ms[0]), "", 401)
	})
	t.Run("CONTRACT_post_unauthenticated_is_401", func(t *testing.T) {
		h := newServer()
		code, _ := zgPost(h, "", "web", "t1", "x")
		if code != 401 {
			t.Errorf("anonymous POST -> %d, want 401", code)
		}
		code, _ = zgPost(h, "zed", "web", "t1", "x")
		if code != 401 {
			t.Errorf("unknown user POST -> %d, want 401", code)
		}
		code, _ = zgList(h, "", "web", "t1")
		if code != 401 {
			t.Errorf("anonymous list -> %d, want 401", code)
		}
	})
	t.Run("CONTRACT_post_nonmember_is_404", func(t *testing.T) {
		h := newServer()
		for _, c := range []struct{ user, pid, tid string }{
			{"hal", "web", "t1"}, {"gus", "web", "t1"}, {"dan", "ops", "t4"}, {"ben", "ops", "t4"}, {"ann", "gx", "t6"}, {"ann", "nosuch", "t1"},
		} {
			code, _ := zgPost(h, c.user, c.pid, c.tid, "x")
			if code != 404 {
				t.Errorf("POST %s/%s as %s -> %d, want 404", c.pid, c.tid, c.user, code)
			}
		}
	})
	t.Run("CONTRACT_list_nonmember_is_404", func(t *testing.T) {
		h := newServer()
		for _, c := range []struct{ user, pid, tid string }{
			{"hal", "web", "t1"}, {"gus", "web", "t1"}, {"dan", "ops", "t4"}, {"ann", "gx", "t6"},
		} {
			code, _ := zgList(h, c.user, c.pid, c.tid)
			if code != 404 {
				t.Errorf("list %s/%s as %s -> %d, want 404", c.pid, c.tid, c.user, code)
			}
		}
	})
	t.Run("CONTRACT_task_not_in_path_project_is_404", func(t *testing.T) {
		h := newServer()
		code, _ := zgPost(h, "ann", "web", "t4", "x")
		if code != 404 {
			t.Errorf("POST web/t4 -> %d, want 404", code)
		}
		code, _ = zgList(h, "ann", "web", "t4")
		if code != 404 {
			t.Errorf("list web/t4 -> %d, want 404", code)
		}
		code, _ = zgPost(h, "ann", "web", "nosuch", "x")
		if code != 404 {
			t.Errorf("POST web/nosuch -> %d, want 404", code)
		}
	})
	t.Run("CONTRACT_post_archived_is_409", func(t *testing.T) {
		h := newServer()
		code, _ := zgPost(h, "ann", "legacy", "t5", "x")
		if code != 409 {
			t.Errorf("POST legacy/t5 as ann (manager) -> %d, want 409", code)
		}
		code, _ = zgPost(h, "ben", "legacy", "t5", "x")
		if code != 409 {
			t.Errorf("POST legacy/t5 as ben -> %d, want 409", code)
		}
	})

	// ---------------- INFO (log only) ----------------
	t.Run("INFO_org_admin_eve_posts", func(t *testing.T) {
		h := newServer()
		before := zgSnap(h)
		code, body := zgPost(h, "eve", "web", "t1", "ZGINFO-eve")
		created := strings.Contains(zgSnap(h), "ZGINFO-eve")
		t.Logf("INFO org admin (non-member) POST comment: status=%d created=%v changed=%v body=%s", code, created, before != zgSnap(h), body)
	})
	t.Run("INFO_empty_and_missing_body", func(t *testing.T) {
		h := newServer()
		c1, b1 := zgDo(h, "ann", "POST", "/projects/web/tasks/t1/comments", `{"body":""}`)
		c2, b2 := zgDo(h, "ann", "POST", "/projects/web/tasks/t1/comments", `{}`)
		c3, b3 := zgDo(h, "ann", "POST", "/projects/web/tasks/t1/comments", ``)
		t.Logf("INFO empty body: %d %s | missing body: %d %s | no payload: %d %s", c1, b1, c2, b2, c3, b3)
	})
	t.Run("INFO_very_long_body", func(t *testing.T) {
		h := newServer()
		code, body := zgPost(h, "ann", "web", "t1", strings.Repeat("x", 200000))
		t.Logf("INFO 200KB body: status=%d respLen=%d", code, len(body))
	})
	t.Run("INFO_archived_read", func(t *testing.T) {
		h := newServer()
		code, body := zgList(h, "ann", "legacy", "t5")
		code2, _ := zgList(h, "eve", "legacy", "t5")
		t.Logf("INFO archived list: ann=%d eve=%d body=%s", code, code2, body)
	})
	t.Run("INFO_task_id_unaffected_by_comments", func(t *testing.T) {
		h := newServer()
		zgSeedMarks(h)
		code, body := zgDo(h, "ann", "POST", "/projects/web/tasks", `{"title":"after comments"}`)
		t.Logf("INFO task created after comments: %d %s (expect id t7)", code, body)
	})
	t.Run("INFO_comment_after_task_deleted", func(t *testing.T) {
		h := newServer()
		ms := zgSeedMarks(h)
		zgDo(h, "ann", "DELETE", "/projects/web/tasks/t1", "")
		code, body := zgGet(h, "ann", zgMarkID(ms[0]))
		lc, lb := zgList(h, "ann", "web", "t1")
		t.Logf("INFO after task delete: GET comment=%d %s ; list=%d %s", code, body, lc, lb)
		// deleted-task comment readable by someone outside the project would still be a leak
		ccode, cbody := zgGet(h, "hal", zgMarkID(ms[0]))
		if zgIs2xx(ccode) && strings.Contains(cbody, ms[0].text) {
			t.Logf("INFO leak after task delete: hal reads comment of deleted task (%d)", ccode)
		}
	})
	t.Run("INFO_ids_format", func(t *testing.T) {
		h := newServer()
		ms := zgSeedMarks(h)
		var ids []string
		for _, m := range ms {
			ids = append(ids, m.id)
		}
		sort.Strings(ids)
		t.Logf("INFO comment ids: %v", ids)
	})
}

func zgName(u string) string {
	switch u {
	case "":
		return "anon"
	default:
		return u
	}
}
