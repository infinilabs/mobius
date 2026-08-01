package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

// failTransition is the guard that routes failTask through the task state
// machine (plan 1.4): a late failure may only act on a task the run still owns
// (in_progress). The high-stakes case is needs_review — a provider error
// arriving after a successful submit must not discard the submission.
func TestFailTransition(t *testing.T) {
	tests := []struct {
		name     string
		status   string
		failures int
		wantNext string
		wantOK   bool
	}{
		{"in_progress below cap retries", "in_progress", 1, "ready", true},
		{"in_progress at cap blocks", "in_progress", 3, "blocked", true},
		{"needs_review keeps submitted result", "needs_review", 1, "", false},
		{"done is terminal", "done", 1, "", false},
		{"ready already reclaimed", "ready", 1, "", false},
		{"blocked already parked", "blocked", 4, "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			next, ok := failTransition(tc.status, tc.failures)
			if ok != tc.wantOK || next != tc.wantNext {
				t.Errorf("failTransition(%q, %d) = (%q, %v), want (%q, %v)",
					tc.status, tc.failures, next, ok, tc.wantNext, tc.wantOK)
			}
		})
	}
}

// --- DB-backed dispatcher integration tests (plan 5.1; harness in pgtest_test.go) ---

// claimReadyTasks is the fallback-sweep claim path. Its predicate (ready +
// assignee + retry_after elapsed) is what keeps failed tasks in their backoff
// window and unassigned tasks out of the dispatcher entirely.
func TestClaimReadyTasks(t *testing.T) {
	pg := testPG(t)
	d := newTestDispatcher(pg)
	ctx := context.Background()
	worker := newTestEmployee(t, pg, "Worker", "Engineer")

	claimable := newTestTask(t, pg, "claimable", "ready", worker, "")
	backoff := newTestTask(t, pg, "in backoff", "ready", worker, "")
	setTaskColumns(t, pg, backoff, "retry_after = NOW() + INTERVAL '1 hour'")
	elapsed := newTestTask(t, pg, "backoff elapsed", "ready", worker, "")
	setTaskColumns(t, pg, elapsed, "retry_after = NOW() - INTERVAL '1 second'")
	unassigned := newTestTask(t, pg, "unassigned", "ready", "", "")
	newTestTask(t, pg, "not ready", "todo", worker, "")

	tasks, err := d.claimReadyTasks(ctx)
	if err != nil {
		t.Fatalf("claimReadyTasks: %v", err)
	}

	claimed := map[string]bool{}
	for _, task := range tasks {
		claimed[task.ID] = true
	}
	if !claimed[claimable] || !claimed[elapsed] {
		t.Errorf("expected claimable+elapsed tasks claimed, got %v", claimed)
	}
	if claimed[backoff] {
		t.Error("task inside its retry_after backoff window must not be claimed")
	}
	if claimed[unassigned] {
		t.Error("unassigned task must not be claimed")
	}
	if got := getTaskRow(t, pg, claimable).Status; got != "in_progress" {
		t.Errorf("claimed task status = %q, want in_progress", got)
	}
	if got := getTaskRow(t, pg, backoff).Status; got != "ready" {
		t.Errorf("backoff task status = %q, want ready (untouched)", got)
	}
}

// A row locked by a concurrent claimer must be skipped (FOR UPDATE SKIP
// LOCKED), not waited on — two dispatchers never double-claim or deadlock.
func TestClaimReadyTasks_SkipsLockedRows(t *testing.T) {
	pg := testPG(t)
	d := newTestDispatcher(pg)
	ctx := context.Background()
	worker := newTestEmployee(t, pg, "Worker", "Engineer")
	id := newTestTask(t, pg, "locked elsewhere", "ready", worker, "")

	tx, err := pg.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin locking tx: %v", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SELECT id FROM tasks WHERE id = $1 FOR UPDATE", id); err != nil {
		t.Fatalf("lock row: %v", err)
	}

	tasks, err := d.claimReadyTasks(ctx)
	if err != nil {
		t.Fatalf("claimReadyTasks: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("claimed %d tasks while row was locked, want 0", len(tasks))
	}
}

