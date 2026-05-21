import { useState, useEffect, useCallback } from 'react';
import {
  Users, Plus, Trash2, X, Pencil, ChevronDown, ChevronRight,
  Brain, Wrench, UserCog,
} from 'lucide-react';
import { listEmployees, createEmployee, updateEmployee, deleteEmployee, setEmployeeManager, listModels } from '../api';
import type { Employee, EmployeeModel, EmployeeSkill, VertexModel } from '../types';

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

  return (
    <div className="p-8 max-w-[1400px] mx-auto">
      <header className="flex items-center justify-between mb-6">
        <div>
          <div className="flex items-center gap-2.5 mb-1">
            <Users size={20} className="text-cyan-400" />
            <h2 className="text-2xl font-bold tracking-tight text-white">HR Management</h2>
          </div>
          <p className="text-xs text-zinc-600 mt-1">Manage AI agents, roles, and reporting structure</p>
        </div>
        <button
          onClick={() => setShowCreate(true)}
          className="flex items-center gap-2 px-4 py-2 rounded-lg text-xs font-medium text-white transition-colors cursor-pointer"
          style={{ background: 'linear-gradient(135deg, #0e7490, #164e63)', border: '1px solid #0e749050' }}
        >
          <Plus size={14} /> New Employee
        </button>
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
                      selected={selected}
                      onSelect={setSelected}
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

function OrgTreeNode({ node, selected, onSelect }: {
  node: TreeNode;
  selected: Employee | null;
  onSelect: (e: Employee) => void;
}) {
  const emp = node.employee;
  const color = ROLE_COLORS[emp.role] || ROLE_COLORS.Custom;
  const isSelected = selected?.id === emp.id;

  return (
    <div className="flex flex-col items-center">
      {/* Node card */}
      <button
        onClick={() => onSelect(emp)}
        className={`flex items-center gap-2.5 px-4 py-3 rounded-xl border cursor-pointer transition-all ${
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

      {/* Children */}
      {node.children.length > 0 && (
        <div className="flex flex-col items-center">
          {/* Vertical line down from parent */}
          <div className="w-px h-6" style={{ background: '#3f3f46' }} />

          {node.children.length === 1 ? (
            <OrgTreeNode node={node.children[0]} selected={selected} onSelect={onSelect} />
          ) : (
            <>
              {/* Horizontal connector bar */}
              <div className="flex items-start">
                {node.children.map((child, i) => (
                  <div key={child.employee.id} className="flex flex-col items-center" style={{ margin: '0 12px' }}>
                    {/* Horizontal + vertical lines */}
                    <div className="flex items-start w-full">
                      <div className={`h-px flex-1 ${i === 0 ? 'bg-transparent' : ''}`} style={i > 0 ? { background: '#3f3f46' } : undefined} />
                      <div className="w-px h-6" style={{ background: '#3f3f46' }} />
                      <div className={`h-px flex-1 ${i === node.children.length - 1 ? 'bg-transparent' : ''}`} style={i < node.children.length - 1 ? { background: '#3f3f46' } : undefined} />
                    </div>
                    <OrgTreeNode node={child} selected={selected} onSelect={onSelect} />
                  </div>
                ))}
              </div>
            </>
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

        {employee.skills.length > 0 && (
          <div>
            <div className="flex items-center gap-1.5 mb-1.5">
              <Wrench size={11} className="text-zinc-600" />
              <p className="text-[10px] font-semibold text-zinc-600 uppercase tracking-wider">Skills</p>
            </div>
            <div className="space-y-1">
              {employee.skills.map(s => (
                <div key={s.skill} className="px-2.5 py-1.5 rounded-lg border border-zinc-800/30" style={{ background: '#09090b' }}>
                  <p className="text-[10px] font-medium text-zinc-300">{s.skill}</p>
                  {s.description && <p className="text-[9px] text-zinc-600 mt-0.5">{s.description}</p>}
                </div>
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
  const [skills, setSkills] = useState<EmployeeSkill[]>(employee?.skills || []);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  const [expandedSections, setExpandedSections] = useState({ models: isEdit, skills: isEdit });

  const addSkill = () => setSkills([...skills, { skill: '', description: '' }]);
  const removeSkill = (i: number) => setSkills(skills.filter((_, idx) => idx !== i));
  const updateSkill = (i: number, field: keyof EmployeeSkill, value: string) => {
    const next = [...skills];
    next[i] = { ...next[i], [field]: value };
    setSkills(next);
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
        skills: skills.filter(s => s.skill.trim()),
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

          {/* Skills */}
          <div>
            <button onClick={() => setExpandedSections(s => ({ ...s, skills: !s.skills }))}
              className="flex items-center gap-1.5 text-[10px] font-semibold text-zinc-600 uppercase tracking-wider cursor-pointer hover:text-zinc-400 transition-colors">
              {expandedSections.skills ? <ChevronDown size={10} /> : <ChevronRight size={10} />}
              Skills ({skills.length})
            </button>
            {expandedSections.skills && (
              <div className="mt-2 space-y-1.5">
                {skills.map((s, i) => (
                  <div key={i} className="flex items-start gap-2">
                    <input value={s.skill} onChange={e => updateSkill(i, 'skill', e.target.value)}
                      placeholder="Skill name"
                      className="w-32 text-xs text-zinc-300 rounded-lg px-2.5 py-1.5 outline-none border border-zinc-800/50 focus:border-cyan-500/30 placeholder:text-zinc-700" style={{ background: '#111114' }} />
                    <input value={s.description} onChange={e => updateSkill(i, 'description', e.target.value)}
                      placeholder="Description"
                      className="flex-1 text-xs text-zinc-300 rounded-lg px-2.5 py-1.5 outline-none border border-zinc-800/50 focus:border-cyan-500/30 placeholder:text-zinc-700" style={{ background: '#111114' }} />
                    <button onClick={() => removeSkill(i)} className="p-1 text-zinc-700 hover:text-red-400 cursor-pointer transition-colors shrink-0 mt-0.5">
                      <Trash2 size={11} />
                    </button>
                  </div>
                ))}
                <button onClick={addSkill} className="flex items-center gap-1 text-[10px] text-cyan-400 hover:text-cyan-300 cursor-pointer transition-colors">
                  <Plus size={10} /> Add Skill
                </button>
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
