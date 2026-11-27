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

// Hidden grader for task 007 (bulk reassign). Uses only newServer() and HTTP.

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

func zgBulk(assignee string, ids ...string) string {
	b, _ := json.Marshal(map[string]any{"taskIds": ids, "assignee": assignee})
	return string(b)
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

func zgDenied(t *testing.T, user, pid, body string) int {
	t.Helper()
	h := newServer()
	before := zgSnap(t, h)
	code, resp := zgDo(h, user, "PATCH", "/projects/"+pid+"/tasks/assignee", body)
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
	pid    string
	body   string
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
		{"SinglePatchContributorSelfAssign403", "cat", "web", "t2", `{"assignee":"cat"}`, 403},
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
	t.Run("REG_GetTask", func(t *testing.T) {
		code, _ := zgDo(newServer(), "ben", "GET", "/projects/web/tasks/t1", "")
		if code != 200 {
			t.Fatalf("got %d", code)
		}
	})

	// ---------- FUNC ----------
	check := func(t *testing.T, user, pid, assignee string, ids []string) {
		t.Helper()
		h := newServer()
		before := zgSnap(t, h)
		code, resp := zgDo(h, user, "PATCH", "/projects/"+pid+"/tasks/assignee", zgBulk(assignee, ids...))
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
		after := zgSnap(t, h)
		want := map[string]bool{}
		for _, id := range ids {
			want[id] = true
		}
		for _, g := range got {
			if !want[g.ID] {
				t.Errorf("response contains unrequested task %s", g.ID)
			}
			if g.Assignee != assignee {
				t.Errorf("response task %s assignee=%q want %q", g.ID, g.Assignee, assignee)
			}
			if g.Title != before[g.ID].Title || g.Status != before[g.ID].Status || g.ProjectID != before[g.ID].ProjectID {
				t.Errorf("response task %s has altered fields: %+v", g.ID, g)
			}
		}
		for id, b := range before {
			if want[id] {
				b.Assignee = assignee
			}
			if a := after[id]; a != b {
				t.Errorf("state of %s: got %+v want %+v", id, a, b)
			}
		}
	}
	for _, c := range []struct {
		n, user, pid, assignee string
		ids                    []string
	}{
		{"ManagerAnnTwoToCat", "ann", "web", "cat", []string{"t1", "t2"}},
		{"ManagerAnnAllToBen", "ann", "web", "ben", []string{"t1", "t2", "t3"}},
		{"ManagerAnnSingle", "ann", "web", "cat", []string{"t3"}},
		{"ManagerAnnToSelf", "ann", "web", "ann", []string{"t1", "t3"}},
		{"ManagerAnnToIvy", "ann", "web", "ivy", []string{"t2"}},
		{"ManagerAnnUnassign", "ann", "web", "", []string{"t1", "t2"}},
		{"ManagerAnnUnassignAlreadyEmpty", "ann", "web", "", []string{"t3"}},
		{"ManagerAnnSameAssignee", "ann", "web", "ben", []string{"t1"}},
		{"ManagerIvyWeb", "ivy", "web", "cat", []string{"t1", "t2", "t3"}},
		{"ManagerIvyToAnn", "ivy", "web", "ann", []string{"t1"}},
		{"ManagerIvyOpsToIvy", "ivy", "ops", "ivy", []string{"t4"}},
		{"ManagerIvyOpsUnassign", "ivy", "ops", "", []string{"t4"}},
		{"ManagerIvyOpsToCat", "ivy", "ops", "cat", []string{"t4"}},
		{"ManagerGusOtherOrg", "gus", "gx", "gus", []string{"t6"}},
	} {
		c := c
		t.Run("FUNC_"+c.n, func(t *testing.T) { check(t, c.user, c.pid, c.assignee, c.ids) })
	}
	t.Run("FUNC_NewlyCreatedTasks", func(t *testing.T) {
		h := newServer()
		var ids []string
		for i := 0; i < 2; i++ {
			code, body := zgDo(h, "ann", "POST", "/projects/web/tasks", fmt.Sprintf(`{"title":"n%d"}`, i))
			var tk zgTask
			json.Unmarshal([]byte(body), &tk)
			if code != 201 || tk.ID == "" {
				t.Fatalf("setup create failed %d %s", code, body)
			}
			ids = append(ids, tk.ID)
		}
		code, resp := zgDo(h, "ivy", "PATCH", "/projects/web/tasks/assignee", zgBulk("ben", ids...))
		if code != 200 {
			t.Fatalf("status %d body=%s", code, resp)
		}
		for _, id := range ids {
			_, b := zgDo(h, "dan", "GET", "/projects/web/tasks/"+id, "")
			var tk zgTask
			json.Unmarshal([]byte(b), &tk)
			if tk.Assignee != "ben" {
				t.Errorf("task %s assignee=%q", id, tk.Assignee)
			}
		}
	})
	t.Run("FUNC_ReadBackViaGet", func(t *testing.T) {
		h := newServer()
		zgDo(h, "ann", "PATCH", "/projects/web/tasks/assignee", zgBulk("cat", "t1", "t3"))
		code, body := zgDo(h, "dan", "GET", "/projects/web/tasks/t3", "")
		var tk zgTask
		json.Unmarshal([]byte(body), &tk)
		if code != 200 || tk.Assignee != "cat" {
			t.Fatalf("code=%d task=%+v", code, tk)
		}
	})
	t.Run("FUNC_ReassignedContributorNowOwnsTask", func(t *testing.T) {
		h := newServer()
		code, _ := zgDo(h, "ann", "PATCH", "/projects/web/tasks/assignee", zgBulk("cat", "t1"))
		if code != 200 {
			t.Fatalf("bulk %d", code)
		}
		c2, _ := zgDo(h, "cat", "PATCH", "/projects/web/tasks/t1", `{"status":"done"}`)
		c3, _ := zgDo(h, "ben", "PATCH", "/projects/web/tasks/t1", `{"status":"done"}`)
		if c2 != 200 || c3 != 403 {
			t.Fatalf("cat own=%d (want 200) ben former=%d (want 403)", c2, c3)
		}
	})
	t.Run("FUNC_TitleStatusUntouched", func(t *testing.T) {
		h := newServer()
		zgDo(h, "ann", "PATCH", "/projects/web/tasks/assignee", zgBulk("ben", "t2"))
		_, body := zgDo(h, "ann", "GET", "/projects/web/tasks/t2", "")
		var tk zgTask
		json.Unmarshal([]byte(body), &tk)
		if tk.Title != "Update footer" || tk.Status != "doing" || tk.Assignee != "ben" {
			t.Fatalf("task=%+v", tk)
		}
	})

	// ---------- AUTHZ / ATOM / CONTRACT ----------
	cases := []zgCase{
		// contributors may not reassign: the central case
		{"ContributorBenOwnToSelf", "ben", "web", zgBulk("ben", "t1"), 403, false},
		{"ContributorBenOwnToCat", "ben", "web", zgBulk("cat", "t1"), 403, false},
		{"ContributorBenOwnUnassign", "ben", "web", zgBulk("", "t1"), 403, false},
		{"ContributorCatOwnToBen", "cat", "web", zgBulk("ben", "t2"), 403, false},
		{"ContributorCatOwnToSelf", "cat", "web", zgBulk("cat", "t2"), 403, false},
		{"ContributorTakesUnassigned", "ben", "web", zgBulk("ben", "t3"), 403, false},
		{"ContributorTakesOthers", "ben", "web", zgBulk("ben", "t2"), 403, false},
		{"ContributorCatOps", "cat", "ops", zgBulk("ivy", "t4"), 403, false},
		{"ContributorCatOpsToSelf", "cat", "ops", zgBulk("cat", "t4"), 403, false},
		{"ContributorOwnTasksBatch", "ben", "web", zgBulk("cat", "t1"), 403, false},
		{"ContributorInvalidAssignee", "ben", "web", zgBulk("dan", "t1"), 0, false},
		{"ContributorOnArchivedOwn", "ben", "legacy", zgBulk("ben", "t5"), 0, false},
		// other roles
		{"ViewerWeb", "dan", "web", zgBulk("ben", "t1"), 403, false},
		{"ViewerWebSelf", "dan", "web", zgBulk("dan", "t3"), 403, false},
		{"ViewerOpsAsAnn", "ann", "ops", zgBulk("cat", "t4"), 403, false},
		{"OrgAdmin", "eve", "web", zgBulk("ben", "t1", "t2"), 403, false},
		{"OrgAdminToSelf", "eve", "web", zgBulk("eve", "t3"), 403, false},
		{"NonMember", "hal", "web", zgBulk("ben", "t1"), 404, false},
		{"NonMemberToSelf", "hal", "web", zgBulk("hal", "t3"), 404, false},
		{"OtherOrgUser", "gus", "web", zgBulk("gus", "t1"), 404, false},
		{"AnnOnOtherOrgProject", "ann", "gx", zgBulk("ann", "t6"), 404, false},
		{"Anonymous", "", "web", zgBulk("ben", "t1"), 401, false},
		{"UnknownUser", "zed", "web", zgBulk("ben", "t1"), 401, false},
		{"NoSuchProject", "ann", "nope", zgBulk("ben", "t1"), 404, false},
		// task scoping
		{"TaskFromOtherProjectOnly", "ann", "web", zgBulk("ben", "t4"), 404, false},
		{"TaskFromOtherOrgOnly", "ann", "web", zgBulk("ben", "t6"), 404, false},
		{"NonexistentIDOnly", "ann", "web", zgBulk("ben", "t999"), 404, false},
		{"IvyOpsTaskViaWeb", "ivy", "web", zgBulk("ivy", "t4"), 404, false},
		{"IvyWebTaskViaOps", "ivy", "ops", zgBulk("ivy", "t1"), 404, false},
		{"ArchivedManager", "ann", "legacy", zgBulk("ann", "t5"), 409, false},
		{"ArchivedManagerUnassign", "ann", "legacy", zgBulk("", "t5"), 409, false},
		// assignee validation
		{"AssigneeViewer", "ann", "web", zgBulk("dan", "t1"), 400, false},
		{"AssigneeNonMember", "ann", "web", zgBulk("hal", "t1"), 400, false},
		{"AssigneeOrgAdmin", "ann", "web", zgBulk("eve", "t1"), 400, false},
		{"AssigneeOtherOrg", "ann", "web", zgBulk("gus", "t1"), 400, false},
		{"AssigneeUnknown", "ann", "web", zgBulk("zed", "t1"), 400, false},
		{"IvyAssigneeViewer", "ivy", "web", zgBulk("dan", "t1", "t2"), 400, false},
		{"IvyAssigneeHal", "ivy", "web", zgBulk("hal", "t3"), 400, false},
		{"IvyOpsAssigneeAnnViewer", "ivy", "ops", zgBulk("ann", "t4"), 400, false},
		{"IvyOpsAssigneeBenNotMember", "ivy", "ops", zgBulk("ben", "t4"), 400, false},
		{"AnnAllThreeAssigneeUnknown", "ann", "web", zgBulk("zed", "t1", "t2", "t3"), 400, false},
		// atomicity: mixed batches
		{"ValidPlusOtherProject", "ann", "web", zgBulk("cat", "t1", "t4"), 404, true},
		{"OtherProjectFirst", "ann", "web", zgBulk("cat", "t4", "t1"), 404, true},
		{"ValidPlusOtherOrg", "ann", "web", zgBulk("cat", "t1", "t6"), 404, true},
		{"ValidPlusNonexistentLast", "ann", "web", zgBulk("cat", "t1", "t2", "t999"), 404, true},
		{"NonexistentFirst", "ann", "web", zgBulk("cat", "t999", "t1"), 404, true},
		{"ValidPlusArchivedTask", "ann", "web", zgBulk("cat", "t3", "t5"), 404, true},
		{"IvyOpsValidPlusWebTask", "ivy", "ops", zgBulk("ivy", "t4", "t1"), 404, true},
		{"IvyWebValidPlusOpsTask", "ivy", "web", zgBulk("", "t1", "t4"), 404, true},
		{"ValidIDsInvalidAssigneeViewer", "ann", "web", zgBulk("dan", "t1", "t2", "t3"), 400, true},
		{"ValidIDsInvalidAssigneeUnknown", "ivy", "web", zgBulk("zed", "t1", "t3"), 400, true},
		{"MixedIDsInvalidAssignee", "ann", "web", zgBulk("dan", "t1", "t4"), 0, true},
		{"ContributorOwnPlusOthers", "ben", "web", zgBulk("ben", "t1", "t2"), 403, true},
		{"ContributorOthersPlusOwn", "cat", "web", zgBulk("cat", "t1", "t2"), 403, true},
		{"ContributorOwnPlusOtherProject", "ben", "web", zgBulk("ben", "t1", "t4"), 0, true},
	}
	for _, c := range cases {
		c := c
		cat := "AUTHZ_"
		if c.atomic {
			cat = "ATOM_"
		}
		t.Run(cat+c.name, func(t *testing.T) { zgDenied(t, c.user, c.pid, c.body) })
		if c.want != 0 {
			t.Run("CONTRACT_"+c.name, func(t *testing.T) {
				code, body := zgDo(newServer(), c.user, "PATCH", "/projects/"+c.pid+"/tasks/assignee", c.body)
				if code != c.want {
					t.Fatalf("got %d want %d body=%s", code, c.want, body)
				}
			})
		}
	}

	// ---------- VALID ----------
	vcases := []struct{ name, body string }{
		{"MalformedJSON", `{"taskIds":["t1","t2"],"assignee":`},
		{"NotJSON", `assignee=cat`},
		{"TaskIdsNotArray", `{"taskIds":"t1","assignee":"cat"}`},
		{"TaskIdsWrongElemType", `{"taskIds":[1,2],"assignee":"cat"}`},
		{"MissingTaskIds", `{"assignee":"cat"}`},
		{"AssigneeWrongType", `{"taskIds":["t1"],"assignee":5}`},
	}
	for _, c := range vcases {
		c := c
		t.Run("VALID_"+c.name, func(t *testing.T) {
			h := newServer()
			before := zgSnap(t, h)
			code, body := zgDo(h, "ann", "PATCH", "/projects/web/tasks/assignee", c.body)
			if code != 400 {
				t.Errorf("got %d want 400 body=%s", code, body)
			}
			if d := zgDiff(before, zgSnap(t, h)); d != "" {
				t.Errorf("state changed: %s", d)
			}
		})
	}

	// ---------- INFO (never fails; ambiguous contract) ----------
	info := func(name, user, pid, body string) {
		t.Run("INFO_"+name, func(t *testing.T) {
			h := newServer()
			before := zgSnap(t, h)
			code, resp := zgDo(h, user, "PATCH", "/projects/"+pid+"/tasks/assignee", body)
			t.Logf("INFO %s: status=%d body=%s changed=%q", name, code, strings.TrimSpace(resp), zgDiff(before, zgSnap(t, h)))
		})
	}
	info("EmptyTaskIds", "ann", "web", zgBulk("cat"))
	info("DuplicateIDs", "ann", "web", zgBulk("cat", "t1", "t1"))
	info("MissingAssigneeField", "ann", "web", `{"taskIds":["t1","t2"]}`)
	info("NullAssignee", "ann", "web", `{"taskIds":["t1","t2"],"assignee":null}`)
	info("NullTaskIds", "ann", "web", `{"taskIds":null,"assignee":"cat"}`)
	info("ContributorInvalidAssignee", "ben", "web", zgBulk("dan", "t1"))
	info("TaskIdsEmptyString", "ann", "web", zgBulk("cat", ""))
}