// dispatchSingleTask is the reactive (LISTEN/NOTIFY) path; plan 1.2's fix was
// making it honor retry_after like the sweep does.
func TestDispatchSingleTask_HonorsRetryAfter(t *testing.T) {
	pg := testPG(t)
	d := newTestDispatcher(pg)
	ctx := context.Background()
	worker := newTestEmployee(t, pg, "Worker", "Engineer")
	id := newTestTask(t, pg, "reactive in backoff", "ready", worker, "")
	setTaskColumns(t, pg, id, "retry_after = NOW() + INTERVAL '1 hour'")

	d.dispatchSingleTask(ctx, id)
	d.wg.Wait()

	if got := getTaskRow(t, pg, id).Status; got != "ready" {
		t.Errorf("task in backoff window was dispatched (status %q), want ready", got)
	}
}

// With an empty adapter registry the claimed task's run fails fast, so a
// single dispatch drives the full claim → execute → failTask cycle: the task
// returns to ready with one counted failure and a backoff window.
func TestDispatchSingleTask_ClaimsAndCountsFailure(t *testing.T) {
	pg := testPG(t)
	d := newTestDispatcher(pg)
	ctx := context.Background()
	worker := newTestEmployee(t, pg, "Worker", "Engineer")
	id := newTestTask(t, pg, "reactive claim", "ready", worker, "")

	d.dispatchSingleTask(ctx, id)
	d.wg.Wait()

	row := getTaskRow(t, pg, id)
	if row.Status != "ready" || row.FailureCount != 1 {
		t.Errorf("after failed run: status=%q failures=%d, want ready/1", row.Status, row.FailureCount)
	}
	if row.RetryAfter == nil || !row.RetryAfter.After(time.Now()) {
		t.Errorf("failed task must get a future retry_after, got %v", row.RetryAfter)
	}
}

// The stale reclaimer recovers in_progress tasks whose monitor died (crash or
// kill): backoff + retry below the failure cap, blocked at the cap. A task
// whose monitor is still touching updated_at is left alone.
func TestReclaimStaleTasks(t *testing.T) {
	pg := testPG(t)
	d := newTestDispatcher(pg)
	ctx := context.Background()
	worker := newTestEmployee(t, pg, "Worker", "Engineer")

	stale := newTestTask(t, pg, "stale", "in_progress", worker, "")
	setTaskColumns(t, pg, stale, "updated_at = NOW() - INTERVAL '10 minutes'")
	atCap := newTestTask(t, pg, "stale at cap", "in_progress", worker, "")
	setTaskColumns(t, pg, atCap, "updated_at = NOW() - INTERVAL '10 minutes', failure_count = 2")
	fresh := newTestTask(t, pg, "fresh", "in_progress", worker, "")

	d.reclaimStaleTasks(ctx)

	row := getTaskRow(t, pg, stale)
	if row.Status != "ready" || row.FailureCount != 1 {
		t.Errorf("stale task: status=%q failures=%d, want ready/1", row.Status, row.FailureCount)
	}
	if row.RetryAfter == nil || !row.RetryAfter.After(time.Now()) {
		t.Errorf("reclaimed task must get a future retry_after, got %v", row.RetryAfter)
	}
	if got := getTaskRow(t, pg, atCap); got.Status != "blocked" || got.FailureCount != 3 {
		t.Errorf("at-cap task: status=%q failures=%d, want blocked/3", got.Status, got.FailureCount)
	}
	if got := getTaskRow(t, pg, fresh).Status; got != "in_progress" {
		t.Errorf("fresh in_progress task reclaimed (status %q), want in_progress", got)
	}
	if comments := taskComments(t, pg, stale); len(comments) == 0 || !strings.Contains(comments[0], "reclaimed") {
		t.Errorf("reclaim must leave an audit comment, got %v", comments)
	}
}

