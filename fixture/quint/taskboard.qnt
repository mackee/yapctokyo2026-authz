// A Quint model of taskboard: the data, the access rules and the model invariants.
//
// Every HTTP operation is modelled twice:
//   - an outcome function (e.g. updateOutcome) that returns the HTTP status the
//     server must answer for that request, following the README access rules;
//   - an action (e.g. updateTask) that applies the change when the outcome is 2xx
//     and leaves the state unchanged otherwise.
// `step` picks any user and any request at random. `invariants` must hold in
// every reachable state.
//
// Check it with:
//   quint typecheck spec/taskboard.qnt
//   quint run spec/taskboard.qnt --invariant=invariants --max-steps=20 --max-samples=2000
//
// Task IDs are integers here (t1 is 1, t2 is 2, ...). "" means "no user" (an
// unassigned task, or no membership).
module taskboard {
  type UserId = str
  type ProjectId = str
  type TaskId = int
  type Role = str // "manager" | "contributor" | "viewer"; "" = not a member

  type User = { org: str, orgAdmin: bool }
  type Project = { org: str, archived: bool }
  type Task = { project: ProjectId, status: str, assignee: UserId }

  pure val USERS = Set("ann", "ben", "cat", "dan", "eve", "hal", "ivy", "gus", "nobody")
  pure val PROJECTS = Set("web", "ops", "legacy", "gx", "missing")
  pure val ROLES = Set("manager", "contributor", "viewer")
  pure val STATUSES = Set("open", "doing", "done")

  var users: UserId -> User
  var projects: ProjectId -> Project
  var members: (ProjectId, UserId) -> Role
  var tasks: TaskId -> Task
  var nextTask: TaskId

  // ---- helpers -------------------------------------------------------------

  def userExists(u: UserId): bool = users.keys().contains(u)
  def projectExists(p: ProjectId): bool = projects.keys().contains(p)
  def taskExists(t: TaskId): bool = tasks.keys().contains(t)

  def roleOf(p: ProjectId, u: UserId): Role =
    if (members.keys().contains((p, u))) members.get((p, u)) else ""

  def isWriter(p: ProjectId, u: UserId): bool =
    Set("contributor", "manager").contains(roleOf(p, u))

  // A project is readable by its members (any role) and by org admins of the
  // same organization. A project in another organization is never readable.
  def canRead(u: UserId, p: ProjectId): bool = and {
    userExists(u),
    projectExists(p),
    users.get(u).org == projects.get(p).org,
    or { roleOf(p, u) != "", users.get(u).orgAdmin },
  }

  // ---- model invariants ----------------------------------------------------

  // Tasks belong to existing projects.
  val inv_taskInExistingProject =
    tasks.keys().forall(t => projectExists(tasks.get(t).project))

  // An assignee is a contributor or manager of the task's project.
  val inv_assigneeIsWriter =
    tasks.keys().forall(t => {
      val x = tasks.get(t)
      x.assignee == "" or isWriter(x.project, x.assignee)
    })

  // Project members belong to the project's organization.
  val inv_memberInProjectOrg =
    members.keys().forall(k => and {
      projectExists(k._1),
      userExists(k._2),
      users.get(k._2).org == projects.get(k._1).org,
    })

  val invariants = and {
    inv_taskInExistingProject,
    inv_assigneeIsWriter,
    inv_memberInProjectOrg,
  }

  // ---- outcome functions (HTTP status per request) -------------------------

  // GET /projects/{pid}/tasks and GET /projects/{pid}/tasks/{tid}
  def readOutcome(u: UserId, p: ProjectId): int =
    if (not(userExists(u))) 401
    else if (not(canRead(u, p))) 404
    else 200

  // POST /projects/{pid}/tasks {"assignee": a}
  def createOutcome(u: UserId, p: ProjectId, a: UserId): int =
    if (not(userExists(u))) 401
    else if (not(canRead(u, p))) 404
    else if (not(isWriter(p, u))) 403
    else if (projects.get(p).archived) 409
    else if (a != "" and not(isWriter(p, a))) 400
    else if (a != "" and roleOf(p, u) == "contributor" and a != u) 403
    else 200

  // PATCH /projects/{pid}/tasks/{tid} {"status"?, "assignee"?}
  // changeAssignee is false when the body has no "assignee" field.
  def updateOutcome(u: UserId, p: ProjectId, t: TaskId, changeAssignee: bool, a: UserId): int =
    if (not(userExists(u))) 401
    else if (not(canRead(u, p))) 404
    else if (not(taskExists(t)) or tasks.get(t).project != p) 404
    else if (not(isWriter(p, u))) 403
    else if (roleOf(p, u) == "contributor" and tasks.get(t).assignee != u) 403
    else if (projects.get(p).archived) 409
    else if (changeAssignee and roleOf(p, u) != "manager") 403
    else if (changeAssignee and a != "" and not(isWriter(p, a))) 400
    else 200

