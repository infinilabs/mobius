import React, { useState, useEffect, useCallback } from 'react';
import {
  Plus, Search, FolderKanban, FileText, Database, Settings,
  Trash2, Upload, RefreshCw, X, Archive, Folder, ChevronUp, MessageSquare,
} from 'lucide-react';
import {
  listProjects, createProject, getProject, updateProject, deleteProject,
  listProjectAssets, uploadProjectAsset, deleteProjectAsset, reindexProjectAssets,
  getProjectMemory, updateProjectMemory, listEmployees, browseDirectories,
  listConversations,
} from '../api';
import type { Project, ProjectAsset, Employee } from '../types';

type Tab = 'assets' | 'memory' | 'settings';

interface ProjectsPageProps {
  onNavigateToChat?: (conversationId: string | null, agentId?: string, projectId?: string) => void;
}

export default function ProjectsPage({ onNavigateToChat }: ProjectsPageProps) {
  const [projects, setProjects] = useState<Project[]>([]);
  const [selected, setSelected] = useState<Project | null>(null);
  const [tab, setTab] = useState<Tab>('assets');
  const [searchQuery, setSearchQuery] = useState('');
  const [showCreate, setShowCreate] = useState(false);

  const refresh = useCallback(() => {
    listProjects().then(setProjects).catch(() => {});
  }, []);

  useEffect(() => { refresh(); }, [refresh]);

  const handleChat = async () => {
    if (!selected || !onNavigateToChat) return;
    const convs = await listConversations();
    const projectConv = convs.find(c => c.project_id === selected.id);
    if (projectConv) {
      onNavigateToChat(projectConv.id);
    } else {
      const agentId = selected.owner?.id;
      onNavigateToChat(null, agentId, selected.id);
    }
  };

  const handleSelect = async (id: string) => {
    const p = await getProject(id);
    setSelected(p);
    setTab('assets');
  };

  const filtered = projects.filter(p =>
    p.name.toLowerCase().includes(searchQuery.toLowerCase()) ||
    p.description.toLowerCase().includes(searchQuery.toLowerCase())
  );

  return (
    <div className="flex h-full">
      {/* Left panel - project list */}
      <div className="w-72 border-r border-zinc-800/60 flex flex-col" style={{ background: '#0c0c0f' }}>
        <div className="p-4 border-b border-zinc-800/40">
          <div className="flex items-center justify-between mb-3">
            <h2 className="text-sm font-semibold text-zinc-200">Projects</h2>
            <button onClick={() => setShowCreate(true)} className="p-1.5 rounded-lg text-zinc-500 hover:text-cyan-400 hover:bg-zinc-800/50 cursor-pointer transition-colors">
              <Plus size={16} />
            </button>
          </div>
          <div className="relative">
            <Search size={14} className="absolute left-2.5 top-1/2 -translate-y-1/2 text-zinc-600" />
            <input
              value={searchQuery}
              onChange={e => setSearchQuery(e.target.value)}
              placeholder="Search projects..."
              className="w-full pl-8 pr-3 py-1.5 rounded-lg border border-zinc-800/60 bg-zinc-900/50 text-xs text-zinc-300 outline-none placeholder:text-zinc-600"
            />
          </div>
        </div>

        <div className="flex-1 overflow-y-auto p-2">
          {filtered.map(p => (
            <button
              key={p.id}
              onClick={() => handleSelect(p.id)}
              className={`w-full text-left p-3 rounded-lg mb-1 cursor-pointer transition-all ${
                selected?.id === p.id ? 'bg-zinc-800/60 border border-zinc-700/50' : 'hover:bg-zinc-800/30 border border-transparent'
              }`}
            >
              <div className="flex items-center gap-2 mb-1">
                <FolderKanban size={14} className={selected?.id === p.id ? 'text-cyan-400' : 'text-zinc-500'} />
                <span className="text-sm font-medium text-zinc-200 truncate">{p.name}</span>
                {p.source_path ? (
                  <span className="text-[10px] px-1.5 py-0.5 rounded bg-amber-900/40 text-amber-300 border border-amber-700/40 shrink-0">Imported</span>
                ) : (
                  <span className="text-[10px] px-1.5 py-0.5 rounded bg-emerald-900/40 text-emerald-300 border border-emerald-700/40 shrink-0">Native</span>
                )}
              </div>
              <div className="flex items-center gap-3 text-[10px] text-zinc-500 pl-5">
                <span>{p.task_count} tasks</span>
                <span>{p.asset_count} assets</span>
                {p.status === 'paused' && <span className="text-amber-400">paused</span>}
              </div>
            </button>
          ))}
          {filtered.length === 0 && (
            <p className="text-xs text-zinc-600 text-center py-8">No projects found</p>
          )}
        </div>
      </div>

      {/* Right panel - detail */}
      <div className="flex-1 flex flex-col overflow-hidden">
        {selected ? (
          <>
            <ProjectHeader project={selected} onChat={onNavigateToChat ? handleChat : undefined} />
            <div className="flex border-b border-zinc-800/40 px-6">
              {(['assets', 'memory', 'settings'] as Tab[]).map(t => (
                <button
                  key={t}
                  onClick={() => setTab(t)}
                  className={`px-4 py-2.5 text-xs font-medium capitalize cursor-pointer transition-colors border-b-2 ${
                    tab === t ? 'text-cyan-400 border-cyan-400' : 'text-zinc-500 border-transparent hover:text-zinc-300'
                  }`}
                >
                  {t === 'assets' && <FileText size={13} className="inline mr-1.5 -mt-0.5" />}
                  {t === 'memory' && <Database size={13} className="inline mr-1.5 -mt-0.5" />}
                  {t === 'settings' && <Settings size={13} className="inline mr-1.5 -mt-0.5" />}
                  {t}
                </button>
              ))}
            </div>
            <div className="flex-1 overflow-y-auto p-6">
              {tab === 'assets' && <AssetsTab project={selected} />}
              {tab === 'memory' && <MemoryTab project={selected} />}
              {tab === 'settings' && <SettingsTab key={selected.id} project={selected} onUpdate={refresh} onDelete={() => { setSelected(null); refresh(); }} />}
            </div>
          </>
        ) : (
          <div className="flex-1 flex items-center justify-center">
            <div className="text-center">
              <FolderKanban size={48} className="mx-auto text-zinc-700 mb-4" />
              <p className="text-zinc-500 text-sm">Select a project to view details</p>
              <button onClick={() => setShowCreate(true)} className="mt-4 px-4 py-2 rounded-lg bg-cyan-600 hover:bg-cyan-500 text-white text-xs font-medium cursor-pointer transition-colors">
                Create Project
              </button>
            </div>
          </div>
        )}
      </div>

      {showCreate && <CreateProjectModal onClose={() => setShowCreate(false)} onCreated={() => { setShowCreate(false); refresh(); }} />}
    </div>
  );
}

