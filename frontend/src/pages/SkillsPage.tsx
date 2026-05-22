import { useState, useEffect, useCallback, useRef } from 'react';
import { Search, Plus, Trash2, X, BookOpen, Users, ChevronDown, RefreshCw } from 'lucide-react';
import {
  listSkills, createSkill, updateSkill, deleteSkill, syncSkills,
  listEmployees, listEmployeeSkills, assignSkillToEmployee, unassignSkillFromEmployee,
} from '../api';
import type { Skill, Employee } from '../types';

const CATEGORY_COLORS: Record<string, string> = {
  'software-development': 'bg-green-900/40 text-green-400 border-green-700/50',
  'devops':               'bg-blue-900/40 text-blue-400 border-blue-700/50',
  'research':             'bg-purple-900/40 text-purple-400 border-purple-700/50',
  'creative':             'bg-pink-900/40 text-pink-400 border-pink-700/50',
  'productivity':         'bg-amber-900/40 text-amber-400 border-amber-700/50',
  'general':              'bg-zinc-800 text-zinc-400 border-zinc-600',
};

function catBadge(cat: string) {
  return CATEGORY_COLORS[cat] || CATEGORY_COLORS['general'];
}

export default function SkillsPage() {
  const [skills, setSkills] = useState<Skill[]>([]);
  const [employees, setEmployees] = useState<Employee[]>([]);
  const [search, setSearch] = useState('');
  const [loading, setLoading] = useState(true);
  const [selectedSkill, setSelectedSkill] = useState<Skill | null>(null);
  const [showCreate, setShowCreate] = useState(false);
  const [tagFilter, setTagFilter] = useState<string | null>(null);

  const [assignedMap, setAssignedMap] = useState<Record<string, string[]>>({});
  const [syncing, setSyncing] = useState(false);
  const [syncMessage, setSyncMessage] = useState<string | null>(null);

  const refresh = useCallback((query?: string) => {
    setLoading(true);
    listSkills(query)
      .then(setSkills)
      .catch(() => setSkills([]))
      .finally(() => setLoading(false));
  }, []);

  const debounceRef = useRef<ReturnType<typeof setTimeout>>(null);

  useEffect(() => { refresh(); listEmployees().then(setEmployees).catch(() => {}); }, [refresh]);

  useEffect(() => {
    if (debounceRef.current) clearTimeout(debounceRef.current);
    debounceRef.current = setTimeout(() => refresh(search || undefined), 250);
    return () => { if (debounceRef.current) clearTimeout(debounceRef.current); };
  }, [search, refresh]);

  useEffect(() => {
    if (employees.length === 0) return;
    const loadAssignments = async () => {
      const map: Record<string, string[]> = {};
      for (const emp of employees) {
        try {
          const empSkills = await listEmployeeSkills(emp.id);
          for (const s of empSkills) {
            if (!map[s.id]) map[s.id] = [];
            map[s.id].push(emp.id);
          }
        } catch { /* ignore */ }
      }
      setAssignedMap(map);
    };
    loadAssignments();
  }, [employees]);

  const allTags = [...new Set(skills.flatMap(s => s.tags))].sort();

  const filtered = tagFilter
    ? skills.filter(s => s.tags.includes(tagFilter))
    : skills;

  const handleDelete = async (id: string) => {
    await deleteSkill(id);
    setSkills(prev => prev.filter(s => s.id !== id));
    if (selectedSkill?.id === id) setSelectedSkill(null);
  };

  const handleAssign = async (skillId: string, empId: string) => {
    await assignSkillToEmployee(empId, skillId);
    setAssignedMap(prev => ({
      ...prev,
      [skillId]: [...(prev[skillId] || []), empId],
    }));
  };

  const handleUnassign = async (skillId: string, empId: string) => {
    await unassignSkillFromEmployee(empId, skillId);
    setAssignedMap(prev => ({
      ...prev,
      [skillId]: (prev[skillId] || []).filter(id => id !== empId),
    }));
  };

  const handleSync = async () => {
    setSyncing(true);
    setSyncMessage(null);
    try {
      const result = await syncSkills();
      const total = result.disk_sync.added + result.disk_sync.updated;
      const srcInfo = result.sources.map(s => `${s.name}: +${s.added} ~${s.updated}${s.error ? ' (!)' : ''}`).join(', ');
      setSyncMessage(`Synced — disk: +${result.disk_sync.added} ~${result.disk_sync.updated}${srcInfo ? ` | ${srcInfo}` : ''}`);
      if (total > 0) refresh();
    } catch {
      setSyncMessage('Sync failed');
    } finally {
      setSyncing(false);
      setTimeout(() => setSyncMessage(null), 6000);
    }
  };

  const dynamicCategories = [...new Set(skills.map(s => s.category))].sort();

  return (
    <div className="h-full flex flex-col">
      {/* Header */}
      <div className="flex items-center justify-between px-6 py-4 border-b border-zinc-800">
        <div className="flex items-center gap-3">
          <BookOpen size={20} className="text-green-400" />
          <h1 className="text-lg font-semibold text-zinc-100">Skills</h1>
          <span className="text-sm text-zinc-500">{filtered.length} skills</span>
        </div>
        <div className="flex items-center gap-3">
          <div className="relative">
            <Search size={16} className="absolute left-3 top-1/2 -translate-y-1/2 text-zinc-500" />
            <input
              type="text"
              placeholder="Search skills..."
              value={search}
              onChange={e => setSearch(e.target.value)}
              className="pl-9 pr-3 py-1.5 bg-zinc-800 border border-zinc-700 rounded-lg text-sm text-zinc-200 placeholder-zinc-500 focus:outline-none focus:border-zinc-500 w-64"
            />
          </div>
          <button
            onClick={handleSync}
            disabled={syncing}
            className="flex items-center gap-1.5 px-3 py-1.5 bg-blue-600 hover:bg-blue-500 text-white text-sm rounded-lg transition-colors disabled:opacity-50"
          >
            <RefreshCw size={14} className={syncing ? 'animate-spin' : ''} /> Sync
          </button>
          <button
            onClick={() => setShowCreate(true)}
            className="flex items-center gap-1.5 px-3 py-1.5 bg-green-600 hover:bg-green-500 text-white text-sm rounded-lg transition-colors"
          >
            <Plus size={14} /> Add Skill
          </button>
        </div>
      </div>

      {/* Sync status toast */}
      {syncMessage && (
        <div className="px-6 py-2 bg-blue-900/30 border-b border-blue-800/50">
          <span className="text-xs text-blue-300">{syncMessage}</span>
        </div>
      )}

      {/* Tag filter bar */}
      {allTags.length > 0 && (
        <div className="flex items-center gap-2 px-6 py-2 border-b border-zinc-800 overflow-x-auto">
          <span className="text-xs text-zinc-500 shrink-0">Tags:</span>
          <button
            onClick={() => setTagFilter(null)}
            className={`px-2 py-0.5 text-xs rounded-full border transition-colors ${
              tagFilter === null ? 'bg-zinc-600 text-zinc-100 border-zinc-500' : 'bg-zinc-800 text-zinc-400 border-zinc-700 hover:border-zinc-500'
            }`}
          >All</button>
          {allTags.map(tag => (
            <button
              key={tag}
              onClick={() => setTagFilter(tagFilter === tag ? null : tag)}
              className={`px-2 py-0.5 text-xs rounded-full border transition-colors ${
                tagFilter === tag ? 'bg-green-900/50 text-green-400 border-green-700' : 'bg-zinc-800 text-zinc-400 border-zinc-700 hover:border-zinc-500'
              }`}
            >{tag}</button>
          ))}
        </div>
      )}

      {/* Content */}
      <div className="flex-1 overflow-y-auto p-6">
        {loading ? (
          <div className="text-zinc-500 text-sm">Loading skills...</div>
        ) : filtered.length === 0 ? (
          <div className="text-zinc-500 text-sm">No skills found.</div>
        ) : (
          <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
            {filtered.map(skill => (
              <SkillCard
                key={skill.id}
                skill={skill}
                assignedEmployeeIds={assignedMap[skill.id] || []}
                employees={employees}
                onSelect={() => setSelectedSkill(skill)}
                onDelete={() => handleDelete(skill.id)}
              />
            ))}
          </div>
        )}
      </div>

      {/* Detail / Edit Modal */}
      {selectedSkill && (
        <SkillDetailModal
          skill={selectedSkill}
          employees={employees}
          assignedEmployeeIds={assignedMap[selectedSkill.id] || []}
          categories={dynamicCategories}
          onClose={() => setSelectedSkill(null)}
          onSave={async (updated) => {
            const saved = await updateSkill(selectedSkill.id, updated);
            setSkills(prev => prev.map(s => s.id === saved.id ? saved : s));
            setSelectedSkill(saved);
          }}
          onAssign={(empId) => handleAssign(selectedSkill.id, empId)}
          onUnassign={(empId) => handleUnassign(selectedSkill.id, empId)}
        />
      )}

      {/* Create Modal */}
      {showCreate && (
        <CreateSkillModal
          categories={dynamicCategories}
          onClose={() => setShowCreate(false)}
          onCreated={(s) => { setSkills(prev => [s, ...prev]); setShowCreate(false); }}
        />
      )}
    </div>
  );
}