  // DELETE /projects/{pid}/tasks/{tid}
  def deleteOutcome(u: UserId, p: ProjectId, t: TaskId): int =
    if (not(userExists(u))) 401
    else if (not(canRead(u, p))) 404
    else if (not(taskExists(t)) or tasks.get(t).project != p) 404
    else if (roleOf(p, u) != "manager") 403
    else if (projects.get(p).archived) 409
    else 204

  // ---- actions -------------------------------------------------------------

  action unchanged = all {
    users' = users,
    projects' = projects,
    members' = members,
    tasks' = tasks,
    nextTask' = nextTask,
  }

  action createTask(u: UserId, p: ProjectId, a: UserId): bool =
    if (createOutcome(u, p, a) == 200) all {
      tasks' = tasks.put(nextTask, { project: p, status: "open", assignee: a }),
      nextTask' = nextTask + 1,
      users' = users,
      projects' = projects,
      members' = members,
    } else unchanged

  action updateTask(u: UserId, p: ProjectId, t: TaskId, s: str, changeAssignee: bool, a: UserId): bool =
    if (updateOutcome(u, p, t, changeAssignee, a) == 200) all {
      tasks' = tasks.set(t, {
        ...tasks.get(t),
        status: s,
        assignee: if (changeAssignee) a else tasks.get(t).assignee,
      }),
      nextTask' = nextTask,
      users' = users,
      projects' = projects,
      members' = members,
    } else unchanged

  action deleteTask(u: UserId, p: ProjectId, t: TaskId): bool =
    if (deleteOutcome(u, p, t) == 204) all {
      tasks' = tasks.keys().exclude(Set(t)).mapBy(k => tasks.get(k)),
      nextTask' = nextTask,
      users' = users,
      projects' = projects,
      members' = members,
    } else unchanged

  // ---- seed data and steps -------------------------------------------------

  action init = all {
    users' = Map(
      "ann" -> { org: "acme", orgAdmin: false }, "ben" -> { org: "acme", orgAdmin: false },
      "cat" -> { org: "acme", orgAdmin: false }, "dan" -> { org: "acme", orgAdmin: false },
      "eve" -> { org: "acme", orgAdmin: true }, "hal" -> { org: "acme", orgAdmin: false },
      "ivy" -> { org: "acme", orgAdmin: false }, "gus" -> { org: "globex", orgAdmin: false },
    ),
    projects' = Map(
      "web" -> { org: "acme", archived: false }, "ops" -> { org: "acme", archived: false },
      "legacy" -> { org: "acme", archived: true }, "gx" -> { org: "globex", archived: false },
    ),
    members' = Map(
      ("web", "ann") -> "manager", ("web", "ben") -> "contributor", ("web", "cat") -> "contributor",
      ("web", "dan") -> "viewer", ("web", "ivy") -> "manager",
      ("ops", "ann") -> "viewer", ("ops", "cat") -> "contributor", ("ops", "ivy") -> "manager",
      ("legacy", "ann") -> "manager", ("legacy", "ben") -> "contributor",
      ("gx", "gus") -> "manager",
    ),
    tasks' = Map(
      1 -> { project: "web", status: "open", assignee: "ben" },
      2 -> { project: "web", status: "doing", assignee: "cat" },
      3 -> { project: "web", status: "open", assignee: "" },
      4 -> { project: "ops", status: "open", assignee: "cat" },
      5 -> { project: "legacy", status: "open", assignee: "ben" },
      6 -> { project: "gx", status: "open", assignee: "gus" },
    ),
    nextTask' = 7,
  }

  action step = {
    nondet u = USERS.oneOf()
    nondet p = PROJECTS.oneOf()
    nondet a = USERS.union(Set("")).oneOf()
    nondet t = 1.to(nextTask).oneOf()
    nondet s = STATUSES.oneOf()
    nondet changeAssignee = Set(true, false).oneOf()
    any {
      createTask(u, p, a),
      updateTask(u, p, t, s, changeAssignee, a),
      deleteTask(u, p, t),
    }
  }

  // ---- examples (run with `quint test`) ------------------------------------

  // Seed checks: who may do what in the initial state.
  run seedOutcomesTest =
    init.then(all {
      assert(readOutcome("dan", "web") == 200),
      assert(readOutcome("eve", "ops") == 200),
      assert(readOutcome("hal", "web") == 404),
      assert(readOutcome("gus", "web") == 404),
      assert(readOutcome("nobody", "web") == 401),
      assert(createOutcome("ben", "web", "ben") == 200),
      assert(createOutcome("ben", "web", "cat") == 403),
      assert(createOutcome("ann", "web", "dan") == 400),
      assert(createOutcome("ann", "legacy", "") == 409),
      assert(updateOutcome("ben", "web", 1, false, "") == 200),
      assert(updateOutcome("ben", "web", 2, false, "") == 403),
      assert(updateOutcome("ann", "web", 2, true, "ben") == 200),
      assert(updateOutcome("cat", "web", 2, true, "ben") == 403),
      assert(deleteOutcome("ann", "web", 1) == 204),
      assert(deleteOutcome("ben", "web", 1) == 403),
      assert(deleteOutcome("ann", "web", 4) == 404),
      unchanged,
    })
}
