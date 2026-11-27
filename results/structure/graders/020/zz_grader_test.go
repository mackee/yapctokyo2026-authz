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

// Hidden grader for task 020 (claim an unassigned task). Uses only newServer() and HTTP.

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

// zgNew creates an unassigned task in pid (as ivy, manager of web and ops) and returns its id.
func zgNew(t *testing.T, h http.Handler, pid, title string) string {
	t.Helper()
	code, body := zgDo(h, "ivy", "POST", "/projects/"+pid+"/tasks", fmt.Sprintf(`{"title":%q}`, title))
	var tk zgTask
	json.Unmarshal([]byte(body), &tk)
	if code != 201 || tk.ID == "" {
		t.Fatalf("grader setup: create failed %d %s", code, body)
	}
	if tk.Assignee != "" {
		t.Fatalf("grader setup: created task is assigned: %+v", tk)
	}
	return tk.ID
}

func zgClaim(h http.Handler, user, pid, tid string) (int, string) {
	return zgDo(h, user, "POST", "/projects/"+pid+"/tasks/"+tid+"/claim", "")
}

// zgDenied: claim must be refused with non-2xx, no state change, no leak.
// tid "NEW:web" / "NEW:ops" means a freshly created unassigned task in that project.
func zgDenied(t *testing.T, user, pid, tid string) int {
	t.Helper()
	h := newServer()
	if strings.HasPrefix(tid, "NEW:") {
		tid = zgNew(t, h, strings.TrimPrefix(tid, "NEW:"), "grader-new-task")
	}
	before := zgSnap(t, h)
	code, resp := zgClaim(h, user, pid, tid)
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
		{"SinglePatchManagerAssignUnassigned", "ann", "web", "t3", `{"assignee":"ben"}`, 200},
		{"SinglePatchContributorSelfAssign403", "cat", "web", "t3", `{"assignee":"cat"}`, 403},
		{"SinglePatchContributorReassign403", "ben", "web", "t1", `{"assignee":"cat"}`, 403},
		{"SinglePatchContributorOwnStatus", "ben", "web", "t1", `{"status":"done"}`, 200},
		{"SinglePatchContributorOthers403", "ben", "web", "t2", `{"status":"done"}`, 403},
		{"SinglePatchViewer403", "dan", "web", "t1", `{"status":"done"}`, 403},
		{"SinglePatchOrgAdmin403", "eve", "web", "t1", `{"assignee":"ben"}`, 403},
		{"SinglePatchBadAssignee400", "ann", "web", "t1", `{"assignee":"dan"}`, 400},
		{"SinglePatchArchived409", "ann", "legacy", "t5", `{"assignee":"ann"}`, 409},
		{"SinglePatchWrongProject404", "ann", "web", "t4", `{"assignee":"ann"}`, 404},
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
	t.Run("REG_CreateUnassignedTask", func(t *testing.T) {
		code, _ := zgDo(newServer(), "ann", "POST", "/projects/web/tasks", `{"title":"x"}`)
		if code != 201 {
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
	claimOK := func(t *testing.T, user, pid, tid string) {
		t.Helper()
		h := newServer()
		if strings.HasPrefix(tid, "NEW:") {
			tid = zgNew(t, h, strings.TrimPrefix(tid, "NEW:"), "grader-new-task")
		}
		before := zgSnap(t, h)
		code, resp := zgClaim(h, user, pid, tid)
		if code != 200 {
			t.Fatalf("status %d (want 200) body=%s", code, resp)
		}
		var got zgTask
		if err := json.Unmarshal([]byte(resp), &got); err != nil {
			t.Fatalf("response is not a JSON task: %v body=%s", err, resp)
		}
		b := before[tid]
		if got.ID != tid || got.Assignee != user {
			t.Errorf("response task %+v, want id=%s assignee=%s", got, tid, user)
		}
		if got.Title != b.Title || got.Status != b.Status || got.ProjectID != b.ProjectID {
			t.Errorf("response task has altered fields: %+v (before %+v)", got, b)
		}
		after := zgSnap(t, h)
		b.Assignee = user
		want := map[string]zgTask{}
		for id, x := range before {
			want[id] = x
		}
		want[tid] = b
		if d := zgDiff(want, after); d != "" {
			t.Errorf("state mismatch after claim: %s", d)
		}
		// readable via GET as a viewer
		_, gb := zgDo(h, "dan", "GET", "/projects/"+pid+"/tasks/"+tid, "")
		if pid == "ops" {
			_, gb = zgDo(h, "ivy", "GET", "/projects/"+pid+"/tasks/"+tid, "")
		}
		var tk zgTask
		json.Unmarshal([]byte(gb), &tk)
		if tk.Assignee != user {
			t.Errorf("GET after claim: assignee=%q want %q", tk.Assignee, user)
		}
	}
	for _, c := range []struct{ n, user, pid, tid string }{
		{"ContributorCatWebT3", "cat", "web", "t3"},
		{"ContributorBenWebT3", "ben", "web", "t3"},
		{"ManagerIvyWebT3", "ivy", "web", "t3"},
		{"ManagerAnnWebT3", "ann", "web", "t3"},
		{"ContributorBenNewWeb", "ben", "web", "NEW:web"},
		{"ContributorCatNewOps", "cat", "ops", "NEW:ops"},
		{"ManagerIvyNewOps", "ivy", "ops", "NEW:ops"},
	} {
		c := c
		t.Run("FUNC_"+c.n, func(t *testing.T) { claimOK(t, c.user, c.pid, c.tid) })
	}
	t.Run("FUNC_TitleStatusUntouched", func(t *testing.T) {
		h := newServer()
		zgClaim(h, "cat", "web", "t3")
		_, body := zgDo(h, "ann", "GET", "/projects/web/tasks/t3", "")
		var tk zgTask
		json.Unmarshal([]byte(body), &tk)
		if tk.Title != "Write copy" || tk.Status != "open" || tk.Assignee != "cat" {
			t.Fatalf("task=%+v", tk)
		}
	})
	t.Run("FUNC_ClaimerNowOwnsTask", func(t *testing.T) {
		h := newServer()
		code, _ := zgClaim(h, "cat", "web", "t3")
		if code != 200 {
			t.Fatalf("claim %d", code)
		}
		c1, _ := zgDo(h, "cat", "PATCH", "/projects/web/tasks/t3", `{"status":"doing"}`)
		c2, _ := zgDo(h, "ben", "PATCH", "/projects/web/tasks/t3", `{"status":"done"}`)
		if c1 != 200 || c2 != 403 {
			t.Fatalf("cat own=%d (want 200) ben other=%d (want 403)", c1, c2)
		}
	})
	t.Run("FUNC_OnlyOneWinnerOfTwoClaims", func(t *testing.T) {
		h := newServer()
		c1, _ := zgClaim(h, "cat", "web", "t3")
		zgClaim(h, "ben", "web", "t3")
		_, body := zgDo(h, "ann", "GET", "/projects/web/tasks/t3", "")
		var tk zgTask
		json.Unmarshal([]byte(body), &tk)
		if c1 != 200 || tk.Assignee != "cat" {
			t.Fatalf("first claim=%d final assignee=%q (want cat)", c1, tk.Assignee)
		}
	})
	t.Run("FUNC_UnassignThenClaim", func(t *testing.T) {
		h := newServer()
		c0, _ := zgDo(h, "ann", "PATCH", "/projects/web/tasks/t1", `{"assignee":""}`)
		if c0 != 200 {
			t.Fatalf("setup unassign %d", c0)
		}
		code, resp := zgClaim(h, "cat", "web", "t1")
		var tk zgTask
		json.Unmarshal([]byte(resp), &tk)
		if code != 200 || tk.Assignee != "cat" {
			t.Fatalf("code=%d body=%s", code, resp)
		}
	})

	// ---------- AUTHZ ----------
	type ac struct {
		name, user, pid, tid string
		want                 int // 0 = no CONTRACT test
	}
	for _, c := range []ac{
		// non-writers must not become assignees
		{"ViewerDanWebT3", "dan", "web", "t3", 403},
		{"ViewerDanNewWeb", "dan", "web", "NEW:web", 403},
		{"ViewerAnnOpsNew", "ann", "ops", "NEW:ops", 403},
		{"OrgAdminEveWebT3", "eve", "web", "t3", 403},
		{"OrgAdminEveOpsNew", "eve", "ops", "NEW:ops", 403},
		// steal protection
		{"StealFromBen", "cat", "web", "t1", 0},
		{"StealFromCat", "ben", "web", "t2", 0},
		{"ManagerStealsAssigned", "ann", "web", "t1", 0},
		{"ManagerIvyStealsAssigned", "ivy", "web", "t2", 0},
		{"ContributorStealsOps", "cat", "ops", "t4", 0},
		{"ViewerStealsAssigned", "dan", "web", "t1", 403},
		// membership / identity
		{"NonMemberHal", "hal", "web", "t3", 404},
		{"NonMemberBenOps", "ben", "ops", "NEW:ops", 404},
		{"OtherOrgGusWeb", "gus", "web", "t3", 404},
		{"AnnOnOtherOrgProject", "ann", "gx", "t6", 404},
		{"GusOnOwnProjectAssigned", "gus", "gx", "t6", 0},
		{"Anonymous", "", "web", "t3", 401},
		{"UnknownUser", "zed", "web", "t3", 401},
		{"NoSuchProject", "ann", "nope", "t3", 404},
		// task scoping
		{"TaskFromOtherProject", "cat", "web", "t4", 404},
		{"IvyOpsTaskViaWeb", "ivy", "web", "t4", 404},
		{"IvyWebTaskViaOps", "ivy", "ops", "t3", 404},
		{"TaskFromOtherOrg", "ann", "web", "t6", 404},
		{"NonexistentTask", "ann", "web", "t999", 404},
		// archived
		{"ArchivedManager", "ann", "legacy", "t5", 409},
		{"ArchivedContributorOwn", "ben", "legacy", "t5", 0},
		{"ArchivedNonMemberCat", "cat", "legacy", "t5", 404},
	} {
		c := c
		t.Run("AUTHZ_"+c.name, func(t *testing.T) { zgDenied(t, c.user, c.pid, c.tid) })
		if c.want != 0 {
			t.Run("CONTRACT_"+c.name, func(t *testing.T) {
				h := newServer()
				tid := c.tid
				if strings.HasPrefix(tid, "NEW:") {
					tid = zgNew(t, h, strings.TrimPrefix(tid, "NEW:"), "grader-new-task")
				}
				code, body := zgClaim(h, c.user, c.pid, tid)
				if code != c.want {
					t.Fatalf("got %d want %d body=%s", code, c.want, body)
				}
			})
		}
	}

	// ---------- VALID ----------
	t.Run("VALID_BodyIgnoredOrAccepted", func(t *testing.T) {
		// A claim carrying a body naming someone else must never assign to that someone else.
		h := newServer()
		code, _ := zgDo(h, "cat", "POST", "/projects/web/tasks/t3/claim", `{"assignee":"ben"}`)
		_, b := zgDo(h, "ann", "GET", "/projects/web/tasks/t3", "")
		var tk zgTask
		json.Unmarshal([]byte(b), &tk)
		if tk.Assignee == "ben" {
			t.Fatalf("claim by cat with body assignee=ben assigned to ben (status %d)", code)
		}
	})
	t.Run("VALID_GetNotAllowedChangesNothing", func(t *testing.T) {
		h := newServer()
		before := zgSnap(t, h)
		code, _ := zgDo(h, "cat", "GET", "/projects/web/tasks/t3/claim", "")
		if code >= 200 && code < 300 {
			t.Errorf("GET claim returned %d", code)
		}
		if d := zgDiff(before, zgSnap(t, h)); d != "" {
			t.Errorf("state changed: %s", d)
		}
	})

	// ---------- INFO ----------
	info := func(name, user, pid, tid string) {
		t.Run("INFO_"+name, func(t *testing.T) {
			h := newServer()
			before := zgSnap(t, h)
			code, resp := zgClaim(h, user, pid, tid)
			t.Logf("INFO %s: status=%d body=%s changed=%q", name, code, strings.TrimSpace(resp), zgDiff(before, zgSnap(t, h)))
		})
	}
	info("ReclaimOwnTaskBen", "ben", "web", "t1")
	info("ReclaimOwnTaskCat", "cat", "web", "t2")
	info("StealFromBenStatus", "cat", "web", "t1")
	info("ManagerStealStatus", "ann", "web", "t1")
	info("ArchivedAssignedManager", "ann", "legacy", "t5")
	info("ViewerClaimsAssignedStatus", "dan", "web", "t1")
	t.Run("INFO_DoubleClaimSameUser", func(t *testing.T) {
		h := newServer()
		c1, _ := zgClaim(h, "cat", "web", "t3")
		c2, r2 := zgClaim(h, "cat", "web", "t3")
		t.Logf("INFO DoubleClaimSameUser: first=%d second=%d body=%s", c1, c2, strings.TrimSpace(r2))
	})
}
