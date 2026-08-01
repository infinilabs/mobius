import { AlertCircle, Clock, Timer } from 'lucide-react';
import type { Task } from '../../types';
import { PRIORITY_STYLES, initials, timeAgo } from './taskMeta';

export function TaskCard({ task, onClick }: { task: Task; onClick: () => void }) {
  const isReview = task.status === 'needs_review';
  const isScheduled = task.is_scheduled;

  return (
    <button
      onClick={onClick}
      className={`w-full text-left p-3 rounded-lg border transition-all cursor-pointer hover:border-zinc-600/60 ${
        isReview
          ? 'border-violet-700/40 shadow-[0_0_8px_rgba(168,85,247,0.15)]'
          : isScheduled
          ? 'border-amber-700/40 shadow-[0_0_8px_rgba(217,119,6,0.1)]'
          : 'border-zinc-800/60'
      }`}
      style={{ background: '#111114' }}
    >
      <div className="flex items-center justify-between">
        <p className="text-sm font-medium text-zinc-200 truncate">{task.title}</p>
        {isScheduled && <Timer size={14} className="text-amber-400 shrink-0 ml-2" />}
      </div>

      {isScheduled && task.cron_expr && (
        <div className="mt-1.5 space-y-0.5">
          <p className="text-[10px] text-amber-400/80 font-mono">{task.cron_expr}</p>
          {task.next_run_at && (
            <p className="text-[10px] text-zinc-500">
              Next: {new Date(task.next_run_at).toLocaleString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })}
            </p>
          )}
          {!task.next_run_at && <p className="text-[10px] text-zinc-600 italic">Paused</p>}
        </div>
      )}

      <div className="flex items-center gap-2 mt-2 flex-wrap">
        <span className={`text-[10px] px-1.5 py-0.5 rounded border ${PRIORITY_STYLES[task.priority]}`}>
          {task.priority}
        </span>
        {task.project_name && (
          <span className="text-[10px] px-1.5 py-0.5 rounded bg-indigo-900/40 text-indigo-300 border border-indigo-700/40 truncate max-w-[100px]">
            {task.project_name}
          </span>
        )}
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
        {isScheduled && task.repeat_times != null && (
          <span className="text-[10px] text-zinc-500">{task.repeat_times} left</span>
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
