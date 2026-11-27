package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
)

// Taskboard: a tiny project task tracker.
//
// Identity: the X-User header names the caller (demo stand-in for real auth).
//
// Access rules (see README.md):
//   - unknown or missing user                                  -> 401
//   - project missing, in another org, or not readable by user -> 404
//   - a project is readable by its members (any role) and by org admins of its org
//   - org admins act as managers in every project of their org
//   - writes need contributor or manager role in the project;
//     readable-but-not-allowed                                 -> 403
//   - contributors may only modify tasks assigned to themselves
//     and may not reassign tasks; managers may modify any task
//   - only managers may delete tasks
//   - archived projects reject all writes                      -> 409
//   - a task ID that is not in the path project               -> 404

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	flag.Parse()
	log.Printf("listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, newServer()))
}

type server struct {
	store *Store
}

func newServer() http.Handler {
	s := &server{store: newSeededStore()}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /projects/{pid}/tasks", s.listTasks)
	mux.HandleFunc("POST /projects/{pid}/tasks", s.createTask)
	mux.HandleFunc("GET /projects/{pid}/tasks/{tid}", s.getTask)
	mux.HandleFunc("PATCH /projects/{pid}/tasks/{tid}", s.updateTask)
	mux.HandleFunc("DELETE /projects/{pid}/tasks/{tid}", s.deleteTask)
	return mux
}

