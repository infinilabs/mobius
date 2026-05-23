import { useState, useEffect, useCallback } from 'react';
import { Plus, X, MessageSquare, ChevronRight, AlertCircle, CheckCircle2, Clock, ArrowRight, Trash2 } from 'lucide-react';
import {
  listTasks, createTask, getTask, updateTask, deleteTask, updateTaskStatus,
  listTaskComments, addTaskComment, listEmployees,
} from '../api';
import type { Task, TaskComment, Employee } from '../types';

const STATUS_COLUMNS: { key: Task['status']; label: string; color: string }[] = [
  { key: 'todo',         label: 'Todo',          color: 'text-zinc-400' },
  { key: 'ready',        label: 'Ready',         color: 'text-cyan-400' },
  { key: 'in_progress',  label: 'In Progress',   color: 'text-blue-400' },
  { key: 'needs_review', label: 'Needs Review',  color: 'text-violet-400' },
  { key: 'done',         label: 'Done',          color: 'text-emerald-400' },
  { key: 'blocked',      label: 'Blocked',       color: 'text-red-400' },
];

const PRIORITY_STYLES: Record<string, string> = {
  urgent: 'bg-red-900/50 text-red-300 border-red-700/50',
  high:   'bg-orange-900/50 text-orange-300 border-orange-700/50',
  medium: 'bg-zinc-800 text-zinc-400 border-zinc-600',
  low:    'bg-zinc-800/50 text-zinc-500 border-zinc-700/50',
};

const PRIORITY_ORDER: Record<string, number> = { urgent: 0, high: 1, medium: 2, low: 3 };

function initials(name: string) {
  return name.split(' ').map(w => w[0]).join('').slice(0, 2).toUpperCase();
}

function timeAgo(ts: string) {
  const d = Date.now() - new Date(ts).getTime();
  const mins = Math.floor(d / 60000);
  if (mins < 1) return 'just now';
  if (mins < 60) return `${mins}m ago`;
  const hrs = Math.floor(mins / 60);
  if (hrs < 24) return `${hrs}h ago`;
  return `${Math.floor(hrs / 24)}d ago`;
}