function ProjectHeader({ project, onChat }: { project: Project; onChat?: () => void }) {
  const isImported = project.source_path != null;
  return (
    <div className="px-6 py-4 border-b border-zinc-800/40">
      <div className="flex items-center gap-3 mb-1">
        <h1 className="text-lg font-semibold text-white">{project.name}</h1>
        {isImported ? (
          <span className="text-[10px] px-1.5 py-0.5 rounded bg-amber-900/40 text-amber-300 border border-amber-700/40">Imported</span>
        ) : (
          <span className="text-[10px] px-1.5 py-0.5 rounded bg-emerald-900/40 text-emerald-300 border border-emerald-700/40">Native</span>
        )}
        <span className={`text-[10px] px-1.5 py-0.5 rounded ${project.status === 'active' ? 'bg-green-900/40 text-green-300 border border-green-700/40' : 'bg-amber-900/40 text-amber-300 border border-amber-700/40'}`}>
          {project.status}
        </span>
        {onChat && (
          <button onClick={onChat} className="ml-auto px-3 py-1.5 rounded-lg bg-cyan-600/20 text-cyan-400 hover:bg-cyan-600/30 text-xs font-medium cursor-pointer transition-colors flex items-center gap-1.5 border border-cyan-700/30">
            <MessageSquare size={13} /> Chat
          </button>
        )}
      </div>
      {project.description && <p className="text-xs text-zinc-400 mb-1">{project.description}</p>}
      {isImported && project.source_path && <p className="text-[10px] text-zinc-600 font-mono">{project.source_path}</p>}
      <div className="flex items-center gap-4 mt-2 text-xs text-zinc-500">
        {project.owner && <span>Owner: {project.owner.name}</span>}
        <span>{project.task_count} tasks</span>
        <span>{project.asset_count} assets</span>
      </div>
    </div>
  );
}

