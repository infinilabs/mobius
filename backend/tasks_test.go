package main

import (
	"context"
	"strings"
	"testing"
)

func TestValidTransitions_AllowedPaths(t *testing.T) {
	allowed := []struct{ from, to string }{
		{"todo", "ready"},
		{"todo", "blocked"},
		{"ready", "in_progress"},
		{"ready", "blocked"},
		{"in_progress", "needs_review"},
		{"in_progress", "ready"},
		{"in_progress", "blocked"},
		{"needs_review", "done"},
		{"needs_review", "ready"},
		{"needs_review", "blocked"},
		{"blocked", "ready"},
	}
	for _, tc := range allowed {
		if !validTransitions[tc.from][tc.to] {
			t.Errorf("expected %s -> %s to be allowed", tc.from, tc.to)
		}
	}
}

func TestValidTransitions_BlockedPaths(t *testing.T) {
	blocked := []struct{ from, to string }{
		{"todo", "done"},
		{"todo", "in_progress"},
		{"ready", "done"},
		{"ready", "needs_review"},
		{"in_progress", "done"},
		{"needs_review", "in_progress"},
		{"done", "ready"},
		{"blocked", "done"},
		{"blocked", "in_progress"},
		{"scheduled", "ready"},
		{"scheduled", "done"},
	}
	for _, tc := range blocked {
		if validTransitions[tc.from][tc.to] {
			t.Errorf("expected %s -> %s to be blocked", tc.from, tc.to)
		}
	}
}

func TestValidTransitions_ScheduledIsTerminal(t *testing.T) {
	if len(validTransitions["scheduled"]) != 0 {
		t.Error("scheduled should have no outgoing transitions")
	}
}

// --- DB-backed state-machine tests (plan 5.3; harness in pgtest_test.go) ---

// UpdateTaskStatus must enforce validTransitions in the database, not just
// hold the map in memory: an invalid flip must leave the row untouched.
func TestUpdateTaskStatus_RejectsInvalidTransition(t *testing.T) {
	pg := testPG(t)
	ctx := context.Background()
	worker := newTestEmployee(t, pg, "Worker", "Engineer")
	id := newTestTask(t, pg, "still ready", "ready", worker, "")

	err := pg.UpdateTaskStatus(ctx, id, "done", "")
	if err == nil || !strings.Contains(err.Error(), "invalid transition") {
		t.Fatalf("ready→done must be rejected as invalid transition, got err=%v", err)
	}
	if got := getTaskRow(t, pg, id).Status; got != "ready" {
		t.Errorf("task status after rejected transition = %q, want ready", got)
	}
}

// A task cannot enter needs_review without a result — the review gate would
// have nothing to review and dependents could be unblocked on empty work.
func TestUpdateTaskStatus_NeedsReviewRequiresResult(t *testing.T) {
	pg := testPG(t)
	ctx := context.Background()
	worker := newTestEmployee(t, pg, "Worker", "Engineer")
	id := newTestTask(t, pg, "no result yet", "in_progress", worker, "")

	err := pg.UpdateTaskStatus(ctx, id, "needs_review", "")
	if err == nil || !strings.Contains(err.Error(), "without a result") {
		t.Fatalf("in_progress→needs_review with empty result must be refused, got err=%v", err)
	}
}

// Approving (→ done) is restricted to the task's creator or the CEO; an
// unrelated employee must not be able to close someone else's review.
func TestUpdateTaskStatus_ApprovalAuthorization(t *testing.T) {
	pg := testPG(t)
	ctx := context.Background()
	creator := newTestEmployee(t, pg, "Creator", "PM")
	ceo := newTestEmployee(t, pg, "Chief", "CEO")
	stranger := newTestEmployee(t, pg, "Stranger", "Engineer")

	mkReviewTask := func(title string) string {
		id := newTestTask(t, pg, title, "needs_review", creator, creator)
		setTaskColumns(t, pg, id, "result = 'deliverable'")
		return id
	}

	id := mkReviewTask("stranger may not approve")
	if err := pg.UpdateTaskStatus(ctx, id, "done", stranger); err == nil ||
		!strings.Contains(err.Error(), "unauthorized") {
		t.Fatalf("approval by unrelated employee must be refused, got err=%v", err)
	}
	if err := pg.UpdateTaskStatus(ctx, id, "done", creator); err != nil {
		t.Fatalf("approval by creator must succeed: %v", err)
	}

	id2 := mkReviewTask("CEO may approve")
	if err := pg.UpdateTaskStatus(ctx, id2, "done", ceo); err != nil {
		t.Fatalf("approval by CEO must succeed: %v", err)
	}
}

// SubmitTaskResult (plan 1.7) is atomic: result + needs_review land together,
// and it obeys the same state machine — a task not in in_progress (e.g.
// already reclaimed or already reviewed) must refuse a late submit.
func TestSubmitTaskResult(t *testing.T) {
	pg := testPG(t)
	ctx := context.Background()
	worker := newTestEmployee(t, pg, "Worker", "Engineer")

	running := newTestTask(t, pg, "submits fine", "in_progress", worker, "")
	if err := pg.SubmitTaskResult(ctx, running, "the deliverable"); err != nil {
		t.Fatalf("submit from in_progress: %v", err)
	}
	row := getTaskRow(t, pg, running)
	if row.Status != "needs_review" || row.Result != "the deliverable" {
		t.Errorf("after submit: status=%q result=%q, want needs_review with result", row.Status, row.Result)
	}

	if err := pg.SubmitTaskResult(ctx, running, "second submit"); err == nil {
		t.Error("submit from needs_review must be refused (double submit)")
	}

	reclaimed := newTestTask(t, pg, "already reclaimed", "ready", worker, "")
	if err := pg.SubmitTaskResult(ctx, reclaimed, "too late"); err == nil {
		t.Error("submit from ready must be refused (task was reclaimed)")
	}
	if row := getTaskRow(t, pg, reclaimed); row.Result != "" {
		t.Errorf("refused submit must not write a result, got %q", row.Result)
	}

	empty := newTestTask(t, pg, "empty result", "in_progress", worker, "")
	if err := pg.SubmitTaskResult(ctx, empty, ""); err == nil {
		t.Error("empty result must be refused")
	}
}

