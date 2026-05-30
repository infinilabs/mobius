import { useState, useEffect, useCallback } from 'react';
import { Target, Plus, ChevronRight, Trash2, Check, X } from 'lucide-react';
import { listGoals, createGoal, updateGoal, deleteGoal } from '../api';
import type { Goal } from '../types';

function StatusBadge({ status }: { status: string }) {
  const colors: Record<string, string> = {
    active: 'bg-cyan-600/20 text-cyan-300 border-cyan-600/40',
    achieved: 'bg-emerald-600/20 text-emerald-300 border-emerald-600/40',
    abandoned: 'bg-zinc-600/20 text-zinc-400 border-zinc-600/40',
  };
  return (
    <span className={`px-2 py-0.5 rounded-full text-[10px] font-medium border ${colors[status] || colors.active}`}>
      {status}
    </span>
  );
}

function GoalNode({ goal, goals, depth, onRefresh, onError, ancestors }: {
  goal: Goal;
  goals: Goal[];
  depth: number;
  onRefresh: () => void;
  onError: (msg: string) => void;
  ancestors: Set<string>;
}) {
  const [expanded, setExpanded] = useState(true);
  // Cycle guard: cyclic goal data (A→B→A) would otherwise recurse until the
  // stack overflows. Stop descending if this goal is already an ancestor.
  if (ancestors.has(goal.id)) return null;
  const childAncestors = new Set(ancestors).add(goal.id);
  const children = goals.filter(g => g.parent_id === goal.id);

  const handleDelete = async () => {
    if (!confirm(`Delete goal "${goal.title}"?`)) return;
    try {
      await deleteGoal(goal.id);
      onRefresh();
    } catch (e) {
      onError(e instanceof Error ? e.message : 'Failed to delete goal');
    }
  };

  const handleStatusChange = async (status: string) => {
    try {
      await updateGoal(goal.id, { status });
      onRefresh();
    } catch (e) {
      onError(e instanceof Error ? e.message : 'Failed to update goal');
    }
  };

  return (
    <div style={{ marginLeft: depth * 24 }}>
      <div className="flex items-center gap-2 py-2 px-3 rounded-lg hover:bg-zinc-800/50 group">
        {children.length > 0 ? (
          <button onClick={() => setExpanded(!expanded)} className="text-zinc-500 hover:text-zinc-300 cursor-pointer">
            <ChevronRight size={14} className={`transition-transform ${expanded ? 'rotate-90' : ''}`} />
          </button>
        ) : (
          <span className="w-[14px]" />
        )}
        <Target size={14} className="text-cyan-400 shrink-0" />
        <span className="text-sm text-zinc-200 flex-1">{goal.title}</span>
        <StatusBadge status={goal.status} />
        <div className="hidden group-hover:flex items-center gap-1">
          {goal.status === 'active' && (
            <button onClick={() => handleStatusChange('achieved')} className="p-1 rounded hover:bg-emerald-600/20 text-zinc-500 hover:text-emerald-400 cursor-pointer" title="Mark achieved">
              <Check size={12} />
            </button>
          )}
          {goal.status === 'active' && (
            <button onClick={() => handleStatusChange('abandoned')} className="p-1 rounded hover:bg-zinc-600/20 text-zinc-500 hover:text-zinc-400 cursor-pointer" title="Abandon">
              <X size={12} />
            </button>
          )}
          <button onClick={handleDelete} className="p-1 rounded hover:bg-red-600/20 text-zinc-500 hover:text-red-400 cursor-pointer" title="Delete">
            <Trash2 size={12} />
          </button>
        </div>
      </div>
      {goal.description && (
        <p className="text-xs text-zinc-500 ml-[38px] mb-1 max-w-lg">{goal.description}</p>
      )}
      {expanded && children.map(child => (
        <GoalNode key={child.id} goal={child} goals={goals} depth={depth + 1} onRefresh={onRefresh} onError={onError} ancestors={childAncestors} />
      ))}
    </div>
  );
}

export default function GoalsPage() {
  const [goals, setGoals] = useState<Goal[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [showCreate, setShowCreate] = useState(false);
  const [newTitle, setNewTitle] = useState('');
  const [newDesc, setNewDesc] = useState('');
  const [newParentID, setNewParentID] = useState('');

  const loadGoals = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      setGoals(await listGoals());
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load goals');
    }
    setLoading(false);
  }, []);

  useEffect(() => { loadGoals(); }, [loadGoals]);

  const handleCreate = async () => {
    if (!newTitle.trim()) return;
    try {
      await createGoal({
        title: newTitle.trim(),
        description: newDesc.trim(),
        parent_id: newParentID || undefined,
      });
      setNewTitle('');
      setNewDesc('');
      setNewParentID('');
      setShowCreate(false);
      loadGoals();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to create goal');
    }
  };

  const rootGoals = goals.filter(g => !g.parent_id);

  return (
    <div className="p-6 space-y-6 max-w-4xl mx-auto">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold text-zinc-100">Goals</h1>
        <button onClick={() => setShowCreate(!showCreate)}
          className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-cyan-600/20 text-cyan-300 border border-cyan-600/40 text-xs font-medium hover:bg-cyan-600/30 cursor-pointer">
          <Plus size={14} />
          New Goal
        </button>
      </div>

      {showCreate && (
        <div className="bg-zinc-800/50 border border-zinc-700/50 rounded-xl p-4 space-y-3">
          <input value={newTitle} onChange={e => setNewTitle(e.target.value)} placeholder="Goal title"
            className="w-full bg-zinc-900 border border-zinc-700 rounded-lg px-3 py-2 text-sm text-zinc-200 outline-none focus:border-cyan-600" />
          <textarea value={newDesc} onChange={e => setNewDesc(e.target.value)} placeholder="Description (optional)"
            className="w-full bg-zinc-900 border border-zinc-700 rounded-lg px-3 py-2 text-sm text-zinc-200 outline-none focus:border-cyan-600 resize-none h-20" />
          <div className="flex items-center gap-2">
            <select value={newParentID} onChange={e => setNewParentID(e.target.value)}
              className="bg-zinc-900 border border-zinc-700 rounded-lg px-3 py-2 text-sm text-zinc-200 outline-none flex-1">
              <option value="">No parent (top-level goal)</option>
              {goals.filter(g => g.status === 'active').map(g => (
                <option key={g.id} value={g.id}>{g.title}</option>
              ))}
            </select>
            <button onClick={handleCreate}
              className="px-4 py-2 rounded-lg bg-cyan-600 text-white text-sm font-medium hover:bg-cyan-500 cursor-pointer">
              Create
            </button>
            <button onClick={() => setShowCreate(false)}
              className="px-4 py-2 rounded-lg bg-zinc-700 text-zinc-300 text-sm hover:bg-zinc-600 cursor-pointer">
              Cancel
            </button>
          </div>
        </div>
      )}

      {loading && <p className="text-xs text-zinc-500">Loading...</p>}

      {error && (
        <p className="text-sm text-red-400 bg-red-600/10 border border-red-600/30 rounded-lg px-3 py-2">
          {error}
        </p>
      )}

      <div className="bg-zinc-800/50 border border-zinc-700/50 rounded-xl p-4">
        {rootGoals.length === 0 && !loading && !error && (
          <p className="text-sm text-zinc-500 text-center py-8">No goals yet. Create one to align your agents' work.</p>
        )}
        {rootGoals.map(goal => (
          <GoalNode key={goal.id} goal={goal} goals={goals} depth={0} onRefresh={loadGoals} onError={setError} ancestors={new Set()} />
        ))}
      </div>
    </div>
  );
}
