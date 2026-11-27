# Examples that Transformer + authorization objects + model functions required by type could constrain (5 cases)

Selected from the haiku-attempt trials (001–015): places where Haiku actually made mistakes, and places the grader role flagged as latent risks.
None of this is implemented in tanukirpc. These are sketches using the same vocabulary as case-3.

- **Transformer**: For each route, puts only authorized objects into the Registry. The raw store lives only inside the Transformer's closure.
- **Authorization object (capability)**: An unexported concrete type that only `AcquireXxx` can create.
- **Model function**: Requires an authorization object through its parameter type. It is the only entry point that changes state.

A shared precondition is that handlers cannot reach `*Store` (case-3's "raw store unreachable").
If this breaks, every type constraint below goes back to being "a promise that can be bypassed".

---

## 1. Assignee rule at the move destination (observed: missing in 6 of 6 move trials)

**Observation**
- In all of 004, 005, 009, 010, 011 and 012, `moveTask` checked 404/403/409 for the destination.
- But it did not check "the assignee must be a contributor/manager of the destination, otherwise 400", and ended with `task.ProjectID = target.ID`.
- Example: when ivy moves ben's t1 from web to ops, a non-member of ops stays as the assignee.
- In 004, Haiku itself wrote "does not re-check" in the README. In 005, its own test expected 200.
- In contrast, copy (008) and re-home via PATCH (013) validated the assignee. Copy copied `createTask`, and re-home copied the existing PATCH assignee check.

**How to constrain it**
```go
// Built by the destination's Transformer. Write permission (contributor/manager), same org and not archived are already checked.
type WriteProject interface {
    ID() string
    // An assignee can only be built after checking that they are a writer in this project.
    AdmitAssignee(userID string) (AssigneeIn, error)
}
type AssigneeIn struct{ project, user string } // fields are unexported; only WriteProject can build one

// The only model function that changes a task's project. The type requires the assignee to be checked again.
func MoveTask(src EditTask, dst WriteProject, assignee AssigneeIn) error // also checks assignee.project == dst.ID() internally
```
- `task.ProjectID` cannot be written directly, so a move has to go through `MoveTask`.
- `MoveTask` requires an `AssigneeIn`, so the code does not compile without calling `dst.AdmitAssignee(task.Assignee)`.
- This moves "a constraint that exists only on the create side" into the destination's authorization object. Copy, move, re-home and merge (014) all go through the same `AdmitAssignee`.

**Limitation**: Types alone cannot prevent reusing the same `AssigneeIn` from the destination for a different task (a runtime check covers this).

---

## 2. Role demotion and the "assignee is a writer" invariant (observed: 6 ATOM FAIL in 015)

**Observation**
- In 015, Haiku added `func (s *Store) SetRole(...) { s.members[p][u] = role }` to `store.go`.
- The handler correctly checked whether the caller is a manager, the archive state and so on, then called `SetRole` directly.
- As a result, demoting ben, the assignee of t1, to viewer in web left ben as "a viewer who is still the assignee" (the PUT returned 200).
- Every caller check was correct. What broke was an invariant that spans other entities.

**How to constrain it**
```go
// Only a manager of the project can receive this from the Transformer. If the project is archived, 409 when acquiring it.
type ManageMembers interface {
    Project() string
    // The only entry point for role changes. On demotion to viewer, the caller must choose what happens to the assigned tasks.
    ChangeRole(user string, role Role, onAssigned AssignedTaskPolicy) (RoleChange, error)
}
type AssignedTaskPolicy interface{ isPolicy() }                // unexported marker
var RejectIfAssigned, UnassignTasks AssignedTaskPolicy        // only these two exist
```
- Lock the equivalent of the store's `SetRole` inside the capability package so handlers cannot call it.
- `onAssigned` is a required argument, so whoever writes the handler (the agent) is made aware of the invariant through the type.
- The actual check only needs to be implemented once, inside `ChangeRole`. It does not need to be copied into each handler.

**Limitation**: The responsibility for writing the invariant correctly inside the model function remains. What can be constrained is "bypassing it to write to the store" and "forgetting that the invariant exists".

---

## 3. Separating "edit" and "change assignee" permissions (observed: 007, 013 and 008 correct, but only depending on how code was copied)

**Observation**
- The README rule is "a contributor can edit a task assigned to themselves, but cannot change its assignee".
- Bulk status update (001) correctly copied "allowed if it is your own task".
- The trap we expected in bulk reassign (007) was copying 001's "allowed if it is your own task" as is. Haiku avoided it (403 for non-managers).
- In re-home via PATCH (013), a contributor changing the assignee also got 403.
- In every case "the copied logic happened to be correct". There is no structural guarantee.

**How to constrain it**
```go
type EditTask interface {        // a manager, or a contributor who is the task's assignee
    View() TaskView
    SetTitle(string) error
    SetStatus(Status) error
}
type AssignTask interface {      // only a manager can Acquire this; a different type from EditTask
    EditTask
    Reassign(AssigneeIn) error   // takes the AssigneeIn from 1., so the assignee check is enforced at the same time
}
func (a *Authorizer) AcquireEdit(actor, project, task string) (EditTask, error)
func (a *Authorizer) AcquireAssign(actor, project, task string) (AssignTask, error)
// Bulk operations acquire every item at once. If any one fails, no capability is returned, so atomicity is expressed in the type too.
func (a *Authorizer) AcquireAssignAll(actor, project string, ids []string) ([]AssignTask, error)
```
- This is the same idea as case-3's split between `ReadInvoice` and `EditInvoice`.
- `EditTask` has no `Reassign`, so writing an assignee change on the contributor path does not compile.
- The all-or-nothing behavior of bulk operations falls naturally into the form "write once every Acquire has succeeded". 001, 007 and 014 each wrote this by hand.

---

## 4. Cross-project queries and store functions "callable from anywhere" (observed: 006 correct, the grader role flagged a latent risk)

**Observation**
- In 006 (`GET /me/tasks`), Haiku added `store.TasksAssignedTo(user)`.
- In the handler, it applied the same "can read" check as `listTasks` to each task, as a 5th copy, and was correct.
- But `TasksAssignedTo` itself scans all projects without authorization. If another caller uses it directly in the future, it leaks as is.

**How to constrain it**
```go
// Only the Transformer (or Authorizer) builds the set of "projects this actor can read".
type ReadableProjects interface {
    // Returns only results narrowed to what is readable. Raw store scans happen only inside this implementation.
    TasksAssignedTo(user string) []TaskView
}
func (a *Authorizer) AcquireReadable(actor string) (ReadableProjects, error)
```
- Define the model function for the cross-project query as a method of the read-permission object. Do not export scan functions without authorization.
- The question "where should the authorization check be copied" disappears.

---

## 5. Collecting policy changes in one place (observed: org admin change in 002/003, up to 5 copies of the same check)

**Observation**
- In 002/003 ("an org admin can write like a manager"), Haiku created `effectiveRole()` on its own, applied it to all 5 handlers, and was correct.
- But this happened only because Haiku chose to put it in a helper. In a codebase that copies `if` statements, every new handler adds another copy of the same check. 006 made the 5th copy, 008 the 4th, and 013 added a destination check inside PATCH.
- The risk of "missing one copy" during a policy change grows as more trials pile up.

**How to constrain it**
```go
// The project-level Transformer builds the authorization object once, from the actor and the project.
// What the handler receives is the result of AcquireXxx, not a role string.
tanukirpc.RouteWithTransformer(r, projectTransformer(authz), "/projects/{pid}", func(r *tanukirpc.Router[*projectRegistry]) {
    // projectRegistry holds only the ones among ReadProject / WriteProject / ManageMembers that could be acquired
})
```
- "Treat org admin as manager" becomes a change in one place, the `Authorizer` decision function. Every handler follows automatically.
- The place where a handler would write a comparison like `role != RoleManager` no longer exists.

**Limitation**: If the Transformer only handles the project in the path, a **project specified in the body**, such as a move or copy destination, requires calling `AcquireWrite(dstID)` inside the model function (the `WriteProject` in 1.). Here too, if the argument cannot be built without Acquire, a forgotten acquisition is stopped at compile time.

---

## Summary: what types stop and what remains

| # | Mistake observed / flagged | What types stop | What remains |
|---|---|---|---|
| 1 | Missing destination assignee rule (6/6) | A move that does not call `AdmitAssignee` | Reusing an acquired `AssigneeIn` |
| 2 | Invariant violation through demotion (015) | Direct store updates, forgetting to choose how assigned tasks are handled | Correctness of the implementation inside `ChangeRole` |
| 3 | Confusing edit with assignee change (expected as a trap) | Assignee change on the contributor path, partial application of bulk operations | The Acquire decision logic itself |
| 4 | Reuse of a scan function without authorization (latent) | Cross-project queries that do not narrow the read scope | ― |
| 5 | Missed copy during a policy change (up to 5 copies) | Per-handler role comparisons | The Authorizer decision logic itself |

All of these assume that "handlers cannot reach the raw store". This uses the same property as in case-3, where `reg.tenant undefined` and `capability.editableInvoice undefined` became compile errors.