export default function TasksPage() {
  const [tasks, setTasks] = useState<Task[]>([]);
  const [employees, setEmployees] = useState<Employee[]>([]);
  const [, setLoading] = useState(true);
  const [showCreate, setShowCreate] = useState(false);
  const [selectedTaskId, setSelectedTaskId] = useState<string | null>(null);

  const refresh = useCallback(() => {
    setLoading(true);
    listTasks()
      .then(setTasks)
      .catch(() => setTasks([]))
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => {
    refresh();
    listEmployees().then(setEmployees).catch(() => {});
  }, [refresh]);

  const grouped: Record<Task['status'], Task[]> = {
    todo: [], ready: [], in_progress: [], needs_review: [], done: [], blocked: [],
  };
  for (const t of tasks) {
    grouped[t.status]?.push(t);
  }
  for (const key of Object.keys(grouped) as Task['status'][]) {
    grouped[key].sort((a, b) => (PRIORITY_ORDER[a.priority] ?? 9) - (PRIORITY_ORDER[b.priority] ?? 9));
  }

  return (
    <div className="h-full flex flex-col p-6">
      {/* Header */}
      <div className="flex items-center justify-between mb-6 shrink-0">
        <div>
          <h1 className="text-xl font-bold text-white">Tasks</h1>
          <p className="text-xs text-zinc-500 mt-0.5">{tasks.length} total</p>
        </div>
        <button
          onClick={() => setShowCreate(true)}
          className="flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium text-white cursor-pointer transition-all hover:opacity-90"
          style={{ background: 'linear-gradient(135deg, #0e7490, #164e63)' }}
        >
          <Plus size={16} /> Create Task
        </button>
      </div>

      {/* Board */}
      <div className="flex-1 overflow-x-auto min-h-0">
        <div className="flex gap-4 h-full min-w-max">
          {STATUS_COLUMNS.map(col => {
            const colTasks = grouped[col.key];
            if (col.key === 'blocked' && colTasks.length === 0) return null;
            return (
              <div key={col.key} className="w-72 flex flex-col shrink-0">
                <div className="flex items-center gap-2 mb-3 px-1">
                  <span className={`text-sm font-semibold ${col.color}`}>{col.label}</span>
                  <span className="text-xs text-zinc-600 bg-zinc-800/60 px-1.5 py-0.5 rounded-full">
                    {colTasks.length}
                  </span>
                </div>
                <div className="flex-1 overflow-y-auto space-y-2 pr-1">
                  {colTasks.map(t => (
                    <TaskCard key={t.id} task={t} onClick={() => setSelectedTaskId(t.id)} />
                  ))}
                  {colTasks.length === 0 && (
                    <div className="text-xs text-zinc-700 text-center py-8 border border-dashed border-zinc-800/60 rounded-lg">
                      No tasks
                    </div>
                  )}
                </div>
              </div>
            );
          })}
        </div>
      </div>

      {showCreate && (
        <CreateTaskModal
          employees={employees}
          allTasks={tasks}
          onClose={() => setShowCreate(false)}
          onCreated={() => { setShowCreate(false); refresh(); }}
        />
      )}

      {selectedTaskId && (
        <TaskDetailModal
          taskId={selectedTaskId}
          employees={employees}
          onClose={() => setSelectedTaskId(null)}
          onChanged={() => refresh()}
        />
      )}
    </div>
  );
}

function TaskCard({ task, onClick }: { task: Task; onClick: () => void }) {
  const isReview = task.status === 'needs_review';

  return (
    <button
      onClick={onClick}
      className={`w-full text-left p-3 rounded-lg border transition-all cursor-pointer hover:border-zinc-600/60 ${
        isReview
          ? 'border-violet-700/40 shadow-[0_0_8px_rgba(168,85,247,0.15)]'
          : 'border-zinc-800/60'
      }`}
      style={{ background: '#111114' }}
    >
      <p className="text-sm font-medium text-zinc-200 truncate">{task.title}</p>

      <div className="flex items-center gap-2 mt-2">
        <span className={`text-[10px] px-1.5 py-0.5 rounded border ${PRIORITY_STYLES[task.priority]}`}>
          {task.priority}
        </span>
        {task.dependencies.length > 0 && task.status === 'todo' && (
          <span className="text-[10px] text-amber-400 flex items-center gap-0.5">
            <AlertCircle size={10} /> {task.dependencies.length} dep{task.dependencies.length > 1 ? 's' : ''}
          </span>
        )}
        {isReview && (
          <span className="text-[10px] text-violet-400 flex items-center gap-0.5 animate-pulse">
            <Clock size={10} /> Review
          </span>
        )}
      </div>

      <div className="flex items-center justify-between mt-2">
        {task.assignee ? (
          <div className="flex items-center gap-1.5">
            <div className="w-5 h-5 rounded-full bg-zinc-700 flex items-center justify-center text-[9px] font-bold text-zinc-300">
              {initials(task.assignee.name)}
            </div>
            <span className="text-[10px] text-zinc-500">{task.assignee.name}</span>
          </div>
        ) : (
          <span className="text-[10px] text-zinc-600 italic">Unassigned</span>
        )}
        <span className="text-[10px] text-zinc-600">{timeAgo(task.updated_at)}</span>
      </div>
    </button>
  );
}

function CreateTaskModal({ employees, allTasks, onClose, onCreated }: {
  employees: Employee[];
  allTasks: Task[];
  onClose: () => void;
  onCreated: () => void;
}) {
  const [title, setTitle] = useState('');
  const [body, setBody] = useState('');
  const [priority, setPriority] = useState('medium');
  const [assigneeId, setAssigneeId] = useState('');
  const [creatorId, setCreatorId] = useState('');
  const [deps, setDeps] = useState<string[]>([]);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  const handleSubmit = async () => {
    if (!title.trim()) { setError('Title is required'); return; }
    setSaving(true);
    setError('');
    try {
      await createTask({
        title: title.trim(),
        body: body.trim(),
        priority,
        assignee_id: assigneeId || undefined,
        creator_id: creatorId || undefined,
        dependencies: deps.length > 0 ? deps : undefined,
      });
      onCreated();
    } catch {
      setError('Failed to create task');
    } finally {
      setSaving(false);
    }
  };

  const openTasks = allTasks.filter(t => t.status !== 'done');

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4" style={{ background: 'rgba(0,0,0,0.7)' }}>
      <div className="w-full max-w-lg rounded-xl border border-zinc-800/60 shadow-2xl overflow-hidden" style={{ background: '#0f0f12' }}>
        <div className="flex items-center justify-between px-6 py-4 border-b border-zinc-800/40">
          <h2 className="text-lg font-semibold text-white">Create Task</h2>
          <button onClick={onClose} className="text-zinc-500 hover:text-zinc-300 cursor-pointer"><X size={18} /></button>
        </div>

        <div className="p-6 space-y-4 max-h-[70vh] overflow-y-auto">
          <div>
            <label className="text-xs text-zinc-500 uppercase tracking-wider block mb-1">Title</label>
            <input
              value={title}
              onChange={e => setTitle(e.target.value)}
              className="w-full px-3 py-2 rounded-lg border border-zinc-800 bg-zinc-900/50 text-sm text-zinc-200 outline-none focus:border-cyan-700/50"
              placeholder="What needs to be done?"
              autoFocus
            />
          </div>

          <div>
            <label className="text-xs text-zinc-500 uppercase tracking-wider block mb-1">Description</label>
            <textarea
              value={body}
              onChange={e => setBody(e.target.value)}
              className="w-full px-3 py-2 rounded-lg border border-zinc-800 bg-zinc-900/50 text-sm text-zinc-200 outline-none focus:border-cyan-700/50 resize-none"
              rows={3}
              placeholder="Details, goals, requirements..."
            />
          </div>

          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="text-xs text-zinc-500 uppercase tracking-wider block mb-1">Priority</label>
              <select
                value={priority}
                onChange={e => setPriority(e.target.value)}
                className="w-full px-3 py-2 rounded-lg border border-zinc-800 bg-zinc-900/50 text-sm text-zinc-200 outline-none cursor-pointer"
              >
                <option value="low">Low</option>
                <option value="medium">Medium</option>
                <option value="high">High</option>
                <option value="urgent">Urgent</option>
              </select>
            </div>
            <div>
              <label className="text-xs text-zinc-500 uppercase tracking-wider block mb-1">Assignee</label>
              <select
                value={assigneeId}
                onChange={e => setAssigneeId(e.target.value)}
                className="w-full px-3 py-2 rounded-lg border border-zinc-800 bg-zinc-900/50 text-sm text-zinc-200 outline-none cursor-pointer"
              >
                <option value="">Unassigned</option>
                {employees.map(e => (
                  <option key={e.id} value={e.id}>{e.name} — {e.role}</option>
                ))}
              </select>
            </div>
          </div>

          <div>
            <label className="text-xs text-zinc-500 uppercase tracking-wider block mb-1">Creator</label>
            <select
              value={creatorId}
              onChange={e => setCreatorId(e.target.value)}
              className="w-full px-3 py-2 rounded-lg border border-zinc-800 bg-zinc-900/50 text-sm text-zinc-200 outline-none cursor-pointer"
            >
              <option value="">None</option>
              {employees.map(e => (
                <option key={e.id} value={e.id}>{e.name} — {e.role}</option>
              ))}
            </select>
          </div>

          {openTasks.length > 0 && (
            <div>
              <label className="text-xs text-zinc-500 uppercase tracking-wider block mb-1">Dependencies (blocks until done)</label>
              <div className="space-y-1 max-h-32 overflow-y-auto border border-zinc-800 rounded-lg p-2 bg-zinc-900/30">
                {openTasks.map(t => (
                  <label key={t.id} className="flex items-center gap-2 text-xs text-zinc-400 cursor-pointer hover:text-zinc-200">
                    <input
                      type="checkbox"
                      checked={deps.includes(t.id)}
                      onChange={e => {
                        if (e.target.checked) setDeps(prev => [...prev, t.id]);
                        else setDeps(prev => prev.filter(d => d !== t.id));
                      }}
                      className="rounded border-zinc-700 cursor-pointer"
                    />
                    <span className="truncate">{t.title}</span>
                    <span className={`ml-auto shrink-0 text-[9px] px-1 rounded border ${PRIORITY_STYLES[t.priority]}`}>{t.priority}</span>
                  </label>
                ))}
              </div>
            </div>
          )}

          {error && <p className="text-xs text-red-400">{error}</p>}
        </div>

        <div className="flex justify-end gap-3 px-6 py-4 border-t border-zinc-800/40">
          <button onClick={onClose} className="px-4 py-2 text-sm text-zinc-400 hover:text-zinc-200 cursor-pointer">Cancel</button>
          <button
            onClick={handleSubmit}
            disabled={saving}
            className="px-4 py-2 rounded-lg text-sm font-medium text-white cursor-pointer transition-all hover:opacity-90 disabled:opacity-50"
            style={{ background: 'linear-gradient(135deg, #0e7490, #164e63)' }}
          >
            {saving ? 'Creating...' : 'Create'}
          </button>
        </div>
      </div>
    </div>
  );
}

