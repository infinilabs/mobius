import { useState, useEffect, useCallback, useMemo } from 'react';
import { LayoutDashboard, Kanban, Activity, ArrowRight } from 'lucide-react';
import { listTasks, listEmployees, listEvents } from '../api';
import type { Task, Employee, Event } from '../types';
import { eventIcon, activityVerb, activityTimeAgo as timeAgo } from '../activity';

const STATUS_STYLES: Record<Task['status'], string> = {
  scheduled:    'bg-amber-900/40 text-amber-300 border-amber-700/40',
  todo:         'bg-zinc-800 text-zinc-400 border-zinc-600',
  ready:        'bg-cyan-900/40 text-cyan-300 border-cyan-700/40',
  in_progress:  'bg-blue-900/40 text-blue-300 border-blue-700/40',
  needs_review: 'bg-violet-900/40 text-violet-300 border-violet-700/40',
  done:         'bg-emerald-900/40 text-emerald-300 border-emerald-700/40',
  blocked:      'bg-red-900/40 text-red-300 border-red-700/40',
};

export default function DashboardPage({ onOpenTask }: {
  onOpenTask: (taskId: string) => void;
}) {
  const [tasks, setTasks] = useState<Task[]>([]);
  const [employees, setEmployees] = useState<Employee[]>([]);
  const [events, setEvents] = useState<Event[]>([]);

  const refresh = useCallback(() => {
    listTasks().then(setTasks).catch(() => {});
    listEvents({ limit: 12 }).then(setEvents).catch(() => {});
  }, []);

  useEffect(() => {
    listEmployees().then(setEmployees).catch(() => {});
    refresh();
    const id = setInterval(refresh, 15000);
    return () => clearInterval(id);
  }, [refresh]);

  const empName = useMemo(() => {
    const m = new Map<string, string>();
    for (const e of employees) m.set(e.id, e.name);
    return m;
  }, [employees]);

  const recentTasks = useMemo(
    () => [...tasks]
      .sort((a, b) => new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime())
      .slice(0, 10),
    [tasks],
  );

  return (
    <div className="h-full flex flex-col p-6">
      {/* Header */}
      <div className="flex items-center gap-2.5 mb-6 shrink-0">
        <LayoutDashboard size={20} className="text-cyan-400" />
        <div>
          <h1 className="text-xl font-bold text-white">Dashboard</h1>
          <p className="text-xs text-zinc-500 mt-0.5">Recent tasks and team activity</p>
        </div>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-5 flex-1 min-h-0">
        {/* Recent Tasks */}
        <section className="flex flex-col rounded-xl border border-zinc-800/60 overflow-hidden" style={{ background: '#0a0a0d' }}>
          <div className="flex items-center gap-2 px-4 py-3 border-b border-zinc-800/60 shrink-0">
            <Kanban size={15} className="text-cyan-400" />
            <h2 className="text-sm font-semibold text-zinc-200">Recent Tasks</h2>
            <span className="text-[10px] text-zinc-600 ml-auto">task lifecycle</span>
          </div>
          <div className="flex-1 overflow-y-auto p-2">
            {recentTasks.length === 0 ? (
              <p className="text-xs text-zinc-600 px-2 py-4">No tasks yet.</p>
            ) : (
              <div className="flex flex-col gap-1">
                {recentTasks.map(t => (
                  <button
                    key={t.id}
                    onClick={() => onOpenTask(t.id)}
                    className="group w-full text-left px-3 py-2.5 rounded-lg border border-transparent hover:border-zinc-700/60 hover:bg-zinc-900/40 transition-all cursor-pointer"
                  >
                    <div className="flex items-center gap-2">
                      <p className="text-sm text-zinc-200 truncate flex-1">{t.title}</p>
                      <span className={`shrink-0 text-[9px] px-1.5 py-0.5 rounded border ${STATUS_STYLES[t.status]}`}>
                        {t.status.replace('_', ' ')}
                      </span>
                      <ArrowRight size={13} className="shrink-0 text-zinc-700 group-hover:text-cyan-400 transition-colors" />
                    </div>
                    <div className="flex items-center gap-2 mt-1">
                      {t.assignee && <span className="text-[10px] text-zinc-500">{t.assignee.name}</span>}
                      {t.project_name && <span className="text-[10px] text-indigo-300/70">· {t.project_name}</span>}
                      <span className="text-[10px] text-zinc-600 ml-auto">{timeAgo(t.updated_at)}</span>
                    </div>
                  </button>
                ))}
              </div>
            )}
          </div>
        </section>

        {/* Recent Activity */}
        <section className="flex flex-col rounded-xl border border-zinc-800/60 overflow-hidden" style={{ background: '#0a0a0d' }}>
          <div className="flex items-center gap-2 px-4 py-3 border-b border-zinc-800/60 shrink-0">
            <Activity size={15} className="text-cyan-400" />
            <h2 className="text-sm font-semibold text-zinc-200">Recent Activity</h2>
            <span className="text-[10px] text-zinc-600 ml-auto">who did what</span>
          </div>
          <div className="flex-1 overflow-y-auto p-2">
            {events.length === 0 ? (
              <p className="text-xs text-zinc-600 px-2 py-4">No activity yet.</p>
            ) : (
              <div className="flex flex-col gap-1">
                {events.map(ev => {
                  const { verb, detail } = activityVerb(ev);
                  const actor = ev.actor_id ? (empName.get(ev.actor_id) ?? 'Unknown') : null;
                  const clickable = !!ev.task_id;
                  return (
                    <button
                      key={ev.id}
                      onClick={() => ev.task_id && onOpenTask(ev.task_id)}
                      disabled={!clickable}
                      className={`group w-full text-left px-3 py-2.5 rounded-lg border border-transparent transition-all flex items-start gap-2.5 ${
                        clickable ? 'hover:border-zinc-700/60 hover:bg-zinc-900/40 cursor-pointer' : 'cursor-default'
                      }`}
                    >
                      <div className="shrink-0 mt-0.5 w-6 h-6 rounded-full bg-zinc-800/60 flex items-center justify-center text-zinc-400">
                        {eventIcon(ev.event_type)}
                      </div>
                      <div className="min-w-0 flex-1">
                        <p className="text-xs text-zinc-300 leading-snug">
                          {actor ? (
                            <span className="font-medium text-zinc-100">{actor}</span>
                          ) : (
                            <span className="font-medium text-zinc-500">System</span>
                          )}{' '}
                          {verb}
                        </p>
                        {detail && <p className="text-[10px] text-zinc-500 truncate mt-0.5">{detail}</p>}
                      </div>
                      <div className="shrink-0 flex items-center gap-1.5">
                        <span className="text-[10px] text-zinc-600">{timeAgo(ev.timestamp)}</span>
                        {clickable && <ArrowRight size={13} className="text-zinc-700 group-hover:text-cyan-400 transition-colors" />}
                      </div>
                    </button>
                  );
                })}
              </div>
            )}
          </div>
        </section>
      </div>
    </div>
  );
}