function AssetsTab({ project }: { project: Project }) {
  const [assets, setAssets] = useState<ProjectAsset[]>([]);
  const [query, setQuery] = useState('');
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    setLoading(true);
    listProjectAssets(project.id, query || undefined).then(setAssets).catch(() => {}).finally(() => setLoading(false));
  }, [project.id, query]);

  const refresh = useCallback(() => {
    setLoading(true);
    listProjectAssets(project.id, query || undefined).then(setAssets).catch(() => {}).finally(() => setLoading(false));
  }, [project.id, query]);

  const handleUpload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;
    await uploadProjectAsset(project.id, file);
    refresh();
    e.target.value = '';
  };

  const handleReindex = async () => {
    setLoading(true);
    await reindexProjectAssets(project.id);
    refresh();
  };

  const handleDelete = async (assetId: string) => {
    await deleteProjectAsset(project.id, assetId);
    refresh();
  };

  return (
    <div>
      <div className="flex items-center gap-3 mb-4">
        <div className="relative flex-1">
          <Search size={14} className="absolute left-2.5 top-1/2 -translate-y-1/2 text-zinc-600" />
          <input
            value={query}
            onChange={e => setQuery(e.target.value)}
            placeholder="Search assets..."
            className="w-full pl-8 pr-3 py-2 rounded-lg border border-zinc-800/60 bg-zinc-900/50 text-sm text-zinc-300 outline-none placeholder:text-zinc-600"
          />
        </div>
        <label className="px-3 py-2 rounded-lg bg-zinc-800/50 text-zinc-400 hover:text-zinc-200 text-xs font-medium cursor-pointer transition-colors flex items-center gap-1.5 border border-zinc-700/40">
          <Upload size={14} /> Upload
          <input type="file" onChange={handleUpload} className="hidden" />
        </label>
        <button onClick={handleReindex} className="px-3 py-2 rounded-lg bg-zinc-800/50 text-zinc-400 hover:text-zinc-200 text-xs font-medium cursor-pointer transition-colors flex items-center gap-1.5 border border-zinc-700/40">
          <RefreshCw size={14} /> Reindex
        </button>
      </div>

      {loading ? (
        <p className="text-zinc-600 text-xs text-center py-8">Loading...</p>
      ) : assets.length === 0 ? (
        <p className="text-zinc-600 text-xs text-center py-8">No assets found</p>
      ) : (
        <div className="grid gap-2">
          {assets.map(a => (
            <div key={a.id} className="flex items-center gap-3 px-4 py-3 rounded-lg border border-zinc-800/40 bg-zinc-900/30 hover:border-zinc-700/60 transition-colors">
              <FileText size={16} className="text-zinc-500 shrink-0" />
              <div className="flex-1 min-w-0">
                <p className="text-sm text-zinc-200 truncate">{a.relative_path}</p>
                <div className="flex items-center gap-3 text-[10px] text-zinc-500 mt-0.5">
                  <span>{a.content_type}</span>
                  <span>{(a.size_bytes / 1024).toFixed(1)} KB</span>
                  <span className={a.gcs_status === 'synced' ? 'text-green-500' : a.gcs_status === 'failed' ? 'text-red-400' : 'text-amber-400'}>
                    {a.gcs_status}
                  </span>
                  {a.content_truncated && <span className="text-amber-400">truncated</span>}
                </div>
              </div>
              <button onClick={() => handleDelete(a.id)} className="p-1.5 rounded text-zinc-600 hover:text-red-400 cursor-pointer transition-colors">
                <Trash2 size={14} />
              </button>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

function MemoryTab({ project }: { project: Project }) {
  const [content, setContent] = useState('');
  const [editing, setEditing] = useState(false);
  const [editValue, setEditValue] = useState('');

  useEffect(() => {
    getProjectMemory(project.id).then(r => { setContent(r.content); setEditValue(r.content); }).catch(() => {});
  }, [project.id]);

  const handleSave = async () => {
    await updateProjectMemory(project.id, editValue);
    setContent(editValue);
    setEditing(false);
  };

  return (
    <div>
      <div className="flex items-center justify-between mb-4">
        <div>
          <h3 className="text-sm font-medium text-zinc-200">Project Memory (mobius.md)</h3>
          <p className="text-[10px] text-zinc-500 mt-0.5">{(content.length / 1024).toFixed(1)} KB</p>
        </div>
        {!editing ? (
          <button onClick={() => { setEditValue(content); setEditing(true); }} className="px-3 py-1.5 rounded-lg bg-zinc-800/50 text-zinc-400 hover:text-zinc-200 text-xs cursor-pointer transition-colors border border-zinc-700/40">
            Edit
          </button>
        ) : (
          <div className="flex gap-2">
            <button onClick={handleSave} className="px-3 py-1.5 rounded-lg bg-cyan-600 hover:bg-cyan-500 text-white text-xs cursor-pointer transition-colors">Save</button>
            <button onClick={() => setEditing(false)} className="px-3 py-1.5 rounded-lg bg-zinc-800/50 text-zinc-400 text-xs cursor-pointer transition-colors border border-zinc-700/40">Cancel</button>
          </div>
        )}
      </div>
      {editing ? (
        <textarea
          value={editValue}
          onChange={e => setEditValue(e.target.value)}
          className="w-full h-[500px] p-4 rounded-lg border border-zinc-800/60 bg-zinc-900/50 text-sm text-zinc-300 font-mono outline-none resize-none"
        />
      ) : (
        <pre className="w-full p-4 rounded-lg border border-zinc-800/40 bg-zinc-900/30 text-sm text-zinc-300 font-mono whitespace-pre-wrap overflow-y-auto max-h-[500px]">
          {content || 'No memory file found.'}
        </pre>
      )}
    </div>
  );
}

function SettingsTab({ project, onUpdate, onDelete }: { project: Project; onUpdate: () => void; onDelete: () => void }) {
  const [description, setDescription] = useState(project.description);
  const [status, setStatus] = useState(project.status);
  const [confirmDelete, setConfirmDelete] = useState('');

  const handleSave = async () => {
    await updateProject(project.id, { description, status });
    onUpdate();
  };

  const handleArchive = async () => {
    if (confirmDelete !== project.name) return;
    await deleteProject(project.id, 'archive');
    onDelete();
  };

  const handleDelete = async () => {
    if (confirmDelete !== project.name) return;
    await deleteProject(project.id, 'delete');
    onDelete();
  };

  return (
    <div className="max-w-lg space-y-6">
      <div>
        <label className="text-xs text-zinc-500 uppercase tracking-wider block mb-1">Name (immutable)</label>
        <input value={project.name} disabled readOnly className="w-full px-3 py-2 rounded-lg border border-zinc-800/60 bg-zinc-950 text-sm text-zinc-500 outline-none cursor-not-allowed opacity-60" />
      </div>
      <div>
        <label className="text-xs text-zinc-500 uppercase tracking-wider block mb-1">Description</label>
        <textarea value={description} onChange={e => setDescription(e.target.value)} rows={3} className="w-full px-3 py-2 rounded-lg border border-zinc-800 bg-zinc-900/50 text-sm text-zinc-200 outline-none resize-none" />
      </div>
      <div>
        <label className="text-xs text-zinc-500 uppercase tracking-wider block mb-1">Status</label>
        <select value={status} onChange={e => setStatus(e.target.value as 'active' | 'paused')} className="w-full px-3 py-2 rounded-lg border border-zinc-800 bg-zinc-900/50 text-sm text-zinc-200 outline-none cursor-pointer">
          <option value="active">Active</option>
          <option value="paused">Paused</option>
        </select>
      </div>
      <button onClick={handleSave} className="px-4 py-2 rounded-lg bg-cyan-600 hover:bg-cyan-500 text-white text-sm font-medium cursor-pointer transition-colors">
        Save Changes
      </button>

      <div className="border-t border-zinc-800/40 pt-6">
        <h4 className="text-sm font-medium text-red-400 mb-2">Danger Zone</h4>
        <p className="text-xs text-zinc-500 mb-3">Type the project name <strong className="text-zinc-300">{project.name}</strong> to confirm.</p>
        <input
          value={confirmDelete}
          onChange={e => setConfirmDelete(e.target.value)}
          placeholder="Type project name..."
          className="w-full px-3 py-2 rounded-lg border border-zinc-800 bg-zinc-900/50 text-sm text-zinc-200 outline-none mb-3"
        />
        <div className="flex gap-3">
          <button
            onClick={handleArchive}
            disabled={confirmDelete !== project.name}
            className="px-4 py-2 rounded-lg bg-amber-600/20 text-amber-400 border border-amber-700/40 text-xs font-medium cursor-pointer transition-colors disabled:opacity-30 disabled:cursor-not-allowed flex items-center gap-1.5"
          >
            <Archive size={14} /> Archive
          </button>
          <button
            onClick={handleDelete}
            disabled={confirmDelete !== project.name}
            className="px-4 py-2 rounded-lg bg-red-600/20 text-red-400 border border-red-700/40 text-xs font-medium cursor-pointer transition-colors disabled:opacity-30 disabled:cursor-not-allowed flex items-center gap-1.5"
          >
            <Trash2 size={14} /> Delete
          </button>
        </div>
      </div>
    </div>
  );
}

function CreateProjectModal({ onClose, onCreated }: { onClose: () => void; onCreated: () => void }) {
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [ownerId, setOwnerId] = useState('');
  const [sourcePath, setSourcePath] = useState('');
  const [showFolderPicker, setShowFolderPicker] = useState(false);
  const [employees, setEmployees] = useState<Employee[]>([]);
  const [error, setError] = useState('');

  useEffect(() => {
    listEmployees().then(emps => {
      setEmployees(emps);
      const ceo = emps.find(e => e.role === 'CEO');
      if (ceo) setOwnerId(ceo.id);
    }).catch(() => {});
  }, []);

  const handleSubmit = async () => {
    setError('');
    try {
      await createProject({
        name,
        description,
        owner_id: ownerId || undefined,
        source_path: sourcePath || undefined,
      });
      onCreated();
    } catch (e: unknown) {
      const axErr = e as { response?: { data?: string }; message?: string };
      setError(axErr.response?.data || axErr.message || 'Failed to create project');
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60">
      <div className="w-full max-w-md rounded-xl border border-zinc-800/60 p-6" style={{ background: '#111114' }}>
        <div className="flex items-center justify-between mb-5">
          <h2 className="text-sm font-semibold text-zinc-200">New Project</h2>
          <button onClick={onClose} className="text-zinc-500 hover:text-zinc-300 cursor-pointer"><X size={16} /></button>
        </div>

        <div className="space-y-4">
          <div>
            <label className="text-xs text-zinc-500 uppercase tracking-wider block mb-1">Name *</label>
            <input value={name} onChange={e => setName(e.target.value)} placeholder="e.g. q3-campaign" className="w-full px-3 py-2 rounded-lg border border-zinc-800 bg-zinc-900/50 text-sm text-zinc-200 outline-none" />
          </div>
          <div>
            <label className="text-xs text-zinc-500 uppercase tracking-wider block mb-1">Description</label>
            <textarea value={description} onChange={e => setDescription(e.target.value)} rows={2} className="w-full px-3 py-2 rounded-lg border border-zinc-800 bg-zinc-900/50 text-sm text-zinc-200 outline-none resize-none" />
          </div>
          <div>
            <label className="text-xs text-zinc-500 uppercase tracking-wider block mb-1">Owner</label>
            <select value={ownerId} onChange={e => setOwnerId(e.target.value)} className="w-full px-3 py-2 rounded-lg border border-zinc-800 bg-zinc-900/50 text-sm text-zinc-200 outline-none cursor-pointer">
              <option value="">No owner</option>
              {employees.map(e => <option key={e.id} value={e.id}>{e.name} - {e.title}</option>)}
            </select>
          </div>
          <div>
            <label className="text-xs text-zinc-500 uppercase tracking-wider block mb-1">Import Path (optional)</label>
            <div className="flex items-center gap-2">
              <div className="flex-1 px-3 py-2 rounded-lg border border-zinc-800 bg-zinc-900/50 text-sm font-mono min-h-[36px] flex items-center">
                {sourcePath ? (
                  <span className="text-zinc-200 truncate">{sourcePath}</span>
                ) : (
                  <span className="text-zinc-600">No folder selected</span>
                )}
              </div>
              <button onClick={() => setShowFolderPicker(true)} className="px-3 py-2 rounded-lg bg-zinc-800/50 text-zinc-400 hover:text-zinc-200 text-xs font-medium cursor-pointer transition-colors border border-zinc-700/40 shrink-0 flex items-center gap-1.5">
                <Folder size={14} /> Browse
              </button>
              {sourcePath && (
                <button onClick={() => setSourcePath('')} className="p-2 rounded-lg text-zinc-600 hover:text-zinc-300 cursor-pointer transition-colors">
                  <X size={14} />
                </button>
              )}
            </div>
            <p className="text-[10px] text-zinc-600 mt-1">Leave empty for a new template project</p>
          </div>

          {error && <p className="text-xs text-red-400">{error}</p>}

          <div className="flex justify-end gap-3 pt-2">
            <button onClick={onClose} className="px-4 py-2 rounded-lg text-zinc-400 hover:text-zinc-200 text-sm cursor-pointer transition-colors">Cancel</button>
            <button onClick={handleSubmit} disabled={!name} className="px-4 py-2 rounded-lg bg-cyan-600 hover:bg-cyan-500 text-white text-sm font-medium cursor-pointer transition-colors disabled:opacity-40 disabled:cursor-not-allowed">Create</button>
          </div>
        </div>
      </div>

      {showFolderPicker && (
        <FolderPickerModal
          onSelect={(path) => { setSourcePath(path); setShowFolderPicker(false); }}
          onClose={() => setShowFolderPicker(false)}
        />
      )}
    </div>
  );
}

function FolderPickerModal({ onSelect, onClose }: { onSelect: (path: string) => void; onClose: () => void }) {
  const [currentPath, setCurrentPath] = useState('');
  const [parentPath, setParentPath] = useState('');
  const [dirs, setDirs] = useState<{ name: string; path: string }[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  const navigate = useCallback((path?: string) => {
    setLoading(true);
    setError('');
    browseDirectories(path)
      .then(data => {
        setCurrentPath(data.current);
        setParentPath(data.parent);
        setDirs(data.dirs);
      })
      .catch(() => setError('Cannot access this directory'))
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => { navigate(); }, [navigate]);

  return (
    <div className="fixed inset-0 z-[60] flex items-center justify-center bg-black/60">
      <div className="w-full max-w-lg rounded-xl border border-zinc-800/60 flex flex-col" style={{ background: '#111114', maxHeight: '70vh' }}>
        <div className="flex items-center justify-between p-4 border-b border-zinc-800/40">
          <h2 className="text-sm font-semibold text-zinc-200">Select Folder</h2>
          <button onClick={onClose} className="text-zinc-500 hover:text-zinc-300 cursor-pointer"><X size={16} /></button>
        </div>

        <div className="flex items-center gap-2 px-4 py-2 border-b border-zinc-800/40 bg-zinc-900/30">
          <button
            onClick={() => navigate(parentPath)}
            disabled={currentPath === parentPath}
            className="p-1.5 rounded text-zinc-500 hover:text-zinc-300 cursor-pointer transition-colors disabled:opacity-30 disabled:cursor-not-allowed"
          >
            <ChevronUp size={16} />
          </button>
          <span className="text-xs text-zinc-400 font-mono truncate flex-1">{currentPath}</span>
        </div>

        <div className="flex-1 overflow-y-auto p-2 min-h-[200px]">
          {loading ? (
            <p className="text-xs text-zinc-600 text-center py-8">Loading...</p>
          ) : error ? (
            <p className="text-xs text-red-400 text-center py-8">{error}</p>
          ) : dirs.length === 0 ? (
            <p className="text-xs text-zinc-600 text-center py-8">No subdirectories</p>
          ) : (
            dirs.map(d => (
              <button
                key={d.path}
                onClick={() => navigate(d.path)}
                className="w-full text-left flex items-center gap-2.5 px-3 py-2 rounded-lg hover:bg-zinc-800/40 cursor-pointer transition-colors"
              >
                <Folder size={15} className="text-amber-500/70 shrink-0" />
                <span className="text-sm text-zinc-300 truncate">{d.name}</span>
              </button>
            ))
          )}
        </div>

        <div className="flex justify-end gap-3 p-4 border-t border-zinc-800/40">
          <button onClick={onClose} className="px-4 py-2 rounded-lg text-zinc-400 hover:text-zinc-200 text-sm cursor-pointer transition-colors">Cancel</button>
          <button onClick={() => onSelect(currentPath)} className="px-4 py-2 rounded-lg bg-cyan-600 hover:bg-cyan-500 text-white text-sm font-medium cursor-pointer transition-colors">
            Select This Folder
          </button>
        </div>
      </div>
    </div>
  );
}
