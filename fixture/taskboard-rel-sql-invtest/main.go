// Taskboard: a tiny project task tracker (tanukirpc + authorization objects, Active Record with relation methods + per-operation aggregate variant, SQLite).
//
// Identity: the X-User header names the caller (demo stand-in for real auth).
// The access rules are in README.md. They are implemented in internal/model;
// this file only wires HTTP to the model.
//
// Structure:
//
//	tanukirpc Transformer "/projects/{pid}"  -> projectRegistry{db, authz model.ProjectAuthz}
//	  (authenticates X-User and builds the actor's authorization object for {pid})
//	tanukirpc Transformer "/tasks/{tid}"     -> taskRegistry{db, authz model.TaskAuthz}
//	  (builds the actor's authorization object for {tid})
//
// Handlers pass the database and the authorization object of their scope to a
// table operation (model.NewTaskTable().Create(ctx, reg.db, reg.authz, input));
// the operation asks the object whether the actor may perform it.
package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"log"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mackee/tanukirpc"

	"taskboard/internal/model"
)

// Task is the JSON shape of a task.
type Task = model.Task

type rootRegistry struct{}

// projectRegistry is the whole handler-visible surface under /projects/{pid}.
type projectRegistry struct {
	db    model.DB
	authz model.ProjectAuthz
}

// taskRegistry is the whole handler-visible surface under /projects/{pid}/tasks/{tid}.
type taskRegistry struct {
	db    model.DB
	authz model.TaskAuthz
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
	conn, err := model.Open()
	if err != nil {
		panic(err)
	}

	r := tanukirpc.NewRouter(&rootRegistry{},
		tanukirpc.WithCodec[*rootRegistry](tanukirpc.CodecList{tanukirpc.NewJSONCodec(), jsonResponseCodec{}}),
		tanukirpc.WithErrorHooker[*rootRegistry](errorHooker{}),
		tanukirpc.WithAccessLogger[*rootRegistry](nil),
	)

	tanukirpc.RouteWithTransformer(r, projectTransformer(conn), "/projects/{pid}", func(pr *tanukirpc.Router[*projectRegistry]) {
		pr.Get("/tasks", tanukirpc.NewHandler(listTasks))
		pr.Post("/tasks", tanukirpc.NewHandler(createTask))

		tanukirpc.RouteWithTransformer(pr, taskTransformer(), "/tasks/{tid}", func(tr *tanukirpc.Router[*taskRegistry]) {
			tr.Get("/", tanukirpc.NewHandler(getTask))
			tr.Patch("/", tanukirpc.NewHandler(updateTask))
			tr.Delete("/", tanukirpc.NewHandler(deleteTask))
		})
	})
	return &appHandler{Handler: r, db: conn}
}

// appHandler is the HTTP handler newServer returns. It keeps the database so that
// the tests can check the model rules after each request (see rules_test.go).
type appHandler struct {
	http.Handler
	db *sql.DB
}

// projectTransformer authenticates the caller and builds the actor's
// authorization object for {pid}. Unreadable projects stop here (404).
func projectTransformer(conn *sql.DB) tanukirpc.Transformer[*rootRegistry, *projectRegistry] {
	return tanukirpc.NewTransformer(func(ctx tanukirpc.Context[*rootRegistry]) (*projectRegistry, error) {
		actor, err := model.Authenticate(ctx, conn, ctx.Request().Header.Get("X-User"))
		if err != nil {
			return nil, err
		}
		authz, err := model.NewProjectAuthz(ctx, conn, actor, chi.URLParam(ctx.Request(), "pid"))
		if err != nil {
			return nil, err
		}
		return &projectRegistry{db: conn, authz: authz}, nil
	})
}

// taskTransformer builds the actor's authorization object for {tid}. A task
// that does not belong to the project stops here (404).
func taskTransformer() tanukirpc.Transformer[*projectRegistry, *taskRegistry] {
	return tanukirpc.NewTransformer(func(ctx tanukirpc.Context[*projectRegistry]) (*taskRegistry, error) {
		reg := ctx.Registry()
		authz, err := model.NewTaskAuthz(ctx, reg.db, reg.authz, chi.URLParam(ctx.Request(), "tid"))
		if err != nil {
			return nil, err
		}
		return &taskRegistry{db: reg.db, authz: authz}, nil
	})
}

