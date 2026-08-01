import { useState, useEffect, useCallback } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Plus, X } from 'lucide-react';
import { listTasks, listEmployees, listProjects } from '../api';
import type { Task, SearchResult } from '../types';
import SearchSelect from '../components/SearchSelect';
import RefreshButton from '../components/RefreshButton';
import { useLiveRefresh } from '../hooks/useLiveRefresh';
import { STATUS_COLUMNS, PRIORITY_ORDER } from './tasks/taskMeta';
import { TaskCard } from './tasks/TaskCard';
import { CreateTaskModal } from './tasks/CreateTaskModal';
import { TaskDetailModal } from './tasks/TaskDetailModal';

export default function TasksPage({ openTask, projectFilter }: { openTask?: { id: string; seq: number }; projectFilter?: { id: string; seq: number } }) {
  const queryClient = useQueryClient();
  const { data: tasks = [], isFetching: loading } = useQuery({ queryKey: ['tasks'], queryFn: () => listTasks() });
  const { data: employees = [] } = useQuery({ queryKey: ['employees'], queryFn: listEmployees });
  const { data: projects = [] } = useQuery({ queryKey: ['projects'], queryFn: () => listProjects() });
  const [showCreate, setShowCreate] = useState(false);
  const [selectedTaskId, setSelectedTaskId] = useState<string | null>(null);

  const [selProjects, setSelProjects] = useState<SearchResult[]>([]);
  const [selEmployees, setSelEmployees] = useState<SearchResult[]>([]);

  const refresh = useCallback(() => {
    queryClient.invalidateQueries({ queryKey: ['tasks'] });
  }, [queryClient]);

  // Live-refresh on backend events so tasks created elsewhere (chat delegation,
  // autonomous runner) appear immediately; falls back to polling when the
  // WebSocket is down. The Refresh button covers the on-demand case.
  useLiveRefresh(refresh);

  // Open a task's detail when navigated in from the Dashboard. The seq nonce
  // lets the same task be re-opened on a repeat navigation.
  useEffect(() => {
    if (openTask) setSelectedTaskId(openTask.id);
  }, [openTask]);

  // Pre-select the project from a deep link (e.g. /tasks?project_id=X) once
  // projects load, so navigating in from a project lands pre-filtered.
  useEffect(() => {
    const pid = new URLSearchParams(window.location.search).get('project_id');
    if (!pid || projects.length === 0) return;
    const proj = projects.find(p => p.id === pid);
    if (proj) setSelProjects([{ id: proj.id, label: proj.name }]);
  }, [projects]);

  // Pre-select the project when navigated in from a project-scoped chat strip.
  // Keyed on the seq nonce so a repeat click re-applies the filter.
  useEffect(() => {
    if (!projectFilter || projects.length === 0) return;
    const proj = projects.find(p => p.id === projectFilter.id);
    if (proj) setSelProjects([{ id: proj.id, label: proj.name }]);
  }, [projectFilter, projects]);

  const projIds = new Set(selProjects.map(p => p.id));
  const empIds = new Set(selEmployees.map(e => e.id));
  const visibleTasks = tasks.filter(t =>
    (projIds.size === 0 || (t.project_id != null && projIds.has(t.project_id))) &&
    (empIds.size === 0 || (t.assignee != null && empIds.has(t.assignee.id)))
  );

  const grouped: Record<Task['status'], Task[]> = {
    scheduled: [], todo: [], ready: [], in_progress: [], needs_review: [], done: [], blocked: [],
  };
  for (const t of visibleTasks) {
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
          <p className="text-xs text-zinc-500 mt-0.5">{visibleTasks.length} of {tasks.length}</p>
        </div>
        <div className="flex items-center gap-2">
          <RefreshButton onClick={refresh} loading={loading} />
          <button
            onClick={() => setShowCreate(true)}
            className="flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium text-white cursor-pointer transition-all hover:opacity-90"
            style={{ background: 'linear-gradient(135deg, #0e7490, #164e63)' }}
          >
            <Plus size={16} /> Create Task
          </button>
        </div>
      </div>

      {/* Filters */}
      <div className="flex flex-wrap items-center gap-2 mb-4 shrink-0">
        <span className="text-xs text-zinc-500">Filter:</span>
        <SearchSelect type="projects" placeholder="Project" selected={selProjects} onChange={setSelProjects} />
        <SearchSelect type="employees" placeholder="Assignee" selected={selEmployees} onChange={setSelEmployees} />
        {(selProjects.length > 0 || selEmployees.length > 0) && (
          <button
            onClick={() => { setSelProjects([]); setSelEmployees([]); }}
            className="flex items-center gap-1 text-xs text-zinc-500 hover:text-zinc-300 cursor-pointer"
          >
            <X size={14} /> Clear
          </button>
        )}
      </div>

      {/* Board */}
      <div className="flex-1 overflow-x-auto min-h-0">
        <div className="flex gap-4 h-full min-w-max">
          {STATUS_COLUMNS.map(col => {
            const colTasks = grouped[col.key];
            if ((col.key === 'blocked' || col.key === 'scheduled') && colTasks.length === 0) return null;
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
          projects={projects}
          defaultProjectId={selProjects.length === 1 ? selProjects[0].id : ''}
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