// Completing a task promotes only those dependents whose dependencies are ALL
// done — a dependent still waiting on another task stays in todo.
func TestPromoteDependents(t *testing.T) {
	pg := testPG(t)
	ctx := context.Background()
	worker := newTestEmployee(t, pg, "Worker", "Engineer")

	done := newTestTask(t, pg, "finishing now", "needs_review", worker, "")
	setTaskColumns(t, pg, done, "result = 'ok'")
	other := newTestTask(t, pg, "still open", "ready", worker, "")
	ripe := newTestTask(t, pg, "only dep finishing", "todo", worker, "")
	waiting := newTestTask(t, pg, "still waiting on other", "todo", worker, "")

	for _, dep := range [][2]string{{ripe, done}, {waiting, done}, {waiting, other}} {
		if _, err := pg.Pool().Exec(ctx,
			"INSERT INTO task_dependencies (task_id, depends_on) VALUES ($1, $2)", dep[0], dep[1]); err != nil {
			t.Fatalf("insert dependency: %v", err)
		}
	}

	if err := pg.UpdateTaskStatus(ctx, done, "done", ""); err != nil {
		t.Fatalf("complete task: %v", err)
	}

	if got := getTaskRow(t, pg, ripe).Status; got != "ready" {
		t.Errorf("dependent with all deps done = %q, want ready (promoted)", got)
	}
	if got := getTaskRow(t, pg, waiting).Status; got != "todo" {
		t.Errorf("dependent with an unfinished dep = %q, want todo (not promoted)", got)
	}
}

// A needs_review → ready rejection increments rejection_count, clears the
// result for the redo, and at the cap leaves the audit comment that pairs with
// claimReviewTasks refusing to re-arm the auto-reviewer (plan 1.6).
func TestUpdateTaskStatus_RejectionCountAndCap(t *testing.T) {
	pg := testPG(t)
	ctx := context.Background()
	manager := newTestEmployee(t, pg, "Manager", "PM")
	worker := newTestEmployee(t, pg, "Worker", "Engineer")

	id := newTestTask(t, pg, "rejected work", "needs_review", worker, manager)
	setTaskColumns(t, pg, id, "result = 'first try', rejection_count = 2")

	if err := pg.UpdateTaskStatus(ctx, id, "ready", manager, "not good enough"); err != nil {
		t.Fatalf("reject: %v", err)
	}
	row := getTaskRow(t, pg, id)
	if row.Status != "ready" || row.Result != "" || row.Rejections != 3 {
		t.Errorf("after capping rejection: status=%q result=%q rejections=%d, want ready/\"\"/3",
			row.Status, row.Result, row.Rejections)
	}
	comments := strings.Join(taskComments(t, pg, id), "\n")
	if !strings.Contains(comments, "not good enough") {
		t.Errorf("rejection feedback must be persisted as a comment, got %q", comments)
	}
	if !strings.Contains(comments, "human review") {
		t.Errorf("capping rejection must leave the human-review audit comment, got %q", comments)
	}
}

// Unblocking is a fresh start: failure counter and backoff are reset so one
// more failure doesn't instantly re-block the task.
func TestUpdateTaskStatus_UnblockResetsFailureState(t *testing.T) {
	pg := testPG(t)
	ctx := context.Background()
	worker := newTestEmployee(t, pg, "Worker", "Engineer")

	id := newTestTask(t, pg, "parked", "blocked", worker, "")
	setTaskColumns(t, pg, id, "failure_count = 3, retry_after = NOW() + INTERVAL '1 hour'")

	if err := pg.UpdateTaskStatus(ctx, id, "ready", ""); err != nil {
		t.Fatalf("unblock: %v", err)
	}
	row := getTaskRow(t, pg, id)
	if row.Status != "ready" || row.FailureCount != 0 || row.RetryAfter != nil {
		t.Errorf("after unblock: status=%q failures=%d retry=%v, want ready/0/nil",
			row.Status, row.FailureCount, row.RetryAfter)
	}
}

// Unblocking a task whose dependencies are still open must route it to todo,
// not ready — otherwise it would dispatch before its inputs exist.
func TestUpdateTaskStatus_UnblockWithOpenDepsGoesTodo(t *testing.T) {
	pg := testPG(t)
	ctx := context.Background()
	worker := newTestEmployee(t, pg, "Worker", "Engineer")

	dep := newTestTask(t, pg, "unfinished dep", "ready", worker, "")
	id := newTestTask(t, pg, "blocked with dep", "blocked", worker, "")
	if _, err := pg.Pool().Exec(ctx,
		"INSERT INTO task_dependencies (task_id, depends_on) VALUES ($1, $2)", id, dep); err != nil {
		t.Fatalf("insert dependency: %v", err)
	}

	if err := pg.UpdateTaskStatus(ctx, id, "ready", ""); err != nil {
		t.Fatalf("unblock: %v", err)
	}
	if got := getTaskRow(t, pg, id).Status; got != "todo" {
		t.Errorf("unblocked task with open deps = %q, want todo", got)
	}
}
