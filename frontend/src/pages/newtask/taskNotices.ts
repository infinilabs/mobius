import type { Task, TaskComment } from '../../types';

// Live notices injected into the chat when a tracked task changes state, so the
// agent appears to narrate progress ("Elong: ✅ ... completed").
export const TASK_NOTICE: Record<string, (t: Task) => string> = {
  in_progress: t => `🚀 ${t.assignee?.name ?? 'The team'} started working on "${t.title}".`,
  needs_review: t => `📝 "${t.title}" is ready for your review.`,
  done: t => `✅ ${t.assignee?.name ?? 'The team'} completed "${t.title}".`,
  blocked: t => `⛔ "${t.title}" is blocked and needs your attention.`,
};

export function latestSystemError(comments: TaskComment[]): string {
  for (let i = comments.length - 1; i >= 0; i--) {
    const c = comments[i].content;
    if (c.startsWith('System Error:')) return c.replace('System Error:', '').trim();
  }
  return '';
}