function TaskDetailModal({ taskId, employees, onClose, onChanged }: {
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

interface TransitionAction {
  status: string;
  label: string;
  icon: React.ReactNode;
  style: string;
}

function getAvailableTransitions(task: Task): TransitionAction[] {
  const actions: TransitionAction[] = [];

  switch (task.status) {
    case 'ready':
      actions.push({
        status: 'in_progress', label: 'Start Work', icon: <ArrowRight size={12} />,
        style: 'bg-blue-900/30 text-blue-300 border-blue-700/50 hover:bg-blue-900/50',
      });
      actions.push({
        status: 'blocked', label: 'Block', icon: <AlertCircle size={12} />,
        style: 'bg-red-900/20 text-red-400 border-red-800/50 hover:bg-red-900/40',
      });
      break;
    case 'in_progress':
      if (task.result) {
        actions.push({
          status: 'needs_review', label: 'Submit for Review', icon: <ChevronRight size={12} />,
          style: 'bg-violet-900/30 text-violet-300 border-violet-700/50 hover:bg-violet-900/50',
        });
      }
      actions.push({
        status: 'ready', label: 'Pause', icon: <Clock size={12} />,
        style: 'bg-zinc-800/50 text-zinc-400 border-zinc-700/50 hover:bg-zinc-800',
      });
      actions.push({
        status: 'blocked', label: 'Block', icon: <AlertCircle size={12} />,
        style: 'bg-red-900/20 text-red-400 border-red-800/50 hover:bg-red-900/40',
      });
      break;
    case 'needs_review':
      actions.push({
        status: 'done', label: 'Approve', icon: <CheckCircle2 size={12} />,
        style: 'bg-emerald-900/30 text-emerald-300 border-emerald-700/50 hover:bg-emerald-900/50',
      });
      actions.push({
        status: 'ready', label: 'Reject', icon: <X size={12} />,
        style: 'bg-red-900/20 text-red-400 border-red-800/50 hover:bg-red-900/40',
      });
      break;
    case 'blocked':
      actions.push({
        status: 'ready', label: 'Unblock', icon: <ArrowRight size={12} />,
        style: 'bg-cyan-900/30 text-cyan-300 border-cyan-700/50 hover:bg-cyan-900/50',
      });
      break;
    case 'todo':
      actions.push({
        status: 'blocked', label: 'Block', icon: <AlertCircle size={12} />,
        style: 'bg-red-900/20 text-red-400 border-red-800/50 hover:bg-red-900/40',
      });
      break;
  }

  return actions;
}
