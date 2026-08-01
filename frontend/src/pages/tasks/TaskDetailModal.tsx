import { useState, useEffect, useCallback } from 'react';
import { X, MessageSquare, Trash2, Pause, Play } from 'lucide-react';
import {
  getTask, updateTask, deleteTask, updateTaskStatus,
  listTaskComments, addTaskComment, listTaskRuns, updateTaskSchedule, listEvents,
} from '../../api';
import type { Task, TaskComment, Employee, Event } from '../../types';
import { activityVerb, eventIcon, activityTimeAgo } from '../../activity';
import { STATUS_COLUMNS, PRIORITY_STYLES, initials, timeAgo, getAvailableTransitions } from './taskMeta';

export function TaskDetailModal({ taskId, employees, onClose, onChanged }: {
  taskId: string;
  employees: Employee[];
  onClose: () => void;
  onChanged: () => void;
}) {
  const [task, setTask] = useState<Task | null>(null);
  const [comments, setComments] = useState<TaskComment[]>([]);
  const [newComment, setNewComment] = useState('');
  const [commentAuthor, setCommentAuthor] = useState('');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [resultDraft, setResultDraft] = useState('');
  const [runs, setRuns] = useState<Task[]>([]);
  const [events, setEvents] = useState<Event[]>([]);
  const [editing, setEditing] = useState(false);
  const [editTitle, setEditTitle] = useState('');
  const [editBody, setEditBody] = useState('');
  const [editPriority, setEditPriority] = useState('');
  const [editAssignee, setEditAssignee] = useState('');

  const reload = useCallback(async () => {
    try {
      const [t, c] = await Promise.all([getTask(taskId), listTaskComments(taskId)]);
      setTask(t);
      setComments(c);
      setResultDraft(t.result);
      if (t.is_scheduled) {
        listTaskRuns(taskId).then(setRuns).catch(() => setRuns([]));
      }
      listEvents({ task_id: taskId, limit: 100 }).then(setEvents).catch(() => setEvents([]));
    } catch {
      setError('Failed to load task');
    } finally {
      setLoading(false);
    }
  }, [taskId]);

  useEffect(() => { reload(); }, [reload]);

  const handleStatusChange = async (status: string) => {
    setError('');
    let feedback: string | undefined;

    if (status === 'ready' && task?.status === 'needs_review') {
      const input = prompt('Please provide rejection feedback so the agent knows what to fix:');
      if (input === null) return;
      if (!input.trim()) {
        setError('Feedback is required when rejecting a task.');
        return;
      }
      feedback = input.trim();
    }

    try {
      await updateTaskStatus(taskId, status, commentAuthor || undefined, feedback);
      await reload();
      onChanged();
    } catch (e: unknown) {
      const axErr = e as { response?: { data?: string } };
      const msg = axErr?.response?.data || (e instanceof Error ? e.message : 'Status change failed');
      setError(typeof msg === 'string' ? msg : 'Status change failed');
    }
  };

  const handleSaveResult = async () => {
    setError('');
    try {
      await updateTask(taskId, { result: resultDraft });
      await reload();
      onChanged();
    } catch {
      setError('Failed to save result');
    }
  };

  const handleSaveEdit = async () => {
    setError('');
    try {
      await updateTask(taskId, {
        title: editTitle,
        body: editBody,
        priority: editPriority,
        assignee_id: editAssignee,
      });
      setEditing(false);
      await reload();
      onChanged();
    } catch {
      setError('Failed to update task');
    }
  };

  const handleDelete = async () => {
    try {
      await deleteTask(taskId);
      onClose();
      onChanged();
    } catch {
      setError('Failed to delete task');
    }
  };

  const handleAddComment = async () => {
    if (!newComment.trim()) return;
    try {
      await addTaskComment(taskId, commentAuthor, newComment.trim());
      setNewComment('');
      await reload();
    } catch {
      setError('Failed to add comment');
    }
  };

  const startEdit = () => {
    if (!task) return;
    setEditTitle(task.title);
    setEditBody(task.body);
    setEditPriority(task.priority);
    setEditAssignee(task.assignee?.id ?? '');
    setEditing(true);
  };

  if (loading || !task) {
    return (
      <div className="fixed inset-0 z-50 flex items-center justify-center" style={{ background: 'rgba(0,0,0,0.7)' }}>
        <div className="text-zinc-400 text-sm">Loading...</div>
      </div>
    );
  }

  const statusCol = STATUS_COLUMNS.find(c => c.key === task.status);
  const transitions = getAvailableTransitions(task);

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4" style={{ background: 'rgba(0,0,0,0.7)' }}>
      <div className="w-full max-w-4xl max-h-[85vh] rounded-xl border border-zinc-800/60 shadow-2xl flex flex-col overflow-hidden" style={{ background: '#0f0f12' }}>
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-zinc-800/40 shrink-0">
          <div className="flex items-center gap-3 min-w-0">
            <span className={`text-xs font-medium px-2 py-0.5 rounded border ${PRIORITY_STYLES[task.priority]}`}>
              {task.priority}
            </span>
            <span className={`text-xs font-medium ${statusCol?.color ?? 'text-zinc-400'}`}>
              {statusCol?.label ?? task.status}
            </span>
            {task.project_name && (
              <span className="text-xs text-indigo-300 bg-indigo-900/30 px-2 py-0.5 rounded border border-indigo-700/40">
                {task.project_name}
              </span>
            )}
          </div>
          <div className="flex items-center gap-2">
            {task.status !== 'done' && (
              <button onClick={startEdit} className="text-xs text-zinc-500 hover:text-zinc-300 cursor-pointer px-2 py-1 rounded hover:bg-zinc-800/50">
                Edit
              </button>
            )}
            <button onClick={handleDelete} className="text-zinc-600 hover:text-red-400 cursor-pointer p-1 rounded hover:bg-zinc-800/50">
              <Trash2 size={14} />
            </button>
            <button onClick={onClose} className="text-zinc-500 hover:text-zinc-300 cursor-pointer"><X size={18} /></button>
          </div>
        </div>

        {/* Body — two panes */}
        <div className="flex flex-1 min-h-0 overflow-hidden">
          {/* Left pane — task details */}
          <div className="flex-1 p-6 overflow-y-auto border-r border-zinc-800/40">
            {editing ? (
              <div className="space-y-4">
                <input value={editTitle} onChange={e => setEditTitle(e.target.value)}
                  className="w-full px-3 py-2 rounded-lg border border-zinc-800 bg-zinc-900/50 text-sm text-zinc-200 outline-none" />
                <textarea value={editBody} onChange={e => setEditBody(e.target.value)}
                  className="w-full px-3 py-2 rounded-lg border border-zinc-800 bg-zinc-900/50 text-sm text-zinc-200 outline-none resize-none" rows={4} />
                <div className="grid grid-cols-2 gap-3">
                  <select value={editPriority} onChange={e => setEditPriority(e.target.value)}
                    className="px-3 py-2 rounded-lg border border-zinc-800 bg-zinc-900/50 text-sm text-zinc-200 outline-none cursor-pointer">
                    <option value="low">Low</option><option value="medium">Medium</option>
                    <option value="high">High</option><option value="urgent">Urgent</option>
                  </select>
                  <select value={editAssignee} onChange={e => setEditAssignee(e.target.value)}
                    className="px-3 py-2 rounded-lg border border-zinc-800 bg-zinc-900/50 text-sm text-zinc-200 outline-none cursor-pointer">
                    <option value="">Unassigned</option>
                    {employees.map(emp => <option key={emp.id} value={emp.id}>{emp.name}</option>)}
                  </select>
                </div>
                <div className="flex gap-2">
                  <button onClick={handleSaveEdit}
                    className="px-3 py-1.5 rounded-lg text-xs font-medium text-white cursor-pointer" style={{ background: '#0e7490' }}>Save</button>
                  <button onClick={() => setEditing(false)}
                    className="px-3 py-1.5 text-xs text-zinc-500 hover:text-zinc-300 cursor-pointer">Cancel</button>
                </div>
              </div>
            ) : (
              <>
                <h2 className="text-lg font-bold text-white mb-2">{task.title}</h2>
                {task.body && <p className="text-sm text-zinc-400 whitespace-pre-wrap mb-4">{task.body}</p>}

                <div className="grid grid-cols-2 gap-4 mb-4">
                  <div>
                    <p className="text-[10px] text-zinc-600 uppercase tracking-wider mb-0.5">Assignee</p>
                    {task.assignee ? (
                      <div className="flex items-center gap-1.5">
                        <div className="w-5 h-5 rounded-full bg-zinc-700 flex items-center justify-center text-[9px] font-bold text-zinc-300">
                          {initials(task.assignee.name)}
                        </div>
                        <span className="text-xs text-zinc-300">{task.assignee.name}</span>
                        <span className="text-[10px] text-zinc-600">{task.assignee.role}</span>
                      </div>
                    ) : <span className="text-xs text-zinc-600 italic">Unassigned</span>}
                  </div>
                  <div>
                    <p className="text-[10px] text-zinc-600 uppercase tracking-wider mb-0.5">Creator</p>
                    {task.creator ? (
                      <span className="text-xs text-zinc-300">{task.creator.name}</span>
                    ) : <span className="text-xs text-zinc-600 italic">—</span>}
                  </div>
                </div>

                {task.dependencies.length > 0 && (
                  <div className="mb-4">
                    <p className="text-[10px] text-zinc-600 uppercase tracking-wider mb-1">Dependencies</p>
                    <div className="flex flex-wrap gap-1">
                      {task.dependencies.map(depId => (
                        <span key={depId} className="text-[10px] px-2 py-0.5 rounded bg-zinc-800 text-zinc-500 border border-zinc-700/50 font-mono">
                          {depId.slice(0, 8)}...
                        </span>
                      ))}
                    </div>
                  </div>
                )}

                {/* Schedule info + run history */}
                {task.is_scheduled && (
                  <div className="mb-4">
                    <div className="flex items-center justify-between mb-2">
                      <div>
                        <p className="text-[10px] text-zinc-600 uppercase tracking-wider mb-0.5">Schedule</p>
                        <p className="text-xs text-amber-400 font-mono">{task.cron_expr}</p>
                        {task.next_run_at ? (
                          <p className="text-[10px] text-zinc-500 mt-0.5">
                            Next: {new Date(task.next_run_at).toLocaleString()}
                          </p>
                        ) : (
                          <p className="text-[10px] text-zinc-600 italic mt-0.5">Paused</p>
                        )}
                        {task.repeat_times != null && (
                          <p className="text-[10px] text-zinc-500 mt-0.5">{task.repeat_times} runs remaining</p>
                        )}
                      </div>
                      <div className="flex gap-2">
                        {task.next_run_at ? (
                          <button
                            onClick={async () => {
                              await updateTaskSchedule(taskId, { is_scheduled: false });
                              await reload();
                              onChanged();
                            }}
                            className="flex items-center gap-1 px-2 py-1 rounded text-[10px] text-amber-400 border border-amber-700/40 hover:bg-amber-900/20 cursor-pointer"
                          >
                            <Pause size={10} /> Pause
                          </button>
                        ) : (
                          <button
                            onClick={async () => {
                              await updateTaskSchedule(taskId, { is_scheduled: true });
                              await reload();
                              onChanged();
                            }}
                            className="flex items-center gap-1 px-2 py-1 rounded text-[10px] text-emerald-400 border border-emerald-700/40 hover:bg-emerald-900/20 cursor-pointer"
                          >
                            <Play size={10} /> Resume
                          </button>
                        )}
                      </div>
                    </div>

                    {runs.length > 0 && (
                      <div className="mt-3">
                        <p className="text-[10px] text-zinc-600 uppercase tracking-wider mb-1">
                          Run History ({runs.length})
                        </p>
                        <div className="space-y-1 max-h-40 overflow-y-auto border border-zinc-800/40 rounded-lg p-2 bg-zinc-900/20">
                          {runs.map(run => {
                            const sc = STATUS_COLUMNS.find(c => c.key === run.status);
                            return (
                              <div key={run.id} className="flex items-center justify-between text-[10px] py-1">
                                <span className="text-zinc-400 truncate flex-1">{run.title}</span>
                                <span className={`ml-2 shrink-0 ${sc?.color ?? 'text-zinc-500'}`}>
                                  {sc?.label ?? run.status}
                                </span>
                              </div>
                            );
                          })}
                        </div>
                      </div>
                    )}
                  </div>
                )}

                {/* Result area */}
                {(task.status === 'in_progress' || task.status === 'needs_review' || task.status === 'done') && (
                  <div className="mb-4">
                    <p className="text-[10px] text-zinc-600 uppercase tracking-wider mb-1">Result</p>
                    {task.status === 'in_progress' ? (
                      <div className="space-y-2">
                        <textarea
                          value={resultDraft}
                          onChange={e => setResultDraft(e.target.value)}
                          className="w-full px-3 py-2 rounded-lg border border-zinc-800 bg-zinc-900/50 text-sm text-zinc-200 outline-none resize-none"
                          rows={4}
                          placeholder="Describe the work output..."
                        />
                        <button
                          onClick={handleSaveResult}
                          disabled={!resultDraft.trim()}
                          className="px-3 py-1.5 rounded-lg text-xs font-medium text-white cursor-pointer disabled:opacity-40"
                          style={{ background: '#0e7490' }}
                        >
                          Save Result
                        </button>
                      </div>
                    ) : (
                      <div className="px-3 py-2 rounded-lg border border-zinc-800 bg-zinc-900/30 text-sm text-zinc-300 whitespace-pre-wrap">
                        {task.result || <span className="text-zinc-600 italic">No result submitted</span>}
                      </div>
                    )}
                  </div>
                )}

                {/* Activity timeline — per-run step trace (tool calls + curated
                    domain events), newest first. */}
                <div className="mb-4">
                  <p className="text-[10px] text-zinc-600 uppercase tracking-wider mb-1">
                    Activity ({events.length})
                  </p>
                  {events.length > 0 ? (
                    <div className="space-y-1.5 max-h-60 overflow-y-auto border border-zinc-800/40 rounded-lg p-2 bg-zinc-900/20">
                      {events.map(ev => {
                        const { verb, detail } = activityVerb(ev);
                        return (
                          <div key={ev.id} className="flex items-start gap-2 text-[11px]">
                            <span className="text-zinc-500 mt-0.5 shrink-0">{eventIcon(ev.event_type)}</span>
                            <div className="min-w-0 flex-1">
                              <span className="text-zinc-300">{verb}</span>
                              {detail && <span className="text-zinc-500"> — {detail}</span>}
                            </div>
                            <span className="text-zinc-600 shrink-0">{activityTimeAgo(ev.timestamp)}</span>
                          </div>
                        );
                      })}
                    </div>
                  ) : (
                    <p className="text-[10px] text-zinc-700 italic">No activity recorded yet</p>
                  )}
                </div>
              </>
            )}

            {/* Status transitions */}
            {transitions.length > 0 && !editing && (
              <div className="mt-4 pt-4 border-t border-zinc-800/40">
                <p className="text-[10px] text-zinc-600 uppercase tracking-wider mb-2">Actions</p>
                <div className="flex flex-wrap gap-2">
                  {transitions.map(tr => (
                    <button
                      key={tr.status}
                      onClick={() => handleStatusChange(tr.status)}
                      className={`flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium cursor-pointer transition-all border ${tr.style}`}
                    >
                      {tr.icon} {tr.label}
                    </button>
                  ))}
                </div>
              </div>
            )}

            {error && <p className="text-xs text-red-400 mt-3">{error}</p>}
          </div>

          {/* Right pane — comments */}
          <div className="w-80 flex flex-col shrink-0">
            <div className="px-4 py-3 border-b border-zinc-800/40">
              <p className="text-xs font-semibold text-zinc-400 flex items-center gap-1.5">
                <MessageSquare size={12} /> Comments ({comments.length})
              </p>
            </div>

            <div className="flex-1 overflow-y-auto p-4 space-y-3">
              {comments.map(c => (
                <div key={c.id} className="text-xs">
                  <div className="flex items-center gap-1.5 mb-0.5">
                    <span className="font-medium text-zinc-300">{c.author?.name ?? 'System'}</span>
                    <span className="text-zinc-600">{timeAgo(c.created_at)}</span>
                  </div>
                  <p className="text-zinc-400 whitespace-pre-wrap">{c.content}</p>
                </div>
              ))}
              {comments.length === 0 && (
                <p className="text-xs text-zinc-700 text-center py-4">No comments yet</p>
              )}
            </div>

            <div className="p-4 border-t border-zinc-800/40 shrink-0 space-y-2">
              <select
                value={commentAuthor}
                onChange={e => setCommentAuthor(e.target.value)}
                className="w-full px-2 py-1.5 rounded-lg border border-zinc-800 bg-zinc-900/50 text-xs text-zinc-300 outline-none cursor-pointer"
              >
                <option value="">Comment as...</option>
                {employees.map(e => <option key={e.id} value={e.id}>{e.name}</option>)}
              </select>
              <div className="flex gap-2">
                <input
                  value={newComment}
                  onChange={e => setNewComment(e.target.value)}
                  onKeyDown={e => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); handleAddComment(); } }}
                  className="flex-1 px-2 py-1.5 rounded-lg border border-zinc-800 bg-zinc-900/50 text-xs text-zinc-200 outline-none"
                  placeholder="Add comment..."
                />
                <button
                  onClick={handleAddComment}
                  disabled={!newComment.trim()}
                  className="px-3 py-1.5 rounded-lg text-xs font-medium text-white cursor-pointer disabled:opacity-40"
                  style={{ background: '#0e7490' }}
                >
                  Send
                </button>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
