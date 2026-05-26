import { useState, useEffect, useCallback } from 'react';
import {
  Users, Plus, Trash2, X, Pencil, ChevronDown, ChevronRight,
  Brain, Wrench, UserCog, Tag, Filter, RotateCcw, Database, Search,
  ChevronUp,
} from 'lucide-react';
import { listEmployees, createEmployee, updateEmployee, deleteEmployee, setEmployeeManager, listModels, listEmployeeSkills, resetEmployeeSkills, listEmployeeMemories, addEmployeeMemory, deleteEmployeeMemory } from '../api';
import type { Employee, EmployeeModel, EmployeeMemory, VertexModel, Skill } from '../types';

const ROLE_COLORS: Record<string, string> = {
  CEO: '#38bdf8', PM: '#c084fc', Engineer: '#4ade80',
  QA: '#fbbf24', Designer: '#fb7185', Custom: '#a1a1aa',
};

interface TreeNode {
  employee: Employee;
  children: TreeNode[];
}

function buildTree(employees: Employee[]): TreeNode[] {
  const map = new Map<string, TreeNode>();
  for (const emp of employees) {
    map.set(emp.id, { employee: emp, children: [] });
  }
  const roots: TreeNode[] = [];
  for (const emp of employees) {
    const node = map.get(emp.id)!;
    if (emp.manager_id && map.has(emp.manager_id)) {
      map.get(emp.manager_id)!.children.push(node);
    } else {
      roots.push(node);
    }
  }
  return roots;
}