function SkillCard({ skill, assignedEmployeeIds, employees, onSelect, onDelete }: {
  skill: Skill;
  assignedEmployeeIds: string[];
  employees: Employee[];
  onSelect: () => void;
  onDelete: () => void;
}) {
  const assigned = employees.filter(e => assignedEmployeeIds.includes(e.id));
  const ROLE_COLORS: Record<string, string> = {
    CEO: '#38bdf8', PM: '#c084fc', Engineer: '#4ade80', QA: '#fbbf24', Designer: '#fb7185', Custom: '#a1a1aa',
  };

  return (
    <div
      className="bg-zinc-900 border border-zinc-800 rounded-xl p-4 hover:border-zinc-600 transition-colors cursor-pointer group"
      onClick={onSelect}
    >
      <div className="flex items-start justify-between mb-2">
        <div className="flex items-center gap-2 flex-1 min-w-0">
          <h3 className="text-sm font-semibold text-zinc-100 truncate">{skill.name}</h3>
          <span className={`px-1.5 py-0.5 text-[10px] rounded border shrink-0 ${catBadge(skill.category)}`}>
            {skill.category}
          </span>
        </div>
        <button
          onClick={e => { e.stopPropagation(); onDelete(); }}
          className="opacity-0 group-hover:opacity-100 p-1 hover:text-red-400 text-zinc-500 transition-all"
        >
          <Trash2 size={14} />
        </button>
      </div>

      {skill.description && (
        <p className="text-xs text-zinc-400 mb-2 line-clamp-2">{skill.description}</p>
      )}

      <div className="flex items-center justify-between mt-3">
        <div className="flex gap-1 flex-wrap">
          {skill.tags.slice(0, 4).map(tag => (
            <span key={tag} className="px-1.5 py-0.5 text-[10px] bg-zinc-800 text-zinc-400 rounded border border-zinc-700">{tag}</span>
          ))}
          {skill.tags.length > 4 && (
            <span className="px-1.5 py-0.5 text-[10px] text-zinc-500">+{skill.tags.length - 4}</span>
          )}
        </div>

        {assigned.length > 0 && (
          <div className="flex -space-x-1.5">
            {assigned.slice(0, 4).map(emp => (
              <div
                key={emp.id}
                title={emp.name}
                className="w-5 h-5 rounded-full flex items-center justify-center text-[8px] font-bold text-white border border-zinc-900"
                style={{ backgroundColor: ROLE_COLORS[emp.role] || ROLE_COLORS.Custom }}
              >
                {emp.name[0]}
              </div>
            ))}
            {assigned.length > 4 && (
              <div className="w-5 h-5 rounded-full bg-zinc-700 flex items-center justify-center text-[8px] text-zinc-300 border border-zinc-900">
                +{assigned.length - 4}
              </div>
            )}
          </div>
        )}
      </div>

      {skill.version && (
        <div className="mt-2 text-[10px] text-zinc-600">v{skill.version}</div>
      )}
    </div>
  );
}

