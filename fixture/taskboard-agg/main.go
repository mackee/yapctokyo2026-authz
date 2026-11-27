// Taskboard: a tiny project task tracker (tanukirpc + capability, Active Record + per-operation aggregate variant).
//
// Identity: the X-User header names the caller (demo stand-in for real auth).
// The access rules are in README.md. They are implemented once, in
// internal/capability; this file only wires HTTP to capabilities.
//
// Structure:
//
//	tanukirpc Transformer "/projects/{pid}"  -> projectRegistry{scope capability.ProjectScope}
//	  (authenticates X-User; the scope is bound to the actor and {pid})
//	tanukirpc Transformer "/tasks/{tid}"     -> taskRegistry{scope capability.TaskScope}
//
// Handlers only ever see scopes. To do anything they acquire a capability
// (scope.Read(), scope.Write(), scope.Edit(), scope.Manage(), ...), which
// applies the access rules, and then call methods on it. The raw data and the
// Authorizer are not reachable from handlers.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"io"
	"log"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mackee/tanukirpc"

	"taskboard/internal/capability"
)

// Task is the JSON shape of a task.
type Task = capability.TaskView

type rootRegistry struct{}

// projectRegistry is the whole handler-visible surface under /projects/{pid}.
type projectRegistry struct {
	scope capability.ProjectScope
}

// taskRegistry is the whole handler-visible surface under /projects/{pid}/tasks/{tid}.
type taskRegistry struct {
	scope capability.TaskScope
}

