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

// Hidden grader for task 017 (handover). Uses only newServer() and HTTP.

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

func zgTo(to string) string {
	b, _ := json.Marshal(map[string]any{"to": to})
	return string(b)
}

func zgPath(pid, uid string) string { return "/projects/" + pid + "/members/" + uid + "/handover" }

func zgSnap(t *testing.T, h http.Handler) map[string]zgTask {
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

// zgSetup adds extra tasks so a handover moves several: two more for ben in
// web and one more for cat in ops (via the existing create endpoint).
func zgSetup(t *testing.T, h http.Handler) {
	t.Helper()
	for i, c := range []struct{ user, pid, as string }{
		{"ann", "web", "ben"}, {"ann", "web", "ben"}, {"ivy", "ops", "cat"}, {"ann", "web", "cat"},
	} {
		code, body := zgDo(h, c.user, "POST", "/projects/"+c.pid+"/tasks", fmt.Sprintf(`{"title":"extra%d","assignee":%q}`, i, c.as))
		if code != 201 {
			t.Fatalf("grader setup create failed: %d %s", code, body)
		}
	}
}

func zgDenied(t *testing.T, user, pid, uid, body string) int {
	t.Helper()
	h := newServer()
	zgSetup(t, h)
	before := zgSnap(t, h)
	code, resp := zgDo(h, user, "POST", zgPath(pid, uid), body)
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

func zgCall(h http.Handler, user, pid, uid, body string) (int, string) {
	return zgDo(h, user, "POST", zgPath(pid, uid), body)
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
		{"ListIvyOps", "ivy", "ops", 200},
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
		{"SinglePatchManagerUnassign", "ann", "web", "t1", `{"assignee":""}`, 200},
		{"SinglePatchContributorReassign403", "ben", "web", "t1", `{"assignee":"cat"}`, 403},
		{"SinglePatchContributorOwnStatus", "ben", "web", "t1", `{"status":"done"}`, 200},
		{"SinglePatchContributorOthers403", "ben", "web", "t2", `{"status":"done"}`, 403},
		{"SinglePatchViewer403", "dan", "web", "t1", `{"status":"done"}`, 403},
		{"SinglePatchOrgAdmin403", "eve", "web", "t1", `{"assignee":"ben"}`, 403},
		{"SinglePatchBadAssignee400", "ann", "web", "t1", `{"assignee":"dan"}`, 400},
		{"SinglePatchArchived409", "ann", "legacy", "t5", `{"assignee":"ann"}`, 409},
		{"SinglePatchNonMember404", "hal", "web", "t1", `{"status":"done"}`, 404},
		{"SinglePatchAnon401", "", "web", "t1", `{"status":"done"}`, 401},
		{"DeleteContributor403", "ben", "web", "t1", "", 403},
		{"DeleteManager204", "ann", "web", "t3", "", 204},
	} {
		c := c
		t.Run("REG_"+c.n, func(t *testing.T) {
			m := "PATCH"
			if c.body == "" {
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
	t.Run("REG_CreateTaskBadAssignee400", func(t *testing.T) {
		code, _ := zgDo(newServer(), "ann", "POST", "/projects/web/tasks", `{"title":"x","assignee":"dan"}`)
		if code != 400 {
			t.Fatalf("got %d", code)
		}
	})
	t.Run("REG_GetTask", func(t *testing.T) {
		code, _ := zgDo(newServer(), "ben", "GET", "/projects/web/tasks/t1", "")
		if code != 200 {
			t.Fatalf("got %d", code)
		}
	})

	// ---------- FUNC ----------
	// check: perform a handover (with extra tasks present) and verify the
	// response array and the whole store.
	check := func(t *testing.T, user, pid, uid, to string) {
		t.Helper()
		h := newServer()
		zgSetup(t, h)
		before := zgSnap(t, h)
		want := map[string]bool{}
		for id, tk := range before {
			if tk.ProjectID == pid && tk.Assignee == uid {
				want[id] = true
			}
		}
		if len(want) == 0 {
			t.Fatalf("grader bug: no tasks to move")
		}
		code, resp := zgCall(h, user, pid, uid, zgTo(to))
		if code != 200 {
			t.Fatalf("status %d (want 200) body=%s", code, resp)
		}
		var got []zgTask
		if err := json.Unmarshal([]byte(resp), &got); err != nil {
			t.Fatalf("response is not a JSON array of tasks: %v body=%s", err, resp)
		}
		if len(got) != len(want) {
			t.Fatalf("response has %d tasks, want %d: %s", len(got), len(want), resp)
		}
		seen := map[string]bool{}
		for _, g := range got {
			if !want[g.ID] {
				t.Errorf("response contains task %s that was not %s's in %s", g.ID, uid, pid)
			}
			if seen[g.ID] {
				t.Errorf("response repeats task %s", g.ID)
			}
			seen[g.ID] = true
			if g.Assignee != to {
				t.Errorf("response task %s assignee=%q want %q", g.ID, g.Assignee, to)
			}
			if g.Title != before[g.ID].Title || g.Status != before[g.ID].Status || g.ProjectID != before[g.ID].ProjectID {
				t.Errorf("response task %s has altered fields: %+v", g.ID, g)
			}
		}
		after := zgSnap(t, h)
		for id, b := range before {
			if want[id] {
				b.Assignee = to
			}
			if a := after[id]; a != b {
				t.Errorf("state of %s: got %+v want %+v", id, a, b)
			}
		}
		if len(after) != len(before) {
			t.Errorf("task count changed: %d -> %d", len(before), len(after))
		}
	}
	for _, c := range []struct{ n, user, pid, uid, to string }{
		{"IvyBenToCat", "ivy", "web", "ben", "cat"},
		{"AnnBenToCat", "ann", "web", "ben", "cat"},
		{"AnnCatToBen", "ann", "web", "cat", "ben"},
		{"AnnBenToAnn", "ann", "web", "ben", "ann"},
		{"AnnBenToIvy", "ann", "web", "ben", "ivy"},
		{"AnnOnlyWebCatToIvy", "ann", "web", "cat", "ivy"},
		{"IvyCatWebToBen", "ivy", "web", "cat", "ben"},
		{"IvyOpsCatToIvy", "ivy", "ops", "cat", "ivy"},
	} {
		c := c
		t.Run("FUNC_"+c.n, func(t *testing.T) { check(t, c.user, c.pid, c.uid, c.to) })
	}
	t.Run("FUNC_ScopeOnlyPathProject", func(t *testing.T) {
		// cat has tasks in web and ops; handing over cat's web tasks must leave ops untouched
		h := newServer()
		zgSetup(t, h)
		code, resp := zgCall(h, "ivy", "web", "cat", zgTo("ben"))
		if code != 200 {
			t.Fatalf("status %d body=%s", code, resp)
		}
		_, b := zgDo(h, "ivy", "GET", "/projects/ops/tasks/t4", "")
		var tk zgTask
		json.Unmarshal([]byte(b), &tk)
		if tk.Assignee != "cat" {
			t.Errorf("ops task t4 assignee=%q want cat", tk.Assignee)
		}
		code, resp = zgCall(h, "ivy", "ops", "cat", zgTo("ivy"))
		if code != 200 {
			t.Fatalf("ops handover status %d body=%s", code, resp)
		}
		_, b = zgDo(h, "ivy", "GET", "/projects/web/tasks/t2", "")
		json.Unmarshal([]byte(b), &tk)
		if tk.Assignee != "ben" {
			t.Errorf("web task t2 assignee=%q want ben (moved by first call, untouched by second)", tk.Assignee)
		}
	})
	t.Run("FUNC_ArchivedTaskOfSameUserUntouched", func(t *testing.T) {
		h := newServer()
		code, resp := zgCall(h, "ann", "web", "ben", zgTo("cat"))
		if code != 200 {
			t.Fatalf("status %d body=%s", code, resp)
		}
		_, b := zgDo(h, "ann", "GET", "/projects/legacy/tasks/t5", "")
		var tk zgTask
		json.Unmarshal([]byte(b), &tk)
		if tk.Assignee != "ben" {
			t.Errorf("legacy t5 assignee=%q want ben", tk.Assignee)
		}
		_, b = zgDo(h, "ann", "GET", "/projects/web/tasks/t1", "")
		json.Unmarshal([]byte(b), &tk)
		if tk.Assignee != "cat" {
			t.Errorf("web t1 assignee=%q want cat", tk.Assignee)
		}
	})
	t.Run("FUNC_NewAssigneeCanEdit", func(t *testing.T) {
		h := newServer()
		code, _ := zgCall(h, "ivy", "web", "ben", zgTo("cat"))
		if code != 200 {
			t.Fatalf("handover %d", code)
		}
		c2, _ := zgDo(h, "cat", "PATCH", "/projects/web/tasks/t1", `{"status":"done"}`)
		c3, _ := zgDo(h, "ben", "PATCH", "/projects/web/tasks/t1", `{"status":"done"}`)
		if c2 != 200 || c3 != 403 {
			t.Fatalf("cat new owner=%d (want 200) ben former=%d (want 403)", c2, c3)
		}
	})
	t.Run("FUNC_EmptiedUserListShowsNoTasks", func(t *testing.T) {
		h := newServer()
		zgSetup(t, h)
		code, _ := zgCall(h, "ivy", "web", "ben", zgTo("cat"))
		if code != 200 {
			t.Fatalf("handover %d", code)
		}
		_, b := zgDo(h, "dan", "GET", "/projects/web/tasks", "")
		var ts []zgTask
		json.Unmarshal([]byte(b), &ts)
		for _, x := range ts {
			if x.Assignee == "ben" {
				t.Errorf("task %s still assigned to ben", x.ID)
			}
		}
	})
	t.Run("FUNC_SecondHandoverMovesNothingMore", func(t *testing.T) {
		h := newServer()
		zgCall(h, "ivy", "web", "ben", zgTo("cat"))
		code, resp := zgCall(h, "ivy", "web", "ben", zgTo("ann"))
		if code != 200 || strings.TrimSpace(resp) != "[]" {
			t.Fatalf("second handover: %d %s (want 200 [])", code, resp)
		}
	})

	// ---------- AUTHZ / ATOM / CONTRACT ----------
	type zc struct {
		name, user, pid, uid, body string
		want                       int // 0 = ambiguous, no CONTRACT test
		atomic                     bool
	}
	cases := []zc{
		// contributors may not reassign (even their own tasks)
		{"ContributorBenOwnToCat", "ben", "web", "ben", zgTo("cat"), 403, false},
		{"ContributorBenOwnToSelf", "ben", "web", "ben", zgTo("ben"), 0, false},
		{"ContributorBenOthersToSelf", "ben", "web", "cat", zgTo("ben"), 403, false},
		{"ContributorCatOwnToBen", "cat", "web", "cat", zgTo("ben"), 403, false},
		{"ContributorCatTakesBens", "cat", "web", "ben", zgTo("cat"), 403, false},
		{"ContributorCatOpsOwn", "cat", "ops", "cat", zgTo("ivy"), 403, false},
		{"ContributorOwnToUnassigned", "ben", "web", "ben", zgTo(""), 0, false},
		{"ContributorInvalidTo", "ben", "web", "ben", zgTo("dan"), 0, false},
		// other roles
		{"ViewerDan", "dan", "web", "ben", zgTo("cat"), 403, false},
		{"ViewerDanToSelfAttempt", "dan", "web", "ben", zgTo("dan"), 0, false},
		{"AnnOpsViewer", "ann", "ops", "cat", zgTo("ivy"), 403, false},
		{"OrgAdmin", "eve", "web", "ben", zgTo("cat"), 403, false},
		{"OrgAdminCatToBen", "eve", "web", "cat", zgTo("ben"), 403, false},
		{"NonMember", "hal", "web", "ben", zgTo("cat"), 404, false},
		{"OtherOrgUser", "gus", "web", "ben", zgTo("gus"), 404, false},
		{"AnnOnOtherOrgProject", "ann", "gx", "gus", zgTo("ann"), 404, false},
		{"Anonymous", "", "web", "ben", zgTo("cat"), 401, false},
		{"UnknownUser", "zed", "web", "ben", zgTo("cat"), 401, false},
		{"NoSuchProject", "ann", "nope", "ben", zgTo("cat"), 404, false},
		{"ArchivedManager", "ann", "legacy", "ben", zgTo("ann"), 409, false},
		{"ArchivedManagerBenToBen", "ann", "legacy", "ben", zgTo("ben"), 409, false},
		// "to" validation: must be contributor or manager of THIS project
		{"ToViewer", "ivy", "web", "ben", zgTo("dan"), 400, true},
		{"ToViewerAnn", "ann", "web", "cat", zgTo("dan"), 400, true},
		{"ToNonMember", "ivy", "web", "ben", zgTo("hal"), 400, true},
		{"ToOtherOrg", "ivy", "web", "ben", zgTo("gus"), 400, true},
		{"ToOrgAdmin", "ann", "web", "ben", zgTo("eve"), 400, true},
		{"ToUnknownUser", "ann", "web", "ben", zgTo("zed"), 400, true},
		{"ToMemberOfOtherProjectOnly", "ivy", "ops", "cat", zgTo("ben"), 400, true},
		{"OpsToAnnOpsViewer", "ivy", "ops", "cat", zgTo("ann"), 400, true},
		{"OpsToDanNoRole", "ivy", "ops", "cat", zgTo("dan"), 400, true},
		{"WebCatToGus", "ann", "web", "cat", zgTo("gus"), 400, true},
	}
	for _, c := range cases {
		c := c
		cat := "AUTHZ_"
		if c.atomic {
			cat = "ATOM_"
		}
		t.Run(cat+c.name, func(t *testing.T) { zgDenied(t, c.user, c.pid, c.uid, c.body) })
		if c.want != 0 {
			t.Run("CONTRACT_"+c.name, func(t *testing.T) {
				code, body := zgCall(newServer(), c.user, c.pid, c.uid, c.body)
				if code != c.want {
					t.Fatalf("got %d want %d body=%s", code, c.want, body)
				}
			})
		}
	}

	// ---------- VALID ----------
	vcases := []struct{ name, body string }{
		{"MalformedJSON", `{"to":`},
		{"NotJSON", `to=cat`},
		{"ToWrongType", `{"to":5}`},
		{"ToIsArray", `{"to":["cat"]}`},
	}
	for _, c := range vcases {
		c := c
		t.Run("VALID_"+c.name, func(t *testing.T) {
			h := newServer()
			zgSetup(t, h)
			before := zgSnap(t, h)
			code, body := zgCall(h, "ann", "web", "ben", c.body)
			if code != 400 {
				t.Errorf("got %d want 400 body=%s", code, body)
			}
			if d := zgDiff(before, zgSnap(t, h)); d != "" {
				t.Errorf("state changed: %s", d)
			}
		})
	}

	// ---------- INFO (never fails; ambiguous contract) ----------
	info := func(name, user, pid, uid, body string) {
		t.Run("INFO_"+name, func(t *testing.T) {
			h := newServer()
			zgSetup(t, h)
			before := zgSnap(t, h)
			code, resp := zgCall(h, user, pid, uid, body)
			t.Logf("INFO %s: status=%d body=%s changed=%q", name, code, strings.TrimSpace(resp), zgDiff(before, zgSnap(t, h)))
		})
	}
	info("ToSameAsFrom", "ann", "web", "ben", zgTo("ben"))
	info("ToEmpty", "ann", "web", "ben", zgTo(""))
	info("ToMissing", "ann", "web", "ben", `{}`)
	info("ToNull", "ann", "web", "ben", `{"to":null}`)
	info("EmptyBody", "ann", "web", "ben", "")
	info("UidWithNoTasks", "ann", "web", "ann", zgTo("cat"))
	info("UidWithNoTasksDan", "ann", "web", "dan", zgTo("cat"))
	info("UidNotMember", "ann", "web", "hal", zgTo("cat"))
	info("UidUnknown", "ann", "web", "zed", zgTo("cat"))
	info("UidOtherOrg", "ann", "web", "gus", zgTo("cat"))
	info("UidNotMemberInvalidTo", "ann", "web", "hal", zgTo("dan"))
	info("ContributorInvalidToPrecedence", "ben", "web", "ben", zgTo("dan"))
	info("ArchivedInvalidTo", "ann", "legacy", "ben", zgTo("dan"))
	info("OrgAdminInvalidTo", "eve", "web", "ben", zgTo("dan"))
}