// ---- handlers -------------------------------------------------------------------
//
// tanukirpc decodes and validates the request body before the handler runs, so
// a malformed body is rejected with 400 before any access decision. Handlers
// pass the database and the authorization object of their scope to a table
// operation.

func listTasks(ctx tanukirpc.Context[*projectRegistry], _ struct{}) ([]*Task, error) {
	reg := ctx.Registry()
	return model.NewTaskTable().List(ctx, reg.db, reg.authz)
}

type createRequest struct {
	Title    string `json:"title" validate:"required"`
	Assignee string `json:"assignee"`
}

func createTask(ctx tanukirpc.Context[*projectRegistry], req createRequest) (*Task, error) {
	reg := ctx.Registry()
	return model.NewTaskTable().Create(ctx, reg.db, reg.authz, model.TaskTableCreateInput{Title: req.Title, AssigneeID: req.Assignee})
}

func getTask(ctx tanukirpc.Context[*taskRegistry], _ struct{}) (*Task, error) {
	reg := ctx.Registry()
	return model.NewTaskTable().Get(ctx, reg.db, reg.authz)
}

type updateRequest struct {
	Title    *string `json:"title" validate:"omitempty,min=1"`
	Status   *string `json:"status" validate:"omitempty,oneof=open doing done"`
	Assignee *string `json:"assignee"`
}

func updateTask(ctx tanukirpc.Context[*taskRegistry], req updateRequest) (*Task, error) {
	reg := ctx.Registry()
	return model.NewTaskTable().Update(ctx, reg.db, reg.authz, model.TaskTableUpdateInput{Title: req.Title, Status: req.Status, Assignee: req.Assignee})
}

func deleteTask(ctx tanukirpc.Context[*taskRegistry], _ struct{}) (*struct{}, error) {
	reg := ctx.Registry()
	if err := model.NewTaskTable().Delete(ctx, reg.db, reg.authz); err != nil {
		return nil, err
	}
	ctx.Response().WriteHeader(http.StatusNoContent)
	return nil, nil
}

// ---- HTTP plumbing ------------------------------------------------------------------

// jsonResponseCodec encodes responses as JSON even when the request has no
// Accept header (tanukirpc's JSON codec only answers "*/*" or
// "application/json"). Request bodies are decoded by tanukirpc's JSON codec.
type jsonResponseCodec struct{}

func (jsonResponseCodec) Name() string { return "json-response" }
func (jsonResponseCodec) Decode(_ *http.Request, _ any) error {
	return tanukirpc.ErrRequestNotSupportedAtThisCodec
}
func (jsonResponseCodec) Encode(w http.ResponseWriter, _ *http.Request, v any) error {
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(v)
}

// errorHooker renders errors as {"error":"..."}: request decoding and
// validation errors are 400; model errors map to their status.
type errorHooker struct{}

func (errorHooker) OnError(w http.ResponseWriter, _ *http.Request, _ *slog.Logger, _ tanukirpc.Codec, err error) {
	code := http.StatusInternalServerError
	msg := "internal error"
	var decodeErr *tanukirpc.ErrCodecDecode
	var validateErr *tanukirpc.ValidateError
	switch {
	case errors.As(err, &decodeErr), errors.As(err, &validateErr):
		code, msg = http.StatusBadRequest, "invalid request body"
	case errors.Is(err, model.ErrUnauthenticated):
		code, msg = http.StatusUnauthorized, err.Error()
	case errors.Is(err, model.ErrNotFound):
		code, msg = http.StatusNotFound, err.Error()
	case errors.Is(err, model.ErrForbidden):
		code, msg = http.StatusForbidden, err.Error()
	case errors.Is(err, model.ErrArchived):
		code, msg = http.StatusConflict, err.Error()
	case errors.Is(err, model.ErrInvalid):
		code, msg = http.StatusBadRequest, err.Error()
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