function SkillDetailModal({ skill, employees, assignedEmployeeIds, categories, onClose, onSave, onAssign, onUnassign }: {
  skill: Skill;
  employees: Employee[];
  assignedEmployeeIds: string[];
  categories: string[];
  onClose: () => void;
  onSave: (data: { name?: string; description?: string; category?: string; content?: string; tags?: string[]; version?: string }) => Promise<void>;
  onAssign: (empId: string) => Promise<void>;
  onUnassign: (empId: string) => Promise<void>;
}) {
  const [editing, setEditing] = useState(false);
  const [name, setName] = useState(skill.name);
  const [description, setDescription] = useState(skill.description);
  const [category, setCategory] = useState(skill.category);
  const [content, setContent] = useState(skill.content);
  const [tagsStr, setTagsStr] = useState(skill.tags.join(', '));
  const [version, setVersion] = useState(skill.version);
  const [saving, setSaving] = useState(false);
  const [showAssignDropdown, setShowAssignDropdown] = useState(false);

  useEffect(() => {
    setName(skill.name);
    setDescription(skill.description);
    setCategory(skill.category);
    setContent(skill.content);
    setTagsStr(skill.tags.join(', '));
    setVersion(skill.version);
    setEditing(false);
  }, [skill]);

  const assigned = employees.filter(e => assignedEmployeeIds.includes(e.id));
  const unassigned = employees.filter(e => !assignedEmployeeIds.includes(e.id));

  const handleSave = async () => {
    setSaving(true);
    try {
      await onSave({
        name, description, category, content, version,
        tags: tagsStr.split(',').map(t => t.trim()).filter(Boolean),
      });
      setEditing(false);
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="fixed inset-0 bg-black/70 z-50 flex items-center justify-center p-4" onClick={onClose}>
      <div className="bg-zinc-900 border border-zinc-700 rounded-xl w-full max-w-3xl max-h-[85vh] overflow-hidden flex flex-col" onClick={e => e.stopPropagation()}>
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-zinc-800">
          <div className="flex items-center gap-3">
            <BookOpen size={18} className="text-green-400" />
            {editing ? (
              <input value={name} onChange={e => setName(e.target.value)}
                className="bg-zinc-800 border border-zinc-600 rounded px-2 py-1 text-sm text-zinc-100 focus:outline-none" />
            ) : (
              <h2 className="text-base font-semibold text-zinc-100">{skill.name}</h2>
            )}
            <span className={`px-2 py-0.5 text-xs rounded border ${catBadge(skill.category)}`}>{skill.category}</span>
          </div>
          <div className="flex items-center gap-2">
            {editing ? (
              <>
                <button onClick={handleSave} disabled={saving}
                  className="px-3 py-1 bg-green-600 hover:bg-green-500 text-white text-xs rounded transition-colors disabled:opacity-50">
                  {saving ? 'Saving...' : 'Save'}
                </button>
                <button onClick={() => setEditing(false)} className="px-3 py-1 bg-zinc-700 hover:bg-zinc-600 text-zinc-300 text-xs rounded transition-colors">Cancel</button>
              </>
            ) : (
              <button onClick={() => setEditing(true)} className="px-3 py-1 bg-zinc-700 hover:bg-zinc-600 text-zinc-300 text-xs rounded transition-colors">Edit</button>
            )}
            <button onClick={onClose} className="p-1 hover:text-zinc-300 text-zinc-500"><X size={18} /></button>
          </div>
        </div>

        {/* Body */}
        <div className="flex-1 overflow-y-auto p-6 space-y-4">
          {editing ? (
            <>
              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label className="block text-xs text-zinc-400 mb-1">Category</label>
                  <select value={category} onChange={e => setCategory(e.target.value)}
                    className="w-full bg-zinc-800 border border-zinc-700 rounded px-2 py-1.5 text-sm text-zinc-200 focus:outline-none">
                    {categories.map(c => <option key={c} value={c}>{c}</option>)}
                  </select>
                </div>
                <div>
                  <label className="block text-xs text-zinc-400 mb-1">Version</label>
                  <input value={version} onChange={e => setVersion(e.target.value)}
                    className="w-full bg-zinc-800 border border-zinc-700 rounded px-2 py-1.5 text-sm text-zinc-200 focus:outline-none" />
                </div>
              </div>
              <div>
                <label className="block text-xs text-zinc-400 mb-1">Description</label>
                <input value={description} onChange={e => setDescription(e.target.value)}
                  className="w-full bg-zinc-800 border border-zinc-700 rounded px-2 py-1.5 text-sm text-zinc-200 focus:outline-none" />
              </div>
              <div>
                <label className="block text-xs text-zinc-400 mb-1">Tags (comma-separated)</label>
                <input value={tagsStr} onChange={e => setTagsStr(e.target.value)}
                  className="w-full bg-zinc-800 border border-zinc-700 rounded px-2 py-1.5 text-sm text-zinc-200 focus:outline-none" />
              </div>
              <div>
                <label className="block text-xs text-zinc-400 mb-1">Content (Markdown)</label>
                <textarea value={content} onChange={e => setContent(e.target.value)} rows={16}
                  className="w-full bg-zinc-800 border border-zinc-700 rounded px-3 py-2 text-sm text-zinc-200 font-mono focus:outline-none resize-y" />
              </div>
            </>
          ) : (
            <>
              {skill.description && (
                <p className="text-sm text-zinc-300">{skill.description}</p>
              )}
              <div className="flex flex-wrap gap-1.5">
                {skill.tags.map(tag => (
                  <span key={tag} className="px-2 py-0.5 text-xs bg-zinc-800 text-zinc-400 rounded-full border border-zinc-700">{tag}</span>
                ))}
              </div>
              <div className="bg-zinc-950 border border-zinc-800 rounded-lg p-4 max-h-80 overflow-y-auto">
                <pre className="text-xs text-zinc-300 whitespace-pre-wrap font-mono">{skill.content}</pre>
              </div>
            </>
          )}

          {/* Assigned employees */}
          <div className="border-t border-zinc-800 pt-4">
            <div className="flex items-center justify-between mb-3">
              <div className="flex items-center gap-2">
                <Users size={14} className="text-zinc-400" />
                <span className="text-sm font-medium text-zinc-300">Assigned to ({assigned.length})</span>
              </div>
              <div className="relative">
                <button
                  onClick={() => setShowAssignDropdown(!showAssignDropdown)}
                  disabled={unassigned.length === 0}
                  className="flex items-center gap-1 px-2 py-1 bg-zinc-800 hover:bg-zinc-700 text-zinc-300 text-xs rounded border border-zinc-700 transition-colors disabled:opacity-40"
                >
                  <Plus size={12} /> Assign <ChevronDown size={10} />
                </button>
                {showAssignDropdown && unassigned.length > 0 && (
                  <div className="absolute right-0 mt-1 bg-zinc-800 border border-zinc-700 rounded-lg shadow-xl z-10 w-48 py-1 max-h-48 overflow-y-auto">
                    {unassigned.map(emp => (
                      <button
                        key={emp.id}
                        onClick={() => { onAssign(emp.id); setShowAssignDropdown(false); }}
                        className="w-full text-left px-3 py-1.5 text-sm text-zinc-300 hover:bg-zinc-700 flex items-center gap-2"
                      >
                        <span className="text-xs">{emp.name}</span>
                        <span className="text-[10px] text-zinc-500">{emp.role}</span>
                      </button>
                    ))}
                  </div>
                )}
              </div>
            </div>
            {assigned.length === 0 ? (
              <p className="text-xs text-zinc-500">Not assigned to any employee.</p>
            ) : (
              <div className="flex flex-wrap gap-2">
                {assigned.map(emp => (
                  <div key={emp.id} className="flex items-center gap-1.5 px-2 py-1 bg-zinc-800 rounded-lg border border-zinc-700">
                    <span className="text-xs text-zinc-300">{emp.name}</span>
                    <span className="text-[10px] text-zinc-500">{emp.role}</span>
                    <button onClick={() => onUnassign(emp.id)} className="ml-1 text-zinc-500 hover:text-red-400">
                      <X size={12} />
                    </button>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

function CreateSkillModal({ categories, onClose, onCreated }: { categories: string[]; onClose: () => void; onCreated: (s: Skill) => void }) {
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [category, setCategory] = useState('software-development');
  const [content, setContent] = useState('');
  const [tagsStr, setTagsStr] = useState('');
  const [saving, setSaving] = useState(false);

  const handleSubmit = async () => {
    if (!name.trim() || !content.trim()) return;
    setSaving(true);
    try {
      const s = await createSkill({
        name: name.trim(),
        description: description.trim(),
        category,
        content: content.trim(),
        tags: tagsStr.split(',').map(t => t.trim()).filter(Boolean),
      });
      onCreated(s);
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="fixed inset-0 bg-black/70 z-50 flex items-center justify-center p-4" onClick={onClose}>
      <div className="bg-zinc-900 border border-zinc-700 rounded-xl w-full max-w-2xl max-h-[80vh] overflow-hidden flex flex-col" onClick={e => e.stopPropagation()}>
        <div className="flex items-center justify-between px-6 py-4 border-b border-zinc-800">
          <h2 className="text-base font-semibold text-zinc-100">Create Skill</h2>
          <button onClick={onClose} className="p-1 hover:text-zinc-300 text-zinc-500"><X size={18} /></button>
        </div>
        <div className="flex-1 overflow-y-auto p-6 space-y-4">
          <div>
            <label className="block text-xs text-zinc-400 mb-1">Name *</label>
            <input value={name} onChange={e => setName(e.target.value)} placeholder="e.g. systematic-debugging"
              className="w-full bg-zinc-800 border border-zinc-700 rounded px-2 py-1.5 text-sm text-zinc-200 focus:outline-none" />
          </div>
          <div>
            <label className="block text-xs text-zinc-400 mb-1">Description</label>
            <input value={description} onChange={e => setDescription(e.target.value)} placeholder="Short description..."
              className="w-full bg-zinc-800 border border-zinc-700 rounded px-2 py-1.5 text-sm text-zinc-200 focus:outline-none" />
          </div>
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-xs text-zinc-400 mb-1">Category</label>
              <select value={category} onChange={e => setCategory(e.target.value)}
                className="w-full bg-zinc-800 border border-zinc-700 rounded px-2 py-1.5 text-sm text-zinc-200 focus:outline-none">
                {categories.map(c => <option key={c} value={c}>{c}</option>)}
              </select>
            </div>
            <div>
              <label className="block text-xs text-zinc-400 mb-1">Tags (comma-separated)</label>
              <input value={tagsStr} onChange={e => setTagsStr(e.target.value)} placeholder="debugging, testing"
                className="w-full bg-zinc-800 border border-zinc-700 rounded px-2 py-1.5 text-sm text-zinc-200 focus:outline-none" />
            </div>
          </div>
          <div>
            <label className="block text-xs text-zinc-400 mb-1">Content (Markdown) *</label>
            <textarea value={content} onChange={e => setContent(e.target.value)} rows={12} placeholder="# Skill Title\n\n## Overview\n..."
              className="w-full bg-zinc-800 border border-zinc-700 rounded px-3 py-2 text-sm text-zinc-200 font-mono focus:outline-none resize-y" />
          </div>
        </div>
        <div className="flex justify-end gap-2 px-6 py-4 border-t border-zinc-800">
          <button onClick={onClose} className="px-4 py-1.5 bg-zinc-700 hover:bg-zinc-600 text-zinc-300 text-sm rounded transition-colors">Cancel</button>
          <button onClick={handleSubmit} disabled={saving || !name.trim() || !content.trim()}
            className="px-4 py-1.5 bg-green-600 hover:bg-green-500 text-white text-sm rounded transition-colors disabled:opacity-50">
            {saving ? 'Creating...' : 'Create Skill'}
          </button>
        </div>
      </div>
    </div>
  );
}