// Scheduled templates: a due interval template materializes a ready child and
// advances next_run_at; a one-shot deactivates after its single child; a
// template far past its grace window fast-forwards without running.
func TestSweepScheduledTasks(t *testing.T) {
	pg := testPG(t)
	d := newTestDispatcher(pg)
	ctx := context.Background()
	worker := newTestEmployee(t, pg, "Worker", "Engineer")

	childCount := func(parent string) int {
		var n int
		if err := pg.pool.QueryRow(ctx,
			"SELECT COUNT(*) FROM tasks WHERE parent_task_id = $1", parent).Scan(&n); err != nil {
			t.Fatalf("count children: %v", err)
		}
		return n
	}
	mkTemplate := func(title, cronExpr string, nextRunOffset string) string {
		id := newTestTask(t, pg, title, "scheduled", worker, "")
		setTaskColumns(t, pg, id,
			"is_scheduled = TRUE, cron_expr = $1, next_run_at = NOW() + $2::interval", cronExpr, nextRunOffset)
		return id
	}

	interval := mkTemplate("hourly report", "every 1 hour", "-1 minute")
	oneShot := mkTemplate("one shot", time.Now().Add(-time.Minute).Format(time.RFC3339), "-1 minute")
	pastGrace := mkTemplate("missed window", "every 5 minutes", "-2 hours")
	notDue := mkTemplate("not due", "every 1 hour", "30 minutes")

	d.sweepScheduledTasks(ctx)

	if n := childCount(interval); n != 1 {
		t.Errorf("interval template materialized %d children, want 1", n)
	}
	row := getTaskRow(t, pg, interval)
	if !row.IsScheduled || row.NextRunAt == nil || !row.NextRunAt.After(time.Now()) {
		t.Errorf("interval template must stay scheduled with future next_run_at, got scheduled=%v next=%v",
			row.IsScheduled, row.NextRunAt)
	}
	var childStatus, childAssignee string
	if err := pg.pool.QueryRow(ctx,
		"SELECT status, assignee_id FROM tasks WHERE parent_task_id = $1", interval,
	).Scan(&childStatus, &childAssignee); err != nil {
		t.Fatalf("read child: %v", err)
	}
	if childStatus != "ready" || childAssignee != worker {
		t.Errorf("child = %q/%q, want ready/%q", childStatus, childAssignee, worker)
	}

	if n := childCount(oneShot); n != 1 {
		t.Errorf("one-shot template materialized %d children, want 1", n)
	}
	if row := getTaskRow(t, pg, oneShot); row.IsScheduled || row.NextRunAt != nil {
		t.Errorf("one-shot template must deactivate after running, got scheduled=%v next=%v",
			row.IsScheduled, row.NextRunAt)
	}

	if n := childCount(pastGrace); n != 0 {
		t.Errorf("past-grace template must fast-forward without children, got %d", n)
	}
	if row := getTaskRow(t, pg, pastGrace); !row.IsScheduled || row.NextRunAt == nil || !row.NextRunAt.After(time.Now()) {
		t.Errorf("past-grace template must stay scheduled with advanced next_run_at, got scheduled=%v next=%v",
			row.IsScheduled, row.NextRunAt)
	}

	if n := childCount(notDue); n != 0 {
		t.Errorf("not-due template must not materialize, got %d children", n)
	}
}

// A repeat-limited template deactivates once its repeat count is exhausted.
func TestSweepScheduledTasks_RepeatExhaustion(t *testing.T) {
	pg := testPG(t)
	d := newTestDispatcher(pg)
	ctx := context.Background()
	worker := newTestEmployee(t, pg, "Worker", "Engineer")

	id := newTestTask(t, pg, "last repeat", "scheduled", worker, "")
	setTaskColumns(t, pg, id,
		"is_scheduled = TRUE, cron_expr = 'every 1 hour', next_run_at = NOW() - INTERVAL '1 minute', repeat_times = 1")

	d.sweepScheduledTasks(ctx)

	row := getTaskRow(t, pg, id)
	if row.IsScheduled || row.NextRunAt != nil {
		t.Errorf("exhausted template must deactivate, got scheduled=%v next=%v", row.IsScheduled, row.NextRunAt)
	}
	var n int
	if err := pg.pool.QueryRow(ctx, "SELECT COUNT(*) FROM tasks WHERE parent_task_id = $1", id).Scan(&n); err != nil {
		t.Fatalf("count children: %v", err)
	}
	if n != 1 {
		t.Errorf("exhausted template must still run its final child, got %d", n)
	}
}

