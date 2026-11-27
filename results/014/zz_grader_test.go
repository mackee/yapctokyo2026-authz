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

// Hidden grader for task 014 (merge projects). Uses only newServer() and HTTP.

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

func zgMergeBody(into string) string {
	b, _ := json.Marshal(map[string]any{"intoProjectId": into})
	return string(b)
}

var zgSnapReaders = []struct{ user, pid string }{{"ann", "web"}, {"ann", "ops"}, {"ann", "legacy"}, {"gus", "gx"}}

// zgSnapLoose reads every task; projects that cannot be listed are skipped (returns the codes).
func zgSnapLoose(h http.Handler) (map[string]zgTask, map[string]int) {
	out := map[string]zgTask{}
	codes := map[string]int{}
	for _, c := range zgSnapReaders {
		code, body := zgDo(h, c.user, "GET", "/projects/"+c.pid+"/tasks", "")
		codes[c.pid] = code
		if code != 200 {
			continue
		}
		var ts []zgTask
		if json.Unmarshal([]byte(body), &ts) != nil {
			continue
		}
		for _, x := range ts {
			out[x.ID] = x
		}
	}
	return out, codes
}

func zgSnap(t *testing.T, h http.Handler) map[string]zgTask {
	t.Helper()
	out, codes := zgSnapLoose(h)
	for pid, c := range codes {
		if c != 200 {
			t.Fatalf("grader snapshot: GET %s -> %d", pid, c)
		}
	}
	return out
}

func zgDiff(a, b map[string]zgTask) string {
	var d []string
	for id, x := range a {
		y, ok := b[id]
		if !ok {
			d = append(d, id+" deleted")
		} else if x != y {
			d = append(d, fmt.Sprintf("%s %+v -> %+v", id, x, y))
		}
	}
	for id := range b {
		if _, ok := a[id]; !ok {
			d = append(d, id+" created")
		}
	}
	sort.Strings(d)
	return strings.Join(d, "; ")
}

var zgReadable = map[string][]string{
	"ann": {"web", "ops", "legacy"},
	"ben": {"web", "legacy"},
	"cat": {"web", "ops"},
	"dan": {"web"},
	"eve": {"web", "ops", "legacy"},
	"ivy": {"web", "ops"},
	"gus": {"gx"},
}

func zgLeaks(user, body string, seed map[string]zgTask) []string {
	ok := map[string]bool{}
	for _, p := range zgReadable[user] {
		ok[p] = true
	}
	var l []string
	for _, tk := range seed {
		if !ok[tk.ProjectID] && strings.Contains(body, tk.Title) {
			l = append(l, tk.Title)
		}
	}
	return l
}

// zgStep is a setup action performed before the merge, via the pre-existing API.
type zgStep struct {
	user, method, path, body string
}

func zgAssign(user, pid, tid, assignee string) zgStep {
	return zgStep{user, "PATCH", "/projects/" + pid + "/tasks/" + tid, fmt.Sprintf(`{"assignee":%q}`, assignee)}
}

func zgCreate(user, pid, title, assignee string) zgStep {
	return zgStep{user, "POST", "/projects/" + pid + "/tasks", fmt.Sprintf(`{"title":%q,"assignee":%q}`, title, assignee)}
}

func zgDelete(user, pid, tid string) zgStep {
	return zgStep{user, "DELETE", "/projects/" + pid + "/tasks/" + tid, ""}
}

func zgSetup(t *testing.T, h http.Handler, steps []zgStep) {
	t.Helper()
	for _, s := range steps {
		code, body := zgDo(h, s.user, s.method, s.path, s.body)
		if code < 200 || code >= 300 {
			t.Fatalf("grader setup failed: %s %s as %s -> %d %s", s.method, s.path, s.user, code, body)
		}
	}
}

func zgDenied(t *testing.T, user, from, into string, steps []zgStep) int {
	t.Helper()
	h := newServer()
	zgSetup(t, h, steps)
	before := zgSnap(t, h)
	code, resp := zgDo(h, user, "POST", "/projects/"+from+"/merge", zgMergeBody(into))
	after := zgSnap(t, h)
	if code >= 200 && code < 300 {
		t.Errorf("unexpected acceptance: status %d body=%s", code, resp)
	}
	if d := zgDiff(before, after); d != "" {
		t.Errorf("state changed: %s", d)
	}
	if l := zgLeaks(user, resp, before); len(l) > 0 {
		t.Errorf("leak: response body exposes titles %v: %s", l, resp)
	}
	return code
}

