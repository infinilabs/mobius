import { useState } from 'react';
import { X, Timer } from 'lucide-react';
import { createTask } from '../../api';
import type { Task, Employee, Project } from '../../types';
import { PRIORITY_STYLES } from './taskMeta';

export function CreateTaskModal({ employees, projects, defaultProjectId, allTasks, onClose, onCreated }: {
  employees: Employee[];
  projects: Project[];
  defaultProjectId: string;
  allTasks: Task[];
  onClose: () => void;
  onCreated: () => void;
}) {
  const [title, setTitle] = useState('');
  const [body, setBody] = useState('');
  const [priority, setPriority] = useState('medium');
  const [assigneeId, setAssigneeId] = useState('');
  const [creatorId, setCreatorId] = useState('');
  const [projectId, setProjectId] = useState(defaultProjectId);
  const [deps, setDeps] = useState<string[]>([]);
  const [isScheduled, setIsScheduled] = useState(false);
  const [cronExpr, setCronExpr] = useState('');
  const [repeatTimes, setRepeatTimes] = useState('');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  const handleSubmit = async () => {
    if (!title.trim()) { setError('Title is required'); return; }
    if (isScheduled && !cronExpr.trim()) { setError('Schedule expression is required'); return; }
    setSaving(true);
    setError('');
    try {
      await createTask({
        title: title.trim(),
        body: body.trim(),
        priority,
        assignee_id: assigneeId || undefined,
        creator_id: creatorId || undefined,
        dependencies: isScheduled ? undefined : (deps.length > 0 ? deps : undefined),
        is_scheduled: isScheduled || undefined,
        cron_expr: isScheduled ? cronExpr.trim() : undefined,
        repeat_times: isScheduled && repeatTimes ? parseInt(repeatTimes, 10) : undefined,
        project_id: projectId || undefined,
      });
      onCreated();
    } catch (e: unknown) {
      const axErr = e as { response?: { data?: { error?: string } | string } };
      const d = axErr?.response?.data;
      const msg = typeof d === 'string' ? d : (typeof d === 'object' && d?.error ? d.error : 'Failed to create task');
      setError(msg);
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

          <div>
            <label className="text-xs text-zinc-500 uppercase tracking-wider block mb-1">Project</label>
            <select
              value={projectId}
              onChange={e => setProjectId(e.target.value)}
              className="w-full px-3 py-2 rounded-lg border border-zinc-800 bg-zinc-900/50 text-sm text-zinc-200 outline-none cursor-pointer"
            >
              <option value="">No Project (global)</option>
              {projects.map(p => (
                <option key={p.id} value={p.id}>{p.name}</option>
              ))}
            </select>
          </div>

          <div>
            <label className="flex items-center gap-2 cursor-pointer">
              <input
                type="checkbox"
                checked={isScheduled}
                onChange={e => setIsScheduled(e.target.checked)}
                className="rounded border-zinc-700 cursor-pointer"
              />
              <span className="text-xs text-zinc-400 flex items-center gap-1">
                <Timer size={12} /> Set Recurring Schedule
              </span>
            </label>

            {isScheduled && (
              <div className="mt-3 space-y-3 pl-6 border-l-2 border-amber-700/30">
                <div>
                  <label className="text-xs text-zinc-500 block mb-1">Schedule Expression</label>
                  <input
                    value={cronExpr}
                    onChange={e => setCronExpr(e.target.value)}
                    className="w-full px-3 py-2 rounded-lg border border-zinc-800 bg-zinc-900/50 text-sm text-zinc-200 outline-none focus:border-amber-700/50 font-mono"
                    placeholder="every 30m  |  0 9 * * 1-5  |  2026-06-01T09:00:00Z"
                  />
                  <p className="text-[10px] text-zinc-600 mt-1">
                    Interval: every 30m, every 2h &middot; Cron: 0 9 * * * &middot; One-shot: ISO timestamp
                  </p>
                </div>
                <div>
                  <label className="text-xs text-zinc-500 block mb-1">Repeat Count (blank = infinite)</label>
                  <input
                    type="number"
                    min="1"
                    value={repeatTimes}
                    onChange={e => setRepeatTimes(e.target.value)}
                    className="w-32 px-3 py-2 rounded-lg border border-zinc-800 bg-zinc-900/50 text-sm text-zinc-200 outline-none focus:border-amber-700/50"
                    placeholder="infinite"
                  />
                </div>
              </div>
            )}
          </div>

          {!isScheduled && openTasks.length > 0 && (
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