export default function HRPage() {
  const [employees, setEmployees] = useState<Employee[]>([]);
  const [loading, setLoading] = useState(true);
  const [selected, setSelected] = useState<Employee | null>(null);
  const [showCreate, setShowCreate] = useState(false);
  const [editTarget, setEditTarget] = useState<Employee | null>(null);
  const [models, setModels] = useState<VertexModel[]>([]);
  const [filterTag, setFilterTag] = useState<string>('');
  const [expandedNodes, setExpandedNodes] = useState<Set<string>>(new Set());
  const toggleNode = useCallback((id: string) => setExpandedNodes(prev => {
    const next = new Set(prev);
    if (next.has(id)) { next.delete(id); } else { next.add(id); }
    return next;
  }), []);

  const refresh = useCallback(() => {
    setLoading(true);
    listEmployees()
      .then(setEmployees)
      .catch(() => setEmployees([]))
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => { refresh(); }, [refresh]);
  useEffect(() => { listModels().then(setModels).catch(() => {}); }, []);

  const handleDelete = async (id: string) => {
    await deleteEmployee(id);
    if (selected?.id === id) setSelected(null);
    refresh();
  };

  const handleCreated = () => {
    setShowCreate(false);
    refresh();
  };

  const handleUpdated = () => {
    setEditTarget(null);
    refresh();
  };

  const handleReassign = async (empId: string, managerId: string) => {
    await setEmployeeManager(empId, managerId);
    refresh();
  };

  const tree = buildTree(employees);

  const allTags = Array.from(new Set(employees.flatMap(e => e.tags))).sort();
  const highlightIds = new Set(
    filterTag ? employees.filter(e => e.tags.includes(filterTag)).map(e => e.id) : []
  );
  const forceExpandedIds = new Set<string>();
  if (filterTag) {
    for (const emp of employees) {
      if (emp.tags.includes(filterTag) && emp.manager_id) {
        forceExpandedIds.add(emp.manager_id);
      }
    }
  }

  return (
    <div className="p-8 max-w-[1400px] mx-auto">
      <style>{`
        @keyframes breathe {
          0%, 100% { box-shadow: 0 0 8px 2px rgba(56, 189, 248, 0.3), 0 0 20px 4px rgba(56, 189, 248, 0.15); }
          50% { box-shadow: 0 0 16px 6px rgba(56, 189, 248, 0.6), 0 0 40px 12px rgba(56, 189, 248, 0.25); }
        }
        .glow-breathe {
          animation: breathe 2s ease-in-out infinite;
          border-color: rgba(56, 189, 248, 0.5) !important;
        }
      `}</style>

      <header className="flex items-center justify-between mb-6">
        <div>
          <div className="flex items-center gap-2.5 mb-1">
            <Users size={20} className="text-cyan-400" />
            <h2 className="text-2xl font-bold tracking-tight text-white">HR Management</h2>
          </div>
          <p className="text-xs text-zinc-600 mt-1">Manage AI agents, roles, and reporting structure</p>
        </div>
        <div className="flex items-center gap-3">
          {allTags.length > 0 && (
            <div className="flex items-center gap-1.5">
              <Filter size={13} className="text-zinc-600" />
              <select
                value={filterTag}
                onChange={e => setFilterTag(e.target.value)}
                className="text-xs text-zinc-300 rounded-lg px-3 py-2 outline-none border border-zinc-800/50 focus:border-cyan-500/30 cursor-pointer"
                style={{ background: '#111114' }}
              >
                <option value="">All employees</option>
                {allTags.map(tag => (
                  <option key={tag} value={tag}>{tag}</option>
                ))}
              </select>
            </div>
          )}
          <button
            onClick={() => setShowCreate(true)}
            className="flex items-center gap-2 px-4 py-2 rounded-lg text-xs font-medium text-white transition-colors cursor-pointer"
            style={{ background: 'linear-gradient(135deg, #0e7490, #164e63)', border: '1px solid #0e749050' }}
          >
            <Plus size={14} /> New Employee
          </button>
        </div>
      </header>

      {showCreate && (
        <EmployeeModal
          employees={employees}
          models={models}
          onClose={() => setShowCreate(false)}
          onSaved={handleCreated}
        />
      )}

      {editTarget && (
        <EmployeeModal
          employee={editTarget}
          employees={employees}
          models={models}
          onClose={() => setEditTarget(null)}
          onSaved={handleUpdated}
        />
      )}

      {loading ? (
        <div className="flex items-center justify-center" style={{ minHeight: 400 }}>
          <div className="h-8 w-8 rounded-full border-2 border-transparent animate-spin" style={{ borderTopColor: '#38bdf8' }} />
        </div>
      ) : employees.length === 0 ? (
        <div className="rounded-xl border border-zinc-800/40 flex flex-col items-center justify-center" style={{ background: '#111114', minHeight: 400 }}>
          <Users size={40} className="text-zinc-800 mb-4" />
          <p className="text-sm font-semibold text-zinc-300 mb-2">No employees yet</p>
          <p className="text-xs text-zinc-600">Add your first AI agent to get started.</p>
        </div>
      ) : (
        <div className="flex gap-6">
          {/* Org Chart */}
          <div className="flex-1 min-w-0">
            <div className="rounded-xl border border-zinc-800/40 p-6 overflow-x-auto" style={{ background: '#111114' }}>
              <p className="text-[10px] font-semibold text-zinc-600 uppercase tracking-wider mb-6">Organization Chart</p>
              <div className="flex justify-center">
                <div className="inline-flex flex-col items-center">
                  {tree.map(root => (
                    <OrgTreeNode
                      key={root.employee.id}
                      node={root}
                      depth={0}
                      selected={selected}
                      onSelect={setSelected}
                      highlightIds={highlightIds}
                      expandedNodes={expandedNodes}
                      forceExpandedIds={forceExpandedIds}
                      toggleNode={toggleNode}
                    />
                  ))}
                </div>
              </div>
            </div>
          </div>

          {/* Detail Panel */}
          {selected && (
            <DetailPanel
              employee={selected}
              employees={employees}
              onEdit={() => setEditTarget(selected)}
              onDelete={() => handleDelete(selected.id)}
              onReassign={handleReassign}
              onClose={() => setSelected(null)}
            />
          )}
        </div>
      )}
    </div>
  );
}

function OrgNodeCard({ emp, isSelected, isHighlighted, onClick }: {
  emp: Employee; isSelected: boolean; isHighlighted: boolean; onClick: () => void;
}) {
  const color = ROLE_COLORS[emp.role] || ROLE_COLORS.Custom;
  return (
    <button
      onClick={onClick}
      className={`flex items-center gap-2.5 px-4 py-3 rounded-xl border cursor-pointer transition-all ${
        isHighlighted ? 'glow-breathe' :
        isSelected ? 'border-cyan-500/40 shadow-lg' : 'border-zinc-800/40 hover:border-zinc-700/60'
      }`}
      style={{ background: isSelected ? '#0e749015' : '#0a0a0d', minWidth: 160 }}
    >
      <div
        className="w-9 h-9 rounded-full flex items-center justify-center text-sm font-bold shrink-0"
        style={{ background: `${color}20`, color, border: `2px solid ${color}40` }}
      >
        {emp.name[0]}
      </div>
      <div className="text-left min-w-0">
        <p className="text-xs font-semibold text-zinc-200 truncate">{emp.name}</p>
        <p className="text-[10px] text-zinc-500 truncate">{emp.title}</p>
      </div>
    </button>
  );
}

function OrgTreeNode({ node, depth, selected, onSelect, highlightIds, expandedNodes, forceExpandedIds, toggleNode }: {
  node: TreeNode;
  depth: number;
  selected: Employee | null;
  onSelect: (e: Employee) => void;
  highlightIds: Set<string>;
  expandedNodes: Set<string>;
  forceExpandedIds: Set<string>;
  toggleNode: (id: string) => void;
}) {
  const emp = node.employee;
  const isSelected = selected?.id === emp.id;
  const isHighlighted = highlightIds.has(emp.id);
  const hasChildren = node.children.length > 0;
  const collapsible = depth >= 1 && hasChildren;
  const isExpanded = collapsible && (expandedNodes.has(emp.id) || forceExpandedIds.has(emp.id));

  return (
    <div className="flex flex-col items-center">
      <OrgNodeCard emp={emp} isSelected={isSelected} isHighlighted={isHighlighted} onClick={() => onSelect(emp)} />

      {/* Collapsible toggle for layer-2 nodes */}
      {collapsible && (
        <div className="flex flex-col items-center">
          <div className="w-px h-4" style={{ background: '#3f3f46' }} />
          <button
            onClick={() => toggleNode(emp.id)}
            className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg border border-zinc-800/40 hover:border-zinc-600/60 cursor-pointer transition-all text-[10px] font-medium"
            style={{ background: '#0a0a0d' }}
          >
            {isExpanded
              ? <><ChevronUp size={10} className="text-cyan-400" /><span className="text-zinc-400">{node.children.length} reports</span></>
              : <><ChevronRight size={10} className="text-zinc-500" /><span className="text-zinc-500">{node.children.length} reports</span></>
            }
          </button>

          {isExpanded && (
            <>
              <div className="w-px h-4" style={{ background: '#3f3f46' }} />
              <div
                className="grid gap-2 p-3 rounded-xl border border-zinc-800/30"
                style={{
                  background: '#09090b',
                  gridTemplateColumns: 'repeat(auto-fill, minmax(170px, 1fr))',
                  maxWidth: 800,
                  width: '100%',
                }}
              >
                {node.children.map(child => {
                  const c = child.employee;
                  const cColor = ROLE_COLORS[c.role] || ROLE_COLORS.Custom;
                  const cSelected = selected?.id === c.id;
                  const cHighlighted = highlightIds.has(c.id);
                  return (
                    <button
                      key={c.id}
                      onClick={() => onSelect(c)}
                      className={`flex items-center gap-2 px-3 py-2.5 rounded-lg border cursor-pointer transition-all text-left ${
                        cHighlighted ? 'glow-breathe' :
                        cSelected ? 'border-cyan-500/40' : 'border-zinc-800/30 hover:border-zinc-700/50'
                      }`}
                      style={{ background: cSelected ? '#0e749015' : '#111114' }}
                    >
                      <div
                        className="w-7 h-7 rounded-full flex items-center justify-center text-[10px] font-bold shrink-0"
                        style={{ background: `${cColor}20`, color: cColor, border: `1.5px solid ${cColor}40` }}
                      >
                        {c.name[0]}
                      </div>
                      <div className="min-w-0">
                        <p className="text-[11px] font-medium text-zinc-300 truncate">{c.name}</p>
                        <p className="text-[9px] text-zinc-600 truncate">{c.title}</p>
                      </div>
                    </button>
                  );
                })}
              </div>
            </>
          )}
        </div>
      )}

      {/* Normal tree rendering for non-collapsible children (depth 0 only) */}
      {hasChildren && !collapsible && (
        <div className="flex flex-col items-center">
          <div className="w-px h-6" style={{ background: '#3f3f46' }} />
          {node.children.length === 1 ? (
            <OrgTreeNode node={node.children[0]} depth={depth + 1} selected={selected} onSelect={onSelect}
              highlightIds={highlightIds} expandedNodes={expandedNodes} forceExpandedIds={forceExpandedIds} toggleNode={toggleNode} />
          ) : (
            <div className="flex items-start">
              {node.children.map((child, i) => (
                <div key={child.employee.id} className="flex flex-col items-center" style={{ margin: '0 12px' }}>
                  <div className="flex items-start w-full">
                    <div className={`h-px flex-1 ${i === 0 ? 'bg-transparent' : ''}`} style={i > 0 ? { background: '#3f3f46' } : undefined} />
                    <div className="w-px h-6" style={{ background: '#3f3f46' }} />
                    <div className={`h-px flex-1 ${i === node.children.length - 1 ? 'bg-transparent' : ''}`} style={i < node.children.length - 1 ? { background: '#3f3f46' } : undefined} />
                  </div>
                  <OrgTreeNode node={child} depth={depth + 1} selected={selected} onSelect={onSelect}
                    highlightIds={highlightIds} expandedNodes={expandedNodes} forceExpandedIds={forceExpandedIds} toggleNode={toggleNode} />
                </div>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
}

function DetailPanel({ employee, employees, onEdit, onDelete, onReassign, onClose }: {
  employee: Employee;
  employees: Employee[];
  onEdit: () => void;
  onDelete: () => void;
  onReassign: (empId: string, managerId: string) => void;
  onClose: () => void;
}) {
  const color = ROLE_COLORS[employee.role] || ROLE_COLORS.Custom;
  const [showReassign, setShowReassign] = useState(false);
  const [assignedSkills, setAssignedSkills] = useState<Skill[]>([]);
  const [memories, setMemories] = useState<EmployeeMemory[]>([]);
  const [memorySearch, setMemorySearch] = useState('');
  const [newMemoryText, setNewMemoryText] = useState('');

  useEffect(() => {
    listEmployeeSkills(employee.id).then(setAssignedSkills).catch(() => setAssignedSkills([]));
    listEmployeeMemories(employee.id).then(setMemories).catch(() => setMemories([]));
  }, [employee.id]);

  const searchMemories = (q: string) => {
    setMemorySearch(q);
    listEmployeeMemories(employee.id, q || undefined).then(setMemories).catch(() => setMemories([]));
  };

  const handleAddMemory = async () => {
    if (!newMemoryText.trim()) return;
    await addEmployeeMemory(employee.id, newMemoryText.trim());
    setNewMemoryText('');
    listEmployeeMemories(employee.id, memorySearch || undefined).then(setMemories).catch(() => setMemories([]));
  };

  const handleDeleteMemory = async (memoryId: string) => {
    await deleteEmployeeMemory(employee.id, memoryId);
    setMemories(prev => prev.filter(m => m.id !== memoryId));
  };

  return (
    <div className="w-[340px] shrink-0 rounded-xl border border-zinc-800/40 overflow-hidden" style={{ background: '#111114' }}>
      {/* Header */}
      <div className="px-5 py-4 border-b border-zinc-800/40">
        <div className="flex items-start justify-between">
          <div className="flex items-center gap-3">
            <div className="w-11 h-11 rounded-full flex items-center justify-center text-lg font-bold"
              style={{ background: `${color}20`, color, border: `2px solid ${color}40` }}>
              {employee.name[0]}
            </div>
            <div>
              <p className="text-sm font-semibold text-white">{employee.name}</p>
              <p className="text-[11px] text-zinc-500">{employee.title}</p>
            </div>
          </div>
          <button onClick={onClose} className="p-1 text-zinc-600 hover:text-zinc-300 cursor-pointer transition-colors">
            <X size={14} />
          </button>
        </div>
        <div className="flex items-center gap-2 mt-3">
          <span className="px-2 py-0.5 rounded text-[10px] font-bold uppercase" style={{ color, background: `${color}15`, border: `1px solid ${color}30` }}>
            {employee.role}
          </span>
        </div>
      </div>

      {/* Body */}
      <div className="px-5 py-4 space-y-4 max-h-[500px] overflow-y-auto">
        {employee.backstory && (
          <div>
            <p className="text-[10px] font-semibold text-zinc-600 uppercase tracking-wider mb-1.5">Backstory</p>
            <p className="text-xs text-zinc-400 leading-relaxed">{employee.backstory}</p>
          </div>
        )}

        {employee.models.length > 0 && (
          <div>
            <div className="flex items-center gap-1.5 mb-1.5">
              <Brain size={11} className="text-zinc-600" />
              <p className="text-[10px] font-semibold text-zinc-600 uppercase tracking-wider">Models</p>
            </div>
            <div className="space-y-1">
              {employee.models.map(m => (
                <div key={m.purpose} className="flex items-center justify-between px-2.5 py-1.5 rounded-lg border border-zinc-800/30" style={{ background: '#09090b' }}>
                  <span className="text-[10px] text-zinc-500 uppercase">{m.purpose.replace('_', ' ')}</span>
                  <span className="text-[10px] text-zinc-300 font-mono">{m.model_id}</span>
                </div>
              ))}
            </div>
          </div>
        )}

        <div>
          <div className="flex items-center justify-between mb-1.5">
            <div className="flex items-center gap-1.5">
              <Wrench size={11} className="text-zinc-600" />
              <p className="text-[10px] font-semibold text-zinc-600 uppercase tracking-wider">Assigned Skills ({assignedSkills.length})</p>
            </div>
            {employee.tags.includes('founder') && (
              <button
                onClick={async () => {
                  await resetEmployeeSkills(employee.id);
                  listEmployeeSkills(employee.id).then(setAssignedSkills).catch(() => setAssignedSkills([]));
                }}
                className="flex items-center gap-1 px-1.5 py-0.5 text-[9px] text-zinc-500 hover:text-cyan-400 border border-zinc-800/30 hover:border-cyan-500/30 rounded transition-colors cursor-pointer"
                title="Reset to default skills"
              >
                <RotateCcw size={9} /> Reset
              </button>
            )}
          </div>
          {assignedSkills.length > 0 ? (
            <div className="space-y-1">
              {assignedSkills.map(s => (
                <div key={s.id} className="px-2.5 py-1.5 rounded-lg border border-zinc-800/30" style={{ background: '#09090b' }}>
                  <div className="flex items-center gap-1.5">
                    <p className="text-[10px] font-medium text-zinc-300">{s.name}</p>
                    <span className="px-1 py-0.5 text-[8px] rounded bg-zinc-800 text-zinc-500 border border-zinc-700">{s.category}</span>
                  </div>
                  {s.description && <p className="text-[9px] text-zinc-600 mt-0.5 truncate">{s.description}</p>}
                </div>
              ))}
            </div>
          ) : (
            <p className="text-[10px] text-zinc-600">No skills assigned</p>
          )}
          <p className="text-[9px] text-zinc-700 mt-2">Manage assignments in Skills page</p>
        </div>

        {/* Memory */}
        <div>
          <div className="flex items-center justify-between mb-1.5">
            <div className="flex items-center gap-1.5">
              <Database size={11} className="text-zinc-600" />
              <p className="text-[10px] font-semibold text-zinc-600 uppercase tracking-wider">Memory ({memories.length})</p>
            </div>
          </div>

          <div className="relative mb-2">
            <Search size={10} className="absolute left-2 top-1/2 -translate-y-1/2 text-zinc-600" />
            <input
              type="text"
              value={memorySearch}
              onChange={e => searchMemories(e.target.value)}
              placeholder="Search memories..."
              className="w-full text-[10px] text-zinc-300 rounded-lg pl-6 pr-2 py-1.5 outline-none border border-zinc-800/50 focus:border-cyan-500/30"
              style={{ background: '#09090b' }}
            />
          </div>

          {memories.length > 0 ? (
            <div className="space-y-1 max-h-[150px] overflow-y-auto">
              {memories.map(m => (
                <div key={m.id} className="flex items-start justify-between gap-1.5 px-2.5 py-1.5 rounded-lg border border-zinc-800/30" style={{ background: '#09090b' }}>
                  <div className="min-w-0 flex-1">
                    <p className="text-[10px] text-zinc-300 leading-relaxed">{m.memory_text}</p>
                    <p className="text-[8px] text-zinc-700 mt-0.5 font-mono">{m.id.slice(0, 8)}</p>
                  </div>
                  <button onClick={() => handleDeleteMemory(m.id)}
                    className="shrink-0 p-0.5 text-zinc-700 hover:text-red-400 cursor-pointer transition-colors">
                    <Trash2 size={9} />
                  </button>
                </div>
              ))}
            </div>
          ) : (
            <p className="text-[10px] text-zinc-600">{memorySearch ? 'No matching memories' : 'No memories yet'}</p>
          )}

          <div className="flex gap-1.5 mt-2">
            <input
              type="text"
              value={newMemoryText}
              onChange={e => setNewMemoryText(e.target.value)}
              onKeyDown={e => e.key === 'Enter' && handleAddMemory()}
              placeholder="Add a memory..."
              className="flex-1 text-[10px] text-zinc-300 rounded-lg px-2.5 py-1.5 outline-none border border-zinc-800/50 focus:border-cyan-500/30"
              style={{ background: '#09090b' }}
            />
            <button onClick={handleAddMemory}
              className="px-2 py-1.5 text-[9px] text-cyan-400 border border-cyan-500/30 rounded-lg hover:bg-cyan-500/10 cursor-pointer transition-colors">
              Add
            </button>
          </div>
        </div>

        {employee.tags.length > 0 && (
          <div>
            <div className="flex items-center gap-1.5 mb-1.5">
              <Tag size={11} className="text-zinc-600" />
              <p className="text-[10px] font-semibold text-zinc-600 uppercase tracking-wider">Tags</p>
            </div>
            <div className="flex flex-wrap gap-1.5">
              {employee.tags.map(tag => (
                <span key={tag} className="px-2 py-0.5 rounded text-[10px] font-medium text-cyan-400"
                  style={{ background: '#0e749015', border: '1px solid #0e749030' }}>
                  {tag}
                </span>
              ))}
            </div>
          </div>
        )}

        {/* Reassign Manager */}
        <div>
          <div className="flex items-center gap-1.5 mb-1.5">
            <UserCog size={11} className="text-zinc-600" />
            <p className="text-[10px] font-semibold text-zinc-600 uppercase tracking-wider">Reports To</p>
          </div>
          {showReassign ? (
            <div className="space-y-1.5">
              <select
                defaultValue={employee.manager_id || ''}
                onChange={e => { onReassign(employee.id, e.target.value); setShowReassign(false); }}
                className="w-full text-xs text-zinc-300 rounded-lg px-2.5 py-2 outline-none border border-zinc-800/50 focus:border-cyan-500/30 cursor-pointer"
                style={{ background: '#09090b' }}
              >
                <option value="">— None (Top Level) —</option>
                {employees.filter(e => e.id !== employee.id).map(e => (
                  <option key={e.id} value={e.id}>{e.name} ({e.title})</option>
                ))}
              </select>
              <button onClick={() => setShowReassign(false)} className="text-[10px] text-zinc-600 hover:text-zinc-400 cursor-pointer transition-colors">Cancel</button>
            </div>
          ) : (
            <button onClick={() => setShowReassign(true)}
              className="text-xs text-zinc-400 hover:text-cyan-400 cursor-pointer transition-colors">
              {employee.manager_id
                ? employees.find(e => e.id === employee.manager_id)?.name || 'Unknown'
                : '— None —'}
              <span className="text-zinc-700 ml-1">(click to reassign)</span>
            </button>
          )}
        </div>
      </div>

      {/* Actions */}
      <div className="px-5 py-3 border-t border-zinc-800/40 flex items-center gap-2">
        <button onClick={onEdit}
          className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs text-zinc-400 hover:text-cyan-400 border border-zinc-800/40 hover:border-cyan-500/30 cursor-pointer transition-colors"
          style={{ background: '#09090b' }}>
          <Pencil size={11} /> Edit
        </button>
        <button onClick={onDelete}
          className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs text-zinc-400 hover:text-red-400 border border-zinc-800/40 hover:border-red-500/30 cursor-pointer transition-colors"
          style={{ background: '#09090b' }}>
          <Trash2 size={11} /> Delete
        </button>
      </div>
    </div>
  );
}

function EmployeeModal({ employee, employees, models, onClose, onSaved }: {
  employee?: Employee;
  employees: Employee[];
  models: VertexModel[];
  onClose: () => void;
  onSaved: () => void;
}) {
  const isEdit = !!employee;
  const [name, setName] = useState(employee?.name || '');
  const [title, setTitle] = useState(employee?.title || '');
  const [role, setRole] = useState(employee?.role || 'Custom');
  const [backstory, setBackstory] = useState(employee?.backstory || '');
  const [managerId, setManagerId] = useState(employee?.manager_id || '');
  const [empModels, setEmpModels] = useState<EmployeeModel[]>(employee?.models || []);
  const [assignedSkills, setAssignedSkills] = useState<Skill[]>([]);
  const [tags, setTags] = useState<string[]>(employee?.tags || []);
  const [tagInput, setTagInput] = useState('');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  const [expandedSections, setExpandedSections] = useState({ models: isEdit, skills: isEdit });

  useEffect(() => {
    if (employee) {
      listEmployeeSkills(employee.id).then(setAssignedSkills).catch(() => setAssignedSkills([]));
    }
  }, [employee]);

  const addTags = () => {
    const newTags = tagInput.split(',').map(t => t.trim().toLowerCase()).filter(t => t && !tags.includes(t));
    if (newTags.length) setTags([...tags, ...newTags]);
    setTagInput('');
  };

  const setModelForPurpose = (purpose: string, modelId: string) => {
    const next = empModels.filter(m => m.purpose !== purpose);
    if (modelId) next.push({ model_id: modelId, purpose });
    setEmpModels(next);
  };

  const handleSubmit = async () => {
    if (!name.trim()) { setError('Name is required'); return; }
    setSaving(true);
    setError('');
    try {
      const data: Partial<Employee> = {
        name: name.trim(),
        title: title.trim(),
        role,
        backstory: backstory.trim(),
        models: empModels,
        skills: [],
        tags,
        manager_id: managerId || undefined,
      };
      if (isEdit) {
        await updateEmployee(employee!.id, data);
      } else {
        await createEmployee(data);
      }
      onSaved();
    } catch {
      setError('Failed to save employee');
    } finally {
      setSaving(false);
    }
  };

  const ROLES = ['CEO', 'PM', 'Engineer', 'QA', 'Designer', 'Custom'];
  const MODEL_PURPOSES = [
    { key: 'primary_llm', label: 'Primary LLM', filterType: 'llm' },
    { key: 'image_gen', label: 'Image Generation', filterType: 'image' },
    { key: 'video_gen', label: 'Video Generation', filterType: 'video' },
    { key: 'code_gen', label: 'Code Generation', filterType: 'llm' },
    { key: 'analysis', label: 'Analysis', filterType: 'llm' },
  ];

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center" style={{ background: 'rgba(0,0,0,0.6)' }}>
      <div className="rounded-2xl border border-zinc-800/60 shadow-2xl w-full max-w-lg mx-4 max-h-[90vh] flex flex-col" style={{ background: '#0a0a0d' }}>
        <div className="flex items-center justify-between px-6 py-4 border-b border-zinc-800/40 shrink-0">
          <h3 className="text-sm font-semibold text-white">{isEdit ? 'Edit Employee' : 'New Employee'}</h3>
          <button onClick={onClose} className="text-zinc-600 hover:text-zinc-300 cursor-pointer transition-colors"><X size={16} /></button>
        </div>

        <div className="px-6 py-4 flex flex-col gap-4 overflow-y-auto flex-1">
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="text-[10px] font-semibold text-zinc-600 uppercase tracking-wider mb-1.5 block">Name</label>
              <input value={name} onChange={e => setName(e.target.value)} placeholder="e.g. Elong"
                className="w-full text-xs text-zinc-300 rounded-lg px-3 py-2.5 outline-none border border-zinc-800/50 focus:border-cyan-500/30 placeholder:text-zinc-700" style={{ background: '#111114' }} />
            </div>
            <div>
              <label className="text-[10px] font-semibold text-zinc-600 uppercase tracking-wider mb-1.5 block">Title</label>
              <input value={title} onChange={e => setTitle(e.target.value)} placeholder="e.g. Chief Executive Officer"
                className="w-full text-xs text-zinc-300 rounded-lg px-3 py-2.5 outline-none border border-zinc-800/50 focus:border-cyan-500/30 placeholder:text-zinc-700" style={{ background: '#111114' }} />
            </div>
          </div>

          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="text-[10px] font-semibold text-zinc-600 uppercase tracking-wider mb-1.5 block">Role</label>
              <select value={role} onChange={e => setRole(e.target.value)}
                className="w-full text-xs text-zinc-300 rounded-lg px-3 py-2.5 outline-none border border-zinc-800/50 focus:border-cyan-500/30 cursor-pointer" style={{ background: '#111114' }}>
                {ROLES.map(r => <option key={r} value={r}>{r}</option>)}
              </select>
            </div>
            <div>
              <label className="text-[10px] font-semibold text-zinc-600 uppercase tracking-wider mb-1.5 block">Reports To</label>
              <select value={managerId} onChange={e => setManagerId(e.target.value)}
                className="w-full text-xs text-zinc-300 rounded-lg px-3 py-2.5 outline-none border border-zinc-800/50 focus:border-cyan-500/30 cursor-pointer" style={{ background: '#111114' }}>
                <option value="">— None —</option>
                {employees.filter(e => e.id !== employee?.id).map(e => (
                  <option key={e.id} value={e.id}>{e.name} ({e.title})</option>
                ))}
              </select>
            </div>
          </div>

          <div>
            <label className="text-[10px] font-semibold text-zinc-600 uppercase tracking-wider mb-1.5 block">Backstory</label>
            <textarea value={backstory} onChange={e => setBackstory(e.target.value)}
              placeholder="Describe this agent's personality, expertise, and approach..."
              rows={3}
              className="w-full text-xs text-zinc-300 rounded-lg px-3 py-2.5 outline-none border border-zinc-800/50 focus:border-cyan-500/30 placeholder:text-zinc-700 resize-none leading-relaxed" style={{ background: '#111114' }} />
          </div>

          {/* Model Assignments */}
          <div>
            <button onClick={() => setExpandedSections(s => ({ ...s, models: !s.models }))}
              className="flex items-center gap-1.5 text-[10px] font-semibold text-zinc-600 uppercase tracking-wider cursor-pointer hover:text-zinc-400 transition-colors">
              {expandedSections.models ? <ChevronDown size={10} /> : <ChevronRight size={10} />}
              Model Assignments
            </button>
            {expandedSections.models && (
              <div className="mt-2 space-y-1.5">
                {MODEL_PURPOSES.map(mp => {
                  const current = empModels.find(m => m.purpose === mp.key)?.model_id || '';
                  const filtered = models.filter(m => m.type === mp.filterType);
                  return (
                    <div key={mp.key} className="flex items-center gap-2">
                      <span className="text-[10px] text-zinc-500 w-28 shrink-0">{mp.label}</span>
                      <select value={current} onChange={e => setModelForPurpose(mp.key, e.target.value)}
                        className="flex-1 text-xs text-zinc-300 rounded-lg px-2.5 py-1.5 outline-none border border-zinc-800/50 focus:border-cyan-500/30 cursor-pointer" style={{ background: '#111114' }}>
                        <option value="">— None —</option>
                        {filtered.map(m => <option key={m.id} value={m.model_id}>{m.name}</option>)}
                        {filtered.length === 0 && <option disabled>No {mp.filterType} models registered</option>}
                      </select>
                    </div>
                  );
                })}
              </div>
            )}
          </div>

          {/* Skills (read-only, managed from Skills page) */}
          <div>
            <button onClick={() => setExpandedSections(s => ({ ...s, skills: !s.skills }))}
              className="flex items-center gap-1.5 text-[10px] font-semibold text-zinc-600 uppercase tracking-wider cursor-pointer hover:text-zinc-400 transition-colors">
              {expandedSections.skills ? <ChevronDown size={10} /> : <ChevronRight size={10} />}
              Assigned Skills ({assignedSkills.length})
            </button>
            {expandedSections.skills && (
              <div className="mt-2 space-y-1.5">
                {assignedSkills.length > 0 ? assignedSkills.map(s => (
                  <div key={s.id} className="px-2.5 py-1.5 rounded-lg border border-zinc-800/30" style={{ background: '#09090b' }}>
                    <div className="flex items-center gap-1.5">
                      <span className="text-[10px] font-medium text-zinc-300">{s.name}</span>
                      <span className="px-1 py-0.5 text-[8px] rounded bg-zinc-800 text-zinc-500 border border-zinc-700">{s.category}</span>
                    </div>
                  </div>
                )) : (
                  <p className="text-[10px] text-zinc-600">No skills assigned</p>
                )}
                <p className="text-[9px] text-zinc-700 mt-1">Manage skill assignments in the Skills page</p>
              </div>
            )}
          </div>

          {/* Tags */}
          <div>
            <label className="text-[10px] font-semibold text-zinc-600 uppercase tracking-wider mb-1.5 block">Tags</label>
            <div className="flex items-center gap-2">
              <input value={tagInput} onChange={e => setTagInput(e.target.value)}
                onKeyDown={e => { if (e.key === 'Enter') { e.preventDefault(); addTags(); } }}
                onBlur={addTags}
                placeholder="Type tags separated by commas, press Enter"
                className="flex-1 text-xs text-zinc-300 rounded-lg px-2.5 py-2 outline-none border border-zinc-800/50 focus:border-cyan-500/30 placeholder:text-zinc-700"
                style={{ background: '#111114' }} />
            </div>
            {tags.length > 0 && (
              <div className="flex flex-wrap gap-1.5 mt-2">
                {tags.map(tag => (
                  <span key={tag} className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-[10px] font-medium text-cyan-400"
                    style={{ background: '#0e749015', border: '1px solid #0e749030' }}>
                    {tag}
                    <button onClick={() => setTags(tags.filter(t => t !== tag))} className="text-cyan-600 hover:text-cyan-300 cursor-pointer">
                      <X size={10} />
                    </button>
                  </span>
                ))}
              </div>
            )}
          </div>

          {error && <p className="text-xs text-red-400">{error}</p>}
        </div>

        <div className="flex items-center justify-end gap-3 px-6 py-4 border-t border-zinc-800/40 shrink-0">
          <button onClick={onClose}
            className="px-4 py-2 rounded-lg text-xs font-medium text-zinc-400 hover:text-zinc-200 border border-zinc-800/50 hover:border-zinc-700/60 transition-colors cursor-pointer"
            style={{ background: '#111114' }}>Cancel</button>
          <button onClick={handleSubmit} disabled={saving}
            className="px-4 py-2 rounded-lg text-xs font-medium text-white transition-colors cursor-pointer disabled:opacity-50"
            style={{ background: 'linear-gradient(135deg, #0e7490, #164e63)', border: '1px solid #0e749050' }}>
            {saving ? 'Saving...' : isEdit ? 'Save Changes' : 'Create Employee'}
          </button>
        </div>
      </div>
    </div>
  );
}