type zgCase struct {
	name   string
	user   string
	from   string
	into   string
	steps  []zgStep
	want   int // 0 = ambiguous, no CONTRACT test
	atomic bool
}

func TestGrader(t *testing.T) {
	// ---------- REG ----------
	for _, c := range []struct {
		n, user, pid string
		want         int
	}{
		{"ListManager", "ann", "web", 200}, {"ListViewer", "dan", "web", 200},
		{"ListOrgAdmin", "eve", "web", 200}, {"ListNonMember404", "hal", "web", 404},
		{"ListOtherOrg404", "gus", "web", 404}, {"ListAnon401", "", "web", 401},
		{"ListIvyOps", "ivy", "ops", 200}, {"ListArchived", "ann", "legacy", 200},
	} {
		c := c
		t.Run("REG_"+c.n, func(t *testing.T) {
			code, _ := zgDo(newServer(), c.user, "GET", "/projects/"+c.pid+"/tasks", "")
			if code != c.want {
				t.Fatalf("got %d want %d", code, c.want)
			}
		})
	}
	for _, c := range []struct {
		n, user, pid, tid, body string
		want                    int
	}{
		{"SinglePatchManagerReassign", "ann", "web", "t1", `{"assignee":"cat"}`, 200},
		{"SinglePatchContributorReassign403", "ben", "web", "t1", `{"assignee":"cat"}`, 403},
		{"SinglePatchContributorOwnStatus", "ben", "web", "t1", `{"status":"done"}`, 200},
		{"SinglePatchContributorOthers403", "ben", "web", "t2", `{"status":"done"}`, 403},
		{"SinglePatchViewer403", "dan", "web", "t1", `{"status":"done"}`, 403},
		{"SinglePatchOrgAdmin403", "eve", "web", "t1", `{"status":"done"}`, 403},
		{"SinglePatchBadAssignee400", "ann", "web", "t1", `{"assignee":"dan"}`, 400},
		{"SinglePatchArchived409", "ann", "legacy", "t5", `{"status":"done"}`, 409},
		{"SinglePatchWrongProject404", "ann", "web", "t4", `{"status":"done"}`, 404},
		{"SinglePatchNonMember404", "hal", "web", "t1", `{"status":"done"}`, 404},
		{"SinglePatchAnon401", "", "web", "t1", `{"status":"done"}`, 401},
		{"DeleteContributor403", "ben", "web", "t1", "", 403},
		{"DeleteManager204", "ann", "web", "t3", "", 204},
		{"GetTask", "ben", "web", "t1", "", 200},
	} {
		c := c
		t.Run("REG_"+c.n, func(t *testing.T) {
			m := "PATCH"
			if c.n == "GetTask" {
				m = "GET"
			} else if c.body == "" {
				m = "DELETE"
			}
			code, _ := zgDo(newServer(), c.user, m, "/projects/"+c.pid+"/tasks/"+c.tid, c.body)
			if code != c.want {
				t.Fatalf("got %d want %d", code, c.want)
			}
		})
	}
	t.Run("REG_CreateTask", func(t *testing.T) {
		code, _ := zgDo(newServer(), "ann", "POST", "/projects/web/tasks", `{"title":"x","assignee":"ben"}`)
		if code != 201 {
			t.Fatalf("got %d", code)
		}
	})
	t.Run("REG_CreateContributorOthers403", func(t *testing.T) {
		code, _ := zgDo(newServer(), "ben", "POST", "/projects/web/tasks", `{"title":"x","assignee":"cat"}`)
		if code != 403 {
			t.Fatalf("got %d", code)
		}
	})

	// ---------- FUNC ----------
	check := func(t *testing.T, user, from, into string, steps []zgStep) (http.Handler, map[string]zgTask) {
		t.Helper()
		h := newServer()
		zgSetup(t, h, steps)
		before, _ := zgSnapLoose(h)
		var ids []string
		for id, tk := range before {
			if tk.ProjectID == from {
				ids = append(ids, id)
			}
		}
		sort.Strings(ids)
		if len(ids) == 0 {
			t.Fatalf("grader bug: source %s empty", from)
		}
		code, resp := zgDo(h, user, "POST", "/projects/"+from+"/merge", zgMergeBody(into))
		if code != 200 {
			t.Fatalf("status %d (want 200) body=%s", code, resp)
		}
		var got []zgTask
		if err := json.Unmarshal([]byte(resp), &got); err != nil {
			t.Fatalf("response is not a JSON array of tasks: %v body=%s", err, resp)
		}
		if len(got) != len(ids) {
			t.Fatalf("response has %d tasks, want %d: %s", len(got), len(ids), resp)
		}
		want := map[string]bool{}
		for _, id := range ids {
			want[id] = true
		}
		for _, g := range got {
			b, ok := before[g.ID]
			if !want[g.ID] || !ok {
				t.Errorf("response contains unexpected task %s", g.ID)
				continue
			}
			if g.ProjectID != into {
				t.Errorf("response task %s projectId=%q want %q", g.ID, g.ProjectID, into)
			}
			if g.Title != b.Title || g.Status != b.Status || g.Assignee != b.Assignee {
				t.Errorf("response task %s altered: %+v (was %+v)", g.ID, g, b)
			}
		}
		after, codes := zgSnapLoose(h)
		for id, b := range before {
			if want[id] {
				b.ProjectID = into
			}
			if a := after[id]; a != b {
				t.Errorf("state of %s: got %+v want %+v", id, a, b)
			}
		}
		if len(after) != len(before) {
			t.Errorf("task count changed %d -> %d", len(before), len(after))
		}
		// source (if still listable) must hold no tasks; target lists every moved task
		if codes[from] == 200 {
			for _, a := range after {
				if a.ProjectID == from {
					t.Errorf("source still holds %s", a.ID)
				}
			}
		}
		return h, after
	}
	for _, c := range []struct {
		n, user, from, into string
		steps               []zgStep
	}{
		{"IvyOpsToWeb", "ivy", "ops", "web", nil},
		{"IvyOpsToWebAssigneeIvy", "ivy", "ops", "web", []zgStep{zgAssign("ivy", "ops", "t4", "ivy")}},
		{"IvyOpsToWebUnassigned", "ivy", "ops", "web", []zgStep{zgAssign("ivy", "ops", "t4", "")}},
		{"IvyOpsToWebMoreTasks", "ivy", "ops", "web", []zgStep{
			zgCreate("ivy", "ops", "extra1", "ivy"), zgCreate("ivy", "ops", "extra2", ""), zgCreate("cat", "ops", "extra3", "cat")}},
		{"IvyWebToOpsT1Unassigned", "ivy", "web", "ops", []zgStep{zgAssign("ivy", "web", "t1", "")}},
		{"IvyWebToOpsT1ToCat", "ivy", "web", "ops", []zgStep{zgAssign("ann", "web", "t1", "cat")}},
		{"IvyWebToOpsT1ToIvy", "ivy", "web", "ops", []zgStep{zgAssign("ann", "web", "t1", "ivy")}},
		{"IvyWebToOpsAllUnassigned", "ivy", "web", "ops", []zgStep{
			zgAssign("ivy", "web", "t1", ""), zgAssign("ivy", "web", "t2", "")}},
		{"IvyWebToOpsWithStatusDone", "ivy", "web", "ops", []zgStep{
			zgAssign("ivy", "web", "t1", ""), {"cat", "PATCH", "/projects/web/tasks/t2", `{"status":"done"}`}}},
	} {
		c := c
		t.Run("FUNC_"+c.n, func(t *testing.T) { check(t, c.user, c.from, c.into, c.steps) })
	}
	t.Run("FUNC_MovedTasksReadableInTarget", func(t *testing.T) {
		h, _ := check(t, "ivy", "ops", "web", nil)
		code, body := zgDo(h, "dan", "GET", "/projects/web/tasks/t4", "")
		var tk zgTask
		json.Unmarshal([]byte(body), &tk)
		if code != 200 || tk.ProjectID != "web" || tk.Title != "Rotate keys" {
			t.Fatalf("code=%d task=%+v", code, tk)
		}
		code, _ = zgDo(h, "ivy", "GET", "/projects/ops/tasks/t4", "")
		if code != 404 {
			t.Errorf("old path still serves moved task: %d", code)
		}
	})
	t.Run("FUNC_MovedTaskAuthFollowsNewProject", func(t *testing.T) {
		h, _ := check(t, "ivy", "ops", "web", nil)
		c1, _ := zgDo(h, "cat", "PATCH", "/projects/web/tasks/t4", `{"status":"done"}`)
		c2, _ := zgDo(h, "ben", "PATCH", "/projects/web/tasks/t4", `{"status":"done"}`)
		if c1 != 200 || c2 != 403 {
			t.Fatalf("cat own=%d (want 200) ben other=%d (want 403)", c1, c2)
		}
	})
	t.Run("FUNC_MergedTargetKeepsOwnTasks", func(t *testing.T) {
		h, after := check(t, "ivy", "ops", "web", nil)
		for _, id := range []string{"t1", "t2", "t3"} {
			if after[id].ProjectID != "web" {
				t.Errorf("%s moved unexpectedly: %+v", id, after[id])
			}
		}
		_, body := zgDo(h, "ann", "GET", "/projects/web/tasks", "")
		var ts []zgTask
		json.Unmarshal(body2b(body), &ts)
		if len(ts) != 4 {
			t.Errorf("web lists %d tasks, want 4", len(ts))
		}
	})

	// ---------- AUTHZ / ATOM / CONTRACT ----------
	clean := []zgStep{zgAssign("ann", "web", "t1", ""), zgAssign("ann", "web", "t2", "")}
	cases := []zgCase{
		// source side
		{"ContributorCatOthersTasks", "cat", "web", "ops", nil, 403, false},
		{"ContributorBenWebToLegacy", "ben", "web", "legacy", nil, 0, false},
		{"ContributorCatWebCleanToOps", "cat", "web", "ops", []zgStep{zgAssign("ann", "web", "t1", "")}, 403, false},
		{"ViewerSourceAnnOps", "ann", "ops", "web", nil, 403, false},
		{"ViewerDanWeb", "dan", "web", "ops", nil, 0, false},
		{"OrgAdminWebToOps", "eve", "web", "ops", nil, 403, false},
		{"OrgAdminOpsToWeb", "eve", "ops", "web", nil, 403, false},
		{"NonMemberWebToOps", "hal", "web", "ops", nil, 404, false},
		{"NonMemberOpsToWeb", "hal", "ops", "web", nil, 404, false},
		{"OtherOrgUserWebToOps", "gus", "web", "ops", nil, 404, false},
		{"OtherOrgUserOwnToOps", "gus", "gx", "ops", nil, 404, false},
		{"OtherOrgUserOwnToWeb", "gus", "gx", "web", nil, 404, false},
		{"AnnIntoOtherOrg", "ann", "web", "gx", nil, 404, false},
		{"IvyIntoOtherOrg", "ivy", "ops", "gx", nil, 404, false},
		{"AnnFromOtherOrg", "ann", "gx", "web", nil, 404, false},
		{"Anonymous", "", "web", "ops", nil, 401, false},
		{"UnknownUser", "zed", "web", "ops", nil, 401, false},
		{"NoSuchSource", "ann", "nope", "ops", nil, 404, false},
		{"ArchivedSource", "ann", "legacy", "web", nil, 409, false},
		{"ArchivedSourceToOps", "ann", "legacy", "ops", nil, 0, false},
		{"ArchivedSourceContributor", "ben", "legacy", "web", nil, 0, false},
		// destination side
		{"ArchivedTargetClean", "ann", "web", "legacy", clean, 409, false},
		{"ArchivedTargetRaw", "ann", "web", "legacy", nil, 0, false},
		{"ArchivedTargetNonMember", "ivy", "web", "legacy", nil, 0, false},
		{"ViewerTargetClean", "ann", "web", "ops", []zgStep{zgAssign("ann", "web", "t1", "")}, 403, false},
		{"ViewerTargetAllUnassigned", "ann", "web", "ops", clean, 403, false},
		{"ViewerTargetRaw", "ann", "web", "ops", nil, 0, false},
		{"UnknownTarget", "ann", "web", "nope", nil, 0, false},
		{"MissingTarget", "ann", "web", "", nil, 0, false},
		{"OpsToLegacyAsIvy", "ivy", "ops", "legacy", nil, 0, false},
		// assignee rule: every carried-over assignee must be a writer in the target
		{"AssigneeNotInTargetRaw", "ivy", "web", "ops", nil, 400, true},
		{"AssigneeNotInTargetAsAnn", "ivy", "web", "ops", []zgStep{zgAssign("ivy", "web", "t1", "cat"), zgAssign("ivy", "web", "t3", "ben")}, 400, true},
		{"AssigneeViewerInTargetFirst", "ivy", "web", "ops", []zgStep{zgAssign("ivy", "web", "t1", "ann")}, 400, true},
		{"AssigneeViewerInTargetLast", "ivy", "web", "ops", []zgStep{zgAssign("ivy", "web", "t1", ""), zgAssign("ivy", "web", "t3", "ann")}, 400, true},
		{"AssigneeBadOnlyNewTaskLast", "ivy", "web", "ops", []zgStep{zgAssign("ivy", "web", "t1", ""), zgCreate("ivy", "web", "zz-last", "ben")}, 400, true},
		{"AssigneeBadOnlyMiddle", "ivy", "web", "ops", []zgStep{zgAssign("ivy", "web", "t1", ""), zgAssign("ivy", "web", "t2", "ben")}, 400, true},
		{"AssigneeBadAllButOne", "ivy", "web", "ops", []zgStep{zgAssign("ivy", "web", "t1", "ben"), zgAssign("ivy", "web", "t2", "ann"), zgAssign("ivy", "web", "t3", "")}, 400, true},
	}
	for _, c := range cases {
		c := c
		cat := "AUTHZ_"
		if c.atomic {
			cat = "ATOM_"
		}
		t.Run(cat+c.name, func(t *testing.T) { zgDenied(t, c.user, c.from, c.into, c.steps) })
		if c.want != 0 {
			t.Run("CONTRACT_"+c.name, func(t *testing.T) {
				h := newServer()
				zgSetup(t, h, c.steps)
				code, body := zgDo(h, c.user, "POST", "/projects/"+c.from+"/merge", zgMergeBody(c.into))
				if code != c.want {
					t.Fatalf("got %d want %d body=%s", code, c.want, body)
				}
			})
		}
	}

	// ---------- VALID ----------
	vcases := []struct{ name, body string }{
		{"MalformedJSON", `{"intoProjectId":`},
		{"NotJSON", `intoProjectId=ops`},
		{"IntoWrongType", `{"intoProjectId":5}`},
		{"IntoArray", `{"intoProjectId":["ops"]}`},
	}
	for _, c := range vcases {
		c := c
		t.Run("VALID_"+c.name, func(t *testing.T) {
			h := newServer()
			before := zgSnap(t, h)
			code, body := zgDo(h, "ivy", "POST", "/projects/web/merge", c.body)
			if code != 400 {
				t.Errorf("got %d want 400 body=%s", code, body)
			}
			if d := zgDiff(before, zgSnap(t, h)); d != "" {
				t.Errorf("state changed: %s", d)
			}
		})
	}

	// ---------- INFO (never fails; ambiguous contract) ----------
	info := func(name, user, from, into string, steps []zgStep) {
		t.Run("INFO_"+name, func(t *testing.T) {
			h := newServer()
			for _, s := range steps {
				zgDo(h, s.user, s.method, s.path, s.body)
			}
			before, _ := zgSnapLoose(h)
			code, resp := zgDo(h, user, "POST", "/projects/"+from+"/merge", zgMergeBody(into))
			after, codes := zgSnapLoose(h)
			t.Logf("INFO %s: status=%d body=%s changed=%q listCodes=%v", name, code, strings.TrimSpace(resp), zgDiff(before, after), codes)
		})
	}
	info("SameProjectAnn", "ann", "web", "web", nil)
	info("SameProjectIvyOps", "ivy", "ops", "ops", nil)
	info("ContributorOwnTasksOnly", "cat", "ops", "web", nil)
	info("EmptySource", "ivy", "web", "ops", []zgStep{zgDelete("ann", "web", "t1"), zgDelete("ann", "web", "t2"), zgDelete("ann", "web", "t3")})
	info("RepeatMerge", "ivy", "ops", "web", nil)
	t.Run("INFO_SourceAfterMerge", func(t *testing.T) {
		h := newServer()
		code, _ := zgDo(h, "ivy", "POST", "/projects/ops/merge", zgMergeBody("web"))
		c2, b2 := zgDo(h, "ivy", "GET", "/projects/ops/tasks", "")
		c3, b3 := zgDo(h, "ivy", "POST", "/projects/ops/merge", zgMergeBody("web"))
		c4, _ := zgDo(h, "ivy", "POST", "/projects/ops/tasks", `{"title":"after","assignee":"ivy"}`)
		t.Logf("INFO SourceAfterMerge: merge=%d list=%d %s repeat=%d %s createInSource=%d", code, c2, strings.TrimSpace(b2), c3, strings.TrimSpace(b3), c4)
	})
	info("UnknownTargetLog", "ann", "web", "nope", nil)
	info("EmptyTargetLog", "ann", "web", "", nil)
}

func body2b(s string) []byte { return []byte(s) }