// effectiveRole returns the role used for authorization checks. An org admin
// acts as a manager in every project of their organization; otherwise the
// project membership role (possibly "") applies.
func effectiveRole(memberRole string, user *User) string {
	if user.OrgAdmin {
		return RoleManager
	}
	return memberRole
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func (s *server) listTasks(w http.ResponseWriter, r *http.Request) {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()

	user := s.store.User(r.Header.Get("X-User"))
	if user == nil {
		writeError(w, 401, "unauthenticated")
		return
	}
	project := s.store.Project(r.PathValue("pid"))
	if project == nil || project.OrgID != user.OrgID {
		writeError(w, 404, "not found")
		return
	}
	role := effectiveRole(s.store.Role(project.ID, user.ID), user)
	if role == "" && !user.OrgAdmin {
		writeError(w, 404, "not found")
		return
	}
	tasks := s.store.TasksInProject(project.ID)
	if tasks == nil {
		tasks = []*Task{}
	}
	writeJSON(w, 200, tasks)
}

func (s *server) getTask(w http.ResponseWriter, r *http.Request) {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()

	user := s.store.User(r.Header.Get("X-User"))
	if user == nil {
		writeError(w, 401, "unauthenticated")
		return
	}
	project := s.store.Project(r.PathValue("pid"))
	if project == nil || project.OrgID != user.OrgID {
		writeError(w, 404, "not found")
		return
	}
	role := effectiveRole(s.store.Role(project.ID, user.ID), user)
	if role == "" && !user.OrgAdmin {
		writeError(w, 404, "not found")
		return
	}
	task := s.store.Task(r.PathValue("tid"))
	if task == nil || task.ProjectID != project.ID {
		writeError(w, 404, "not found")
		return
	}
	writeJSON(w, 200, task)
}

type createRequest struct {
	Title    string `json:"title"`
	Assignee string `json:"assignee"`
}

func (s *server) createTask(w http.ResponseWriter, r *http.Request) {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()

	user := s.store.User(r.Header.Get("X-User"))
	if user == nil {
		writeError(w, 401, "unauthenticated")
		return
	}
	project := s.store.Project(r.PathValue("pid"))
	if project == nil || project.OrgID != user.OrgID {
		writeError(w, 404, "not found")
		return
	}
	role := effectiveRole(s.store.Role(project.ID, user.ID), user)
	if role == "" && !user.OrgAdmin {
		writeError(w, 404, "not found")
		return
	}
	if role != RoleManager && role != RoleContributor {
		writeError(w, 403, "forbidden")
		return
	}
	if project.Archived {
		writeError(w, 409, "project is archived")
		return
	}
	var req createRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Title == "" {
		writeError(w, 400, "invalid request body")
		return
	}
	if req.Assignee != "" {
		assigneeRole := s.store.Role(project.ID, req.Assignee)
		if assigneeRole != RoleManager && assigneeRole != RoleContributor {
			writeError(w, 400, "assignee must be a contributor or manager of the project")
			return
		}
		if role == RoleContributor && req.Assignee != user.ID {
			writeError(w, 403, "contributors may only assign tasks to themselves")
			return
		}
	}
	task := &Task{
		ID:        s.store.NewTaskID(),
		ProjectID: project.ID,
		Title:     req.Title,
		Status:    "open",
		Assignee:  req.Assignee,
	}
	s.store.PutTask(task)
	writeJSON(w, 201, task)
}

type updateRequest struct {
	Title    *string `json:"title"`
	Status   *string `json:"status"`
	Assignee *string `json:"assignee"`
}

func (s *server) updateTask(w http.ResponseWriter, r *http.Request) {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()

	user := s.store.User(r.Header.Get("X-User"))
	if user == nil {
		writeError(w, 401, "unauthenticated")
		return
	}
	project := s.store.Project(r.PathValue("pid"))
	if project == nil || project.OrgID != user.OrgID {
		writeError(w, 404, "not found")
		return
	}
	role := effectiveRole(s.store.Role(project.ID, user.ID), user)
	if role == "" && !user.OrgAdmin {
		writeError(w, 404, "not found")
		return
	}
	task := s.store.Task(r.PathValue("tid"))
	if task == nil || task.ProjectID != project.ID {
		writeError(w, 404, "not found")
		return
	}
	if role != RoleManager && role != RoleContributor {
		writeError(w, 403, "forbidden")
		return
	}
	if role == RoleContributor && task.Assignee != user.ID {
		writeError(w, 403, "contributors may only modify their own tasks")
		return
	}
	if project.Archived {
		writeError(w, 409, "project is archived")
		return
	}
	var req updateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid request body")
		return
	}
	if req.Title != nil && *req.Title == "" {
		writeError(w, 400, "title must not be empty")
		return
	}
	if req.Status != nil && !validStatuses[*req.Status] {
		writeError(w, 400, "invalid status")
		return
	}
	if req.Assignee != nil {
		if role != RoleManager {
			writeError(w, 403, "only managers may reassign tasks")
			return
		}
		if *req.Assignee != "" {
			assigneeRole := s.store.Role(project.ID, *req.Assignee)
			if assigneeRole != RoleManager && assigneeRole != RoleContributor {
				writeError(w, 400, "assignee must be a contributor or manager of the project")
				return
			}
		}
	}
	if req.Title != nil {
		task.Title = *req.Title
	}
	if req.Status != nil {
		task.Status = *req.Status
	}
	if req.Assignee != nil {
		task.Assignee = *req.Assignee
	}
	writeJSON(w, 200, task)
}

func (s *server) deleteTask(w http.ResponseWriter, r *http.Request) {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()

	user := s.store.User(r.Header.Get("X-User"))
	if user == nil {
		writeError(w, 401, "unauthenticated")
		return
	}
	project := s.store.Project(r.PathValue("pid"))
	if project == nil || project.OrgID != user.OrgID {
		writeError(w, 404, "not found")
		return
	}
	role := effectiveRole(s.store.Role(project.ID, user.ID), user)
	if role == "" && !user.OrgAdmin {
		writeError(w, 404, "not found")
		return
	}
	task := s.store.Task(r.PathValue("tid"))
	if task == nil || task.ProjectID != project.ID {
		writeError(w, 404, "not found")
		return
	}
	if role != RoleManager {
		writeError(w, 403, "forbidden")
		return
	}
	if project.Archived {
		writeError(w, 409, "project is archived")
		return
	}
	s.store.DeleteTask(task.ID)
	w.WriteHeader(204)
}