// salvageOrFail (plan 1.5): a timed-out run's partial output is submitted for
// review instead of discarded; only a truly empty run fails the task.
func TestSalvageOrFail(t *testing.T) {
	pg := testPG(t)
	d := newTestDispatcher(pg)
	ctx := context.Background()
	worker := newTestEmployee(t, pg, "Worker", "Engineer")

	withOutput := newTestTask(t, pg, "salvageable", "in_progress", worker, "")
	d.salvageOrFail(ctx, withOutput, "partial deliverable text")
	row := getTaskRow(t, pg, withOutput)
	if row.Status != "needs_review" || row.Result != "partial deliverable text" {
		t.Errorf("salvage: status=%q result=%q, want needs_review with the partial output", row.Status, row.Result)
	}

	empty := newTestTask(t, pg, "nothing to salvage", "in_progress", worker, "")
	d.salvageOrFail(ctx, empty, "")
	row = getTaskRow(t, pg, empty)
	if row.Status != "ready" || row.FailureCount != 1 {
		t.Errorf("empty salvage: status=%q failures=%d, want ready/1 (failTask path)", row.Status, row.FailureCount)
	}
}

// claimReviewTasks arms the creator as auto-reviewer, debounces re-dispatch
// via retry_after, and refuses tasks past the rejection cap (plan 1.6).
func TestClaimReviewTasks(t *testing.T) {
	pg := testPG(t)
	d := newTestDispatcher(pg)
	ctx := context.Background()
	manager := newTestEmployee(t, pg, "Manager", "PM")
	worker := newTestEmployee(t, pg, "Worker", "Engineer")

	reviewable := newTestTask(t, pg, "awaiting review", "needs_review", worker, manager)
	setTaskColumns(t, pg, reviewable, "result = 'the deliverable'")
	capped := newTestTask(t, pg, "rejected too often", "needs_review", worker, manager)
	setTaskColumns(t, pg, capped, "result = 'x', rejection_count = 3")
	orphan := newTestTask(t, pg, "no creator", "needs_review", worker, "")
	setTaskColumns(t, pg, orphan, "result = 'x'")

	tasks, err := d.claimReviewTasks(ctx)
	if err != nil {
		t.Fatalf("claimReviewTasks: %v", err)
	}
	if len(tasks) != 1 || tasks[0].ID != reviewable {
		ids := make([]string, len(tasks))
		for i, task := range tasks {
			ids[i] = task.ID
		}
		t.Fatalf("claimed %v, want exactly [%s] (capped + creatorless skipped)", ids, reviewable)
	}
	row := getTaskRow(t, pg, reviewable)
	if row.Status != "needs_review" {
		t.Errorf("claimed review task status = %q, must stay needs_review", row.Status)
	}
	if row.RetryAfter == nil || !row.RetryAfter.After(time.Now()) {
		t.Errorf("claimed review task must be debounced with future retry_after, got %v", row.RetryAfter)
	}

	// While debounced it must not be claimable again.
	again, err := d.claimReviewTasks(ctx)
	if err != nil {
		t.Fatalf("second claimReviewTasks: %v", err)
	}
	if len(again) != 0 {
		t.Errorf("debounced review task was claimed twice")
	}
}

// resumeBlockedTask (plan 1.3): a resolved interaction unblocks a blocked task
// (fresh failure counter, ready again); tasks that already moved on are left alone.
func TestResumeBlockedTask(t *testing.T) {
	pg := testPG(t)
	d := newTestDispatcher(pg)
	ctx := context.Background()
	worker := newTestEmployee(t, pg, "Worker", "Engineer")

	blocked := newTestTask(t, pg, "waiting on answer", "blocked", worker, "")
	setTaskColumns(t, pg, blocked, "failure_count = 3, retry_after = NOW() + INTERVAL '1 hour'")
	d.resumeBlockedTask(ctx, blocked)
	row := getTaskRow(t, pg, blocked)
	if row.Status != "ready" || row.FailureCount != 0 || row.RetryAfter != nil {
		t.Errorf("unblocked task: status=%q failures=%d retry=%v, want ready/0/nil (fresh start)",
			row.Status, row.FailureCount, row.RetryAfter)
	}

	moved := newTestTask(t, pg, "already running", "in_progress", worker, "")
	d.resumeBlockedTask(ctx, moved)
	if got := getTaskRow(t, pg, moved).Status; got != "in_progress" {
		t.Errorf("non-blocked task touched by resume (status %q), want in_progress", got)
	}
}