func init() {
	// tanukirpc logs every request through the default slog logger; keep the
	// demo and test output quiet.
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	flag.Parse()
	log.Printf("listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, newServer()))
}

func newServer() http.Handler {
	authz := capability.New()

	r := tanukirpc.NewRouter(&rootRegistry{},
		tanukirpc.WithCodec[*rootRegistry](jsonCodec{}),
		tanukirpc.WithErrorHooker[*rootRegistry](errorHooker{}),
		tanukirpc.WithAccessLogger[*rootRegistry](nil),
	)

	tanukirpc.RouteWithTransformer(r, projectTransformer(authz), "/projects/{pid}", func(pr *tanukirpc.Router[*projectRegistry]) {
		pr.Get("/tasks", tanukirpc.NewHandler(listTasks))
		pr.Post("/tasks", tanukirpc.NewHandler(createTask))

		tanukirpc.RouteWithTransformer(pr, taskTransformer(), "/tasks/{tid}", func(tr *tanukirpc.Router[*taskRegistry]) {
			tr.Get("/", tanukirpc.NewHandler(getTask))
			tr.Patch("/", tanukirpc.NewHandler(updateTask))
			tr.Delete("/", tanukirpc.NewHandler(deleteTask))
		})
	})
	return r
}

// projectTransformer authenticates the caller and binds the scope to {pid}.
// The Authorizer stays in this closure; it is never put in a registry.
func projectTransformer(authz *capability.Authorizer) tanukirpc.Transformer[*rootRegistry, *projectRegistry] {
	return tanukirpc.NewTransformer(func(ctx tanukirpc.Context[*rootRegistry]) (*projectRegistry, error) {
		actor, err := authz.Authenticate(ctx.Request().Header.Get("X-User"))
		if err != nil {
			return nil, err
		}
		return &projectRegistry{scope: actor.Project(chi.URLParam(ctx.Request(), "pid"))}, nil
	})
}

func taskTransformer() tanukirpc.Transformer[*projectRegistry, *taskRegistry] {
	return tanukirpc.NewTransformer(func(ctx tanukirpc.Context[*projectRegistry]) (*taskRegistry, error) {
		return &taskRegistry{scope: ctx.Registry().scope.Task(chi.URLParam(ctx.Request(), "tid"))}, nil
	})
}

// ---- handlers -------------------------------------------------------------------

func listTasks(ctx tanukirpc.Context[*projectRegistry], _ struct{}) ([]Task, error) {
	p, err := ctx.Registry().scope.Read()
	if err != nil {
		return nil, err
	}
	return p.Tasks(), nil
}

type createRequest struct {
	Title    string `json:"title"`
	Assignee string `json:"assignee"`
}

func createTask(ctx tanukirpc.Context[*projectRegistry], _ struct{}) (*Task, error) {
	p, err := ctx.Registry().scope.Write()
	if err != nil {
		return nil, err
	}
	var req createRequest
	if err := decodeBody(ctx.Request(), &req); err != nil || req.Title == "" {
		return nil, errInvalidBody
	}
	assignee, err := p.AdmitAssignee(req.Assignee)
	if err != nil {
		return nil, err
	}
	t, err := p.CreateTask(req.Title, assignee)
	if err != nil {
		return nil, err
	}
	return writeJSON(ctx.Response(), http.StatusCreated, t)
}

func getTask(ctx tanukirpc.Context[*taskRegistry], _ struct{}) (*Task, error) {
	t, err := ctx.Registry().scope.Read()
	if err != nil {
		return nil, err
	}
	v := t.View()
	return &v, nil
}

type updateRequest struct {
	Title    *string `json:"title"`
	Status   *string `json:"status"`
	Assignee *string `json:"assignee"`
}

func updateTask(ctx tanukirpc.Context[*taskRegistry], _ struct{}) (*Task, error) {
	t, err := ctx.Registry().scope.Edit()
	if err != nil {
		return nil, err
	}
	var req updateRequest
	if err := decodeBody(ctx.Request(), &req); err != nil {
		return nil, errInvalidBody
	}
	v, err := t.Apply(capability.TaskPatch{Title: req.Title, Status: req.Status, Assignee: req.Assignee})
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func deleteTask(ctx tanukirpc.Context[*taskRegistry], _ struct{}) (*struct{}, error) {
	t, err := ctx.Registry().scope.Manage()
	if err != nil {
		return nil, err
	}
	if err := t.Delete(); err != nil {
		return nil, err
	}
	ctx.Response().WriteHeader(http.StatusNoContent)
	return nil, nil
}

// ---- HTTP plumbing ------------------------------------------------------------------

// httpError is an error with an explicit status for request-shape problems
// (malformed body) that are not access decisions.
type httpError struct {
	code int
	msg  string
}

func (e *httpError) Error() string { return e.msg }

var errInvalidBody = &httpError{code: http.StatusBadRequest, msg: "invalid request body"}

// decodeBody decodes the JSON request body. Handlers call it after acquiring
// their capability, so authorization errors take precedence over body errors.
func decodeBody(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}

// writeJSON writes a non-200 JSON response; returning nil afterwards tells
// tanukirpc the response is complete.
func writeJSON[T any](w http.ResponseWriter, code int, v T) (*T, error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
	return nil, nil
}

// jsonCodec leaves the request body to the handler (see decodeBody) and
// encodes successful responses as JSON.
type jsonCodec struct{}

func (jsonCodec) Name() string                        { return "json" }
func (jsonCodec) Decode(_ *http.Request, _ any) error { return nil }
func (jsonCodec) Encode(w http.ResponseWriter, _ *http.Request, v any) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	return json.NewEncoder(w).Encode(v)
}

// errorHooker renders errors as {"error":"..."} with the status implied by the
// capability error kind.
type errorHooker struct{}

func (errorHooker) OnError(w http.ResponseWriter, _ *http.Request, _ *slog.Logger, _ tanukirpc.Codec, err error) {
	code := http.StatusInternalServerError
	msg := "internal error"
	var he *httpError
	switch {
	case errors.Is(err, capability.ErrUnauthenticated):
		code, msg = http.StatusUnauthorized, err.Error()
	case errors.Is(err, capability.ErrNotFound):
		code, msg = http.StatusNotFound, err.Error()
	case errors.Is(err, capability.ErrForbidden):
		code, msg = http.StatusForbidden, err.Error()
	case errors.Is(err, capability.ErrArchived):
		code, msg = http.StatusConflict, err.Error()
	case errors.Is(err, capability.ErrInvalid):
		code, msg = http.StatusBadRequest, err.Error()
	case errors.As(err, &he):
		code, msg = he.code, he.msg
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
