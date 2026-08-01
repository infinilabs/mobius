import { Activity, ChevronRight, Ban, Play, ExternalLink } from 'lucide-react';
import type { Task } from '../../types';

const TASK_STATUS_META: Record<string, { label: string; color: string }> = {
  todo: { label: 'To do', color: '#a1a1aa' },
  ready: { label: 'Ready', color: '#60a5fa' },
  in_progress: { label: 'In progress', color: '#38bdf8' },
  needs_review: { label: 'Needs review', color: '#fbbf24' },
  done: { label: 'Done', color: '#4ade80' },
  blocked: { label: 'Blocked', color: '#fb7185' },
  scheduled: { label: 'Scheduled', color: '#c084fc' },
};

const TERMINAL_STATUSES = new Set(['done', 'blocked']);

// Display order for the status-count strip (most actionable first).
const STATUS_DISPLAY_ORDER = ['in_progress', 'needs_review', 'ready', 'scheduled', 'todo', 'blocked', 'done'];

export function TaskStatusStrip({ tasks, onOpen }: { tasks: Task[]; onOpen?: () => void }) {
  if (tasks.length === 0) return null;
  const counts: Record<string, number> = {};
  for (const t of tasks) counts[t.status] = (counts[t.status] || 0) + 1;
  const active = tasks.filter(t => !TERMINAL_STATUSES.has(t.status)).length;
  const stages = STATUS_DISPLAY_ORDER.filter(s => counts[s] > 0);

  return (
    <div className="shrink-0 px-8 pt-3">
      <button
        onClick={onOpen}
        disabled={!onOpen}
        className={`group w-full max-w-[800px] mx-auto block rounded-lg border border-zinc-800/40 px-3 py-2 text-left transition-colors ${onOpen ? 'cursor-pointer hover:border-cyan-700/40' : 'cursor-default'}`}
        style={{ background: '#0c0c0f' }}
        title={onOpen ? 'View these in Tasks' : undefined}
      >
        <div className="flex items-center gap-3">
          <span className="flex items-center gap-1.5 shrink-0">
            <Activity size={12} className="text-cyan-400" />
            <span className="text-[11px] font-medium text-zinc-400">
              {tasks.length} {tasks.length === 1 ? 'activity' : 'activities'}{active > 0 ? ` · ${active} active` : ''}
            </span>
          </span>
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1 min-w-0">
            {stages.map(s => {
              const meta = TASK_STATUS_META[s] || { label: s, color: '#a1a1aa' };
              return (
                <span key={s} className="flex items-center gap-1.5 text-[11px]">
                  <span className={`w-1.5 h-1.5 rounded-full shrink-0 ${s === 'in_progress' ? 'animate-pulse' : ''}`} style={{ background: meta.color }} />
                  <span className="text-zinc-500">{meta.label}</span>
                  <span className="font-semibold" style={{ color: meta.color }}>{counts[s]}</span>
                </span>
              );
            })}
          </div>
          {onOpen && (
            <ChevronRight size={13} className="text-zinc-600 group-hover:text-cyan-400 transition-colors ml-auto shrink-0" />
          )}
        </div>
      </button>
    </div>
  );
}

export function BlockedTasksCallout({ tasks, reasons, onOpen, onUnblock }: {
  tasks: Task[];
  reasons: Record<string, string>;
  onOpen?: (taskId: string) => void;
  onUnblock: (taskId: string) => void;
}) {
  const blocked = tasks.filter(t => t.status === 'blocked');
  if (blocked.length === 0) return null;
  return (
    <div className="shrink-0 px-8 pt-2">
      <div className="w-full max-w-[800px] mx-auto rounded-lg border border-red-900/40 px-3 py-2" style={{ background: '#190f10' }}>
        <div className="flex items-center gap-1.5 mb-2">
          <Ban size={12} className="text-red-400 shrink-0" />
          <span className="text-[11px] font-medium text-red-300">
            {blocked.length} blocked — needs your attention
          </span>
        </div>
        <div className="space-y-1.5">
          {blocked.map(t => {
            const reason = reasons[t.id]
              || (t.failure_count > 0 ? `Failed ${t.failure_count}× — max retries exceeded` : 'Blocked — awaiting unblock');
            return (
              <div key={t.id} className="flex items-center gap-2">
                <div className="min-w-0 flex-1">
                  <p className="text-[11px] text-zinc-200 truncate">{t.title}</p>
                  <p className="text-[10px] text-red-400/80 truncate" title={reason}>{reason}</p>
                </div>
                {onOpen && (
                  <button
                    onClick={() => onOpen(t.id)}
                    className="flex items-center gap-1 text-[10px] px-2 py-1 rounded-md border border-zinc-700/50 text-zinc-300 hover:border-zinc-600 cursor-pointer shrink-0"
                  >
                    <ExternalLink size={10} /> Open
                  </button>
                )}
                <button
                  onClick={() => onUnblock(t.id)}
                  className="flex items-center gap-1 text-[10px] px-2 py-1 rounded-md border border-cyan-700/50 text-cyan-300 hover:bg-cyan-900/20 cursor-pointer shrink-0"
                >
                  <Play size={10} /> Unblock
                </button>
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
}
