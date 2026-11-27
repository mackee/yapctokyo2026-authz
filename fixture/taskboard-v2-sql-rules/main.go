package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"log"
	"net/http"
)

// Taskboard: a tiny project task tracker (SQLite).
//
// Identity: the X-User header names the caller (demo stand-in for real auth).
//
// Access rules (see README.md):
//   - unknown or missing user                                  -> 401
//   - project missing, in another org, or not readable by user -> 404
//   - a project is readable by its members (any role) and by org admins of its org
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
	db *sql.DB
}

func newServer() http.Handler {
	db, err := openSeededDB()
	if err != nil {
		panic(err)
	}
	s := &server{db: db}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /projects/{pid}/tasks", s.listTasks)
	mux.HandleFunc("POST /projects/{pid}/tasks", s.createTask)
	mux.HandleFunc("GET /projects/{pid}/tasks/{tid}", s.getTask)
	mux.HandleFunc("PATCH /projects/{pid}/tasks/{tid}", s.updateTask)
	mux.HandleFunc("DELETE /projects/{pid}/tasks/{tid}", s.deleteTask)
	return mux
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// ---- lookups (sql.ErrNoRows -> found=false) ------------------------------------

type user struct {
	ID       string
	OrgID    string
	OrgAdmin bool
}

type project struct {
	ID       string
	OrgID    string
	Archived bool
}

func findUser(ctx context.Context, db *sql.DB, id string) (*user, error) {
	var u user
	err := db.QueryRowContext(ctx, `SELECT id, org_id, org_admin FROM users WHERE id = ?`, id).Scan(&u.ID, &u.OrgID, &u.OrgAdmin)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func findProject(ctx context.Context, db *sql.DB, id string) (*project, error) {
	var p project
	err := db.QueryRowContext(ctx, `SELECT id, org_id, archived FROM projects WHERE id = ?`, id).Scan(&p.ID, &p.OrgID, &p.Archived)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// findRole returns the user's role in the project ("" if not a member).
func findRole(ctx context.Context, db *sql.DB, projectID, userID string) (string, error) {
	var role string
	err := db.QueryRowContext(ctx, `SELECT role FROM memberships WHERE project_id = ? AND user_id = ?`, projectID, userID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return role, err
}

func findTask(ctx context.Context, db *sql.DB, id string) (*Task, error) {
	var t Task
	err := db.QueryRowContext(ctx, `SELECT id, project_id, title, status, assignee FROM tasks WHERE id = ?`, id).Scan(&t.ID, &t.ProjectID, &t.Title, &t.Status, &t.Assignee)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// ---- handlers --------------------------------------------------------------------

func (s *server) listTasks(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, err := findUser(ctx, s.db, r.Header.Get("X-User"))
	if err != nil {
		writeError(w, 500, "internal error")
		return
	}
	if user == nil {
		writeError(w, 401, "unauthenticated")
		return
	}
	project, err := findProject(ctx, s.db, r.PathValue("pid"))
	if err != nil {
		writeError(w, 500, "internal error")
		return
	}
	if project == nil || project.OrgID != user.OrgID {
		writeError(w, 404, "not found")
		return
	}
	role, err := findRole(ctx, s.db, project.ID, user.ID)
	if err != nil {
		writeError(w, 500, "internal error")
		return
	}
	if role == "" && !user.OrgAdmin {
		writeError(w, 404, "not found")
		return
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, project_id, title, status, assignee FROM tasks WHERE project_id = ? ORDER BY id`, project.ID)
	if err != nil {
		writeError(w, 500, "internal error")
		return
	}
	defer rows.Close()
	tasks := []*Task{}
	for rows.Next() {
		var t Task
		if err := rows.Scan(&t.ID, &t.ProjectID, &t.Title, &t.Status, &t.Assignee); err != nil {
			writeError(w, 500, "internal error")
			return
		}
		tasks = append(tasks, &t)
	}
	if err := rows.Err(); err != nil {
		writeError(w, 500, "internal error")
		return
	}
	writeJSON(w, 200, tasks)
}

func (s *server) getTask(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, err := findUser(ctx, s.db, r.Header.Get("X-User"))
	if err != nil {
		writeError(w, 500, "internal error")
		return
	}
	if user == nil {
		writeError(w, 401, "unauthenticated")
		return
	}
	project, err := findProject(ctx, s.db, r.PathValue("pid"))
	if err != nil {
		writeError(w, 500, "internal error")
		return
	}
	if project == nil || project.OrgID != user.OrgID {
		writeError(w, 404, "not found")
		return
	}
	role, err := findRole(ctx, s.db, project.ID, user.ID)
	if err != nil {
		writeError(w, 500, "internal error")
		return
	}
	if role == "" && !user.OrgAdmin {
		writeError(w, 404, "not found")
		return
	}
	task, err := findTask(ctx, s.db, r.PathValue("tid"))
	if err != nil {
		writeError(w, 500, "internal error")
		return
	}
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
	ctx := r.Context()
	user, err := findUser(ctx, s.db, r.Header.Get("X-User"))
	if err != nil {
		writeError(w, 500, "internal error")
		return
	}
	if user == nil {
		writeError(w, 401, "unauthenticated")
		return
	}
	project, err := findProject(ctx, s.db, r.PathValue("pid"))
	if err != nil {
		writeError(w, 500, "internal error")
		return
	}
	if project == nil || project.OrgID != user.OrgID {
		writeError(w, 404, "not found")
		return
	}
	role, err := findRole(ctx, s.db, project.ID, user.ID)
	if err != nil {
		writeError(w, 500, "internal error")
		return
	}
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
		assigneeRole, err := findRole(ctx, s.db, project.ID, req.Assignee)
		if err != nil {
			writeError(w, 500, "internal error")
			return
		}
		if assigneeRole != RoleManager && assigneeRole != RoleContributor {
			writeError(w, 400, "assignee must be a contributor or manager of the project")
			return
		}
		if role == RoleContributor && req.Assignee != user.ID {
			writeError(w, 403, "contributors may only assign tasks to themselves")
			return
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		writeError(w, 500, "internal error")
		return
	}
	defer tx.Rollback()
	id, err := newTaskID(ctx, tx)
	if err != nil {
		writeError(w, 500, "internal error")
		return
	}
	task := &Task{ID: id, ProjectID: project.ID, Title: req.Title, Status: "open", Assignee: req.Assignee}
	if _, err := tx.ExecContext(ctx, `INSERT INTO tasks (id, project_id, title, status, assignee) VALUES (?, ?, ?, ?, ?)`,
		task.ID, task.ProjectID, task.Title, task.Status, task.Assignee); err != nil {
		writeError(w, 500, "internal error")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, 500, "internal error")
		return
	}
	writeJSON(w, 200, task)
}

type updateRequest struct {
	Title    *string `json:"title"`
	Status   *string `json:"status"`
	Assignee *string `json:"assignee"`
}

func (s *server) updateTask(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, err := findUser(ctx, s.db, r.Header.Get("X-User"))
	if err != nil {
		writeError(w, 500, "internal error")
		return
	}
	if user == nil {
		writeError(w, 401, "unauthenticated")
		return
	}
	project, err := findProject(ctx, s.db, r.PathValue("pid"))
	if err != nil {
		writeError(w, 500, "internal error")
		return
	}
	if project == nil || project.OrgID != user.OrgID {
		writeError(w, 404, "not found")
		return
	}
	role, err := findRole(ctx, s.db, project.ID, user.ID)
	if err != nil {
		writeError(w, 500, "internal error")
		return
	}
	if role == "" && !user.OrgAdmin {
		writeError(w, 404, "not found")
		return
	}
	task, err := findTask(ctx, s.db, r.PathValue("tid"))
	if err != nil {
		writeError(w, 500, "internal error")
		return
	}
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
			assigneeRole, err := findRole(ctx, s.db, project.ID, *req.Assignee)
			if err != nil {
				writeError(w, 500, "internal error")
				return
			}
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
	if _, err := s.db.ExecContext(ctx, `UPDATE tasks SET title = ?, status = ?, assignee = ? WHERE id = ?`,
		task.Title, task.Status, task.Assignee, task.ID); err != nil {
		writeError(w, 500, "internal error")
		return
	}
	writeJSON(w, 200, task)
}

func (s *server) deleteTask(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, err := findUser(ctx, s.db, r.Header.Get("X-User"))
	if err != nil {
		writeError(w, 500, "internal error")
		return
	}
	if user == nil {
		writeError(w, 401, "unauthenticated")
		return
	}
	project, err := findProject(ctx, s.db, r.PathValue("pid"))
	if err != nil {
		writeError(w, 500, "internal error")
		return
	}
	if project == nil || project.OrgID != user.OrgID {
		writeError(w, 404, "not found")
		return
	}
	role, err := findRole(ctx, s.db, project.ID, user.ID)
	if err != nil {
		writeError(w, 500, "internal error")
		return
	}
	if role == "" && !user.OrgAdmin {
		writeError(w, 404, "not found")
		return
	}
	task, err := findTask(ctx, s.db, r.PathValue("tid"))
	if err != nil {
		writeError(w, 500, "internal error")
		return
	}
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
	if _, err := s.db.ExecContext(ctx, `DELETE FROM tasks WHERE id = ?`, task.ID); err != nil {
		writeError(w, 500, "internal error")
		return
	}
	w.WriteHeader(204)
}
