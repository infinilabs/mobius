import { useState, useEffect, useCallback } from 'react';
import {
  Search, Filter, X, Gamepad2, Film, ImageOff, Sparkles,
  Download, Loader2, Plus, Check, Monitor, FolderOpen,
} from 'lucide-react';
import {
  listCreatives, listCreativeTags, updateCreativeMeta, uploadCreative,
  addAssetToCreatives, listProjects, listProjectAssets, assetContentUrl, type CreativeFilters,
} from '../api';
import RefreshButton from '../components/RefreshButton';
import {
  AssetMediaPreview, isImageAsset, isVideoAsset, isPlayableAsset, isCreativeEligible,
} from '../components/AssetPreview';
import type { ProjectAsset, Project } from '../types';

const ASSET_SOURCES = [
  { key: '', label: 'All' },
  { key: 'ai_generated', label: 'AI Generated' },
  { key: 'local', label: 'Local' },
];
const TYPES = [
  { key: 'image', label: 'Image' },
  { key: 'video', label: 'Video' },
];
const ASPECT_RATIOS = ['9:16', '1:1', '4:5', '16:9', 'other'];
const STATUSES = ['draft', 'ready'];

function fmtDate(a: ProjectAsset): string {
  const d = a.published_at || a.created_at;
  return d ? d.slice(0, 10) : '';
}

// daysAgoISO returns an RFC3339 timestamp N days before now, for the date presets.
function daysAgoISO(days: number): string {
  return new Date(Date.now() - days * 86400000).toISOString();
}

export default function CreativesPage() {
  const [search, setSearch] = useState('');
  const [filters, setFilters] = useState<CreativeFilters>({});
  const [activeTag, setActiveTag] = useState('');
  const [creatives, setCreatives] = useState<ProjectAsset[]>([]);
  const [tags, setTags] = useState<string[]>([]);
  const [loading, setLoading] = useState(false);
  const [showFilter, setShowFilter] = useState(false);
  const [showAdd, setShowAdd] = useState(false);
  const [selected, setSelected] = useState<ProjectAsset | null>(null);

  const refresh = useCallback(() => {
    setLoading(true);
    listCreatives({ ...filters, q: search || undefined, tag: activeTag || filters.tag })
      .then(setCreatives)
      .catch(() => setCreatives([]))
      .finally(() => setLoading(false));
  }, [filters, search, activeTag]);

  useEffect(() => {
    const t = setTimeout(refresh, 200);
    return () => clearTimeout(t);
  }, [refresh]);

  useEffect(() => {
    listCreativeTags().then(setTags).catch(() => setTags([]));
  }, [creatives.length]);

  const activeFilterCount = Object.values(filters).filter(Boolean).length;

  return (
    <div className="p-8 max-w-[1400px] mx-auto">
      <header className="mb-5 flex items-center justify-between">
        <h2 className="text-2xl font-bold tracking-tight text-white">Creatives</h2>
        <RefreshButton onClick={refresh} loading={loading} />
      </header>

      {/* Toolbar: search + Upload + Filter */}
      <div className="flex items-center gap-3 mb-4 flex-wrap">
        <div className="relative flex-1 min-w-[240px] max-w-[520px]">
          <Search size={14} className="absolute left-3 top-1/2 -translate-y-1/2 text-zinc-600" />
          <input
            type="text"
            value={search}
            onChange={e => setSearch(e.target.value)}
            placeholder="Search by name, tag, or description..."
            className="w-full text-xs text-zinc-300 rounded-lg pl-9 pr-3 py-2.5 outline-none border border-zinc-800/50 transition-colors focus:border-cyan-500/30 placeholder:text-zinc-700"
            style={{ background: '#111114' }}
          />
        </div>
        <button
          onClick={() => setShowAdd(true)}
          className="flex items-center gap-1.5 px-3 py-2.5 rounded-lg text-xs font-medium text-zinc-300 border border-zinc-800/50 hover:border-zinc-700/60 cursor-pointer"
          style={{ background: '#111114' }}
        >
          <Plus size={14} /> Add
        </button>
        <div className="relative">
          <button
            onClick={() => setShowFilter(v => !v)}
            className="flex items-center gap-1.5 px-3 py-2.5 rounded-lg text-xs font-medium border cursor-pointer"
            style={{
              background: showFilter || activeFilterCount > 0 ? '#0e749015' : '#111114',
              borderColor: showFilter || activeFilterCount > 0 ? '#0e749050' : '#27272a80',
              color: showFilter || activeFilterCount > 0 ? '#22d3ee' : '#a1a1aa',
            }}
          >
            <Filter size={14} /> Filter{activeFilterCount > 0 ? ` (${activeFilterCount})` : ''}
          </button>
          {showFilter && (
            <FilterPopover
              filters={filters}
              onChange={setFilters}
              onClose={() => setShowFilter(false)}
            />
          )}
        </div>
      </div>

      {/* Quick tag chips */}
      <div className="flex items-center gap-2 mb-7 flex-wrap">
        <Chip label="All" active={!activeTag && filters.origin !== 'ai_generated'} onClick={() => { setActiveTag(''); setFilters(f => ({ ...f, origin: undefined })); }} />
        <Chip
          label="AI"
          icon={<Sparkles size={11} />}
          active={filters.origin === 'ai_generated'}
          onClick={() => setFilters(f => ({ ...f, origin: f.origin === 'ai_generated' ? undefined : 'ai_generated' }))}
        />
        {tags.map(t => (
          <Chip key={t} label={t} active={activeTag === t} onClick={() => setActiveTag(activeTag === t ? '' : t)} />
        ))}
      </div>

      {loading ? (
        <p className="text-zinc-600 text-xs text-center py-12">Loading creatives...</p>
      ) : creatives.length === 0 ? (
        <div className="rounded-xl border border-zinc-800/40 flex flex-col items-center justify-center" style={{ background: '#111114', minHeight: 400 }}>
          <ImageOff size={40} className="text-zinc-800 mb-4" />
          <p className="text-sm font-semibold text-zinc-300 mb-2">No creatives yet.</p>
          <p className="text-xs text-zinc-600 text-center max-w-sm leading-relaxed">
            Add images, videos, or playable ads to the Creatives library from a project's
            assets, or upload directly here.
          </p>
        </div>
      ) : (
        <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 gap-4">
          {creatives.map(a => (
            <CreativeCard key={a.id} asset={a} onClick={() => setSelected(a)} />
          ))}
        </div>
      )}

      {selected && (
        <CreativeDetail
          asset={selected}
          onClose={() => setSelected(null)}
          onSaved={(updated) => { setSelected(updated); refresh(); }}
        />
      )}
      {showAdd && <AddModal onClose={() => setShowAdd(false)} onAdded={refresh} />}
    </div>
  );
}

function Chip({ label, icon, active, onClick }: { label: string; icon?: React.ReactNode; active: boolean; onClick: () => void }) {
  return (
    <button
      onClick={onClick}
      className="px-3 py-1 rounded-full text-xs font-medium transition-colors cursor-pointer flex items-center gap-1.5"
      style={{
        background: active ? '#0e749015' : '#111114',
        border: `1px solid ${active ? '#0e749050' : '#27272a40'}`,
        color: active ? '#22d3ee' : '#71717a',
      }}
    >
      {icon}{label}
    </button>
  );
}

function CreativeCard({ asset, onClick }: { asset: ProjectAsset; onClick: () => void }) {
  const draft = asset.status !== 'ready';
  return (
    <button
      onClick={onClick}
      className="group rounded-xl border border-zinc-800/40 overflow-hidden cursor-pointer hover:border-cyan-500/40 transition-colors text-left"
      style={{ background: '#111114' }}
    >
      <div className="relative aspect-square flex items-center justify-center bg-zinc-900/50 overflow-hidden">
        {isImageAsset(asset) ? (
          <img src={assetContentUrl(asset.project_id, asset.id)} alt={asset.title || asset.filename} className="w-full h-full object-cover" loading="lazy" />
        ) : isPlayableAsset(asset) ? (
          <Gamepad2 size={36} className="text-cyan-500/70" />
        ) : isVideoAsset(asset) ? (
          <Film size={36} className="text-zinc-600" />
        ) : (
          <Film size={36} className="text-zinc-700" />
        )}
        {asset.aspect_ratio && asset.aspect_ratio !== 'other' && (
          <span className="absolute top-2 right-2 text-[9px] px-1.5 py-0.5 rounded bg-black/60 text-zinc-200">{asset.aspect_ratio}</span>
        )}
        <span className={`absolute top-2 left-2 text-[9px] px-1.5 py-0.5 rounded border ${draft ? 'bg-amber-900/60 text-amber-200 border-amber-700/50' : 'bg-emerald-900/60 text-emerald-200 border-emerald-700/50'}`}>
          {draft ? 'Draft' : 'Ready'}
        </span>
      </div>
      <div className="p-2.5">
        <p className="text-xs text-zinc-300 truncate">{asset.title || asset.filename}</p>
        <div className="flex items-center gap-1 mt-1 flex-wrap">
          {asset.tags?.filter(t => t !== '' && t !== 'creative').slice(0, 2).map(t => (
            <span key={t} className={`text-[9px] px-1.5 py-0.5 rounded ${t === 'playable' ? 'bg-cyan-900/40 text-cyan-300 border border-cyan-700/40' : 'bg-zinc-800/60 text-zinc-500'}`}>{t}</span>
          ))}
        </div>
        <p className="text-[9px] text-zinc-600 mt-1">{fmtDate(asset)}</p>
      </div>
    </button>
  );
}

function FilterPopover({ filters, onChange, onClose }: {
  filters: CreativeFilters;
  onChange: (f: CreativeFilters) => void;
  onClose: () => void;
}) {
  const set = (key: keyof CreativeFilters, value: string) => {
    onChange({ ...filters, [key]: filters[key] === value ? undefined : value });
  };
  const datePreset = (days: number) => onChange({ ...filters, date_from: daysAgoISO(days), date_to: undefined });

  const Section = ({ title, children }: { title: string; children: React.ReactNode }) => (
    <div className="mb-3">
      <p className="text-[10px] font-semibold text-zinc-500 uppercase tracking-wider mb-1.5">{title}</p>
      <div className="flex flex-wrap gap-1.5">{children}</div>
    </div>
  );
  const Opt = ({ label, active, onClick }: { label: string; active: boolean; onClick: () => void }) => (
    <button onClick={onClick} className="px-2.5 py-1 rounded-md text-[11px] cursor-pointer border"
      style={{ background: active ? '#0e749015' : '#0a0a0d', borderColor: active ? '#0e749050' : '#27272a80', color: active ? '#22d3ee' : '#a1a1aa' }}>
      {label}
    </button>
  );

  return (
    <div className="absolute right-0 top-full mt-2 z-50 w-[280px] rounded-xl border border-zinc-800/60 shadow-2xl p-4" style={{ background: '#0c0c0f' }}>
      <div className="flex items-center justify-between mb-3">
        <span className="text-sm font-semibold text-zinc-200">Filter</span>
        <button onClick={() => { onChange({}); }} className="text-[11px] text-zinc-500 hover:text-zinc-300 cursor-pointer">Clear All</button>
      </div>
      <Section title="Asset Source">
        {ASSET_SOURCES.map(s => (
          <Opt key={s.key} label={s.label} active={(filters.origin || '') === s.key} onClick={() => onChange({ ...filters, origin: s.key || undefined })} />
        ))}
      </Section>
      <Section title="Type">
        {TYPES.map(t => <Opt key={t.key} label={t.label} active={filters.type === t.key} onClick={() => set('type', t.key)} />)}
      </Section>
      <Section title="Aspect Ratio">
        {ASPECT_RATIOS.map(r => <Opt key={r} label={r} active={filters.aspect_ratio === r} onClick={() => set('aspect_ratio', r)} />)}
      </Section>
      <Section title="Status">
        {STATUSES.map(s => <Opt key={s} label={s === 'draft' ? 'Draft' : 'Ready'} active={filters.status === s} onClick={() => set('status', s)} />)}
      </Section>
      <Section title="Published Date">
        <Opt label="Today" active={false} onClick={() => datePreset(1)} />
        <Opt label="Last 7 Days" active={false} onClick={() => datePreset(7)} />
        <Opt label="Last 30 Days" active={false} onClick={() => datePreset(30)} />
      </Section>
      <div className="flex items-center gap-2">
        <input type="date" onChange={e => onChange({ ...filters, date_from: e.target.value ? new Date(e.target.value).toISOString() : undefined })}
          className="flex-1 px-2 py-1 rounded-md text-[11px] bg-zinc-950 border border-zinc-800 text-zinc-300 outline-none" />
        <span className="text-zinc-600 text-xs">–</span>
        <input type="date" onChange={e => onChange({ ...filters, date_to: e.target.value ? new Date(e.target.value).toISOString() : undefined })}
          className="flex-1 px-2 py-1 rounded-md text-[11px] bg-zinc-950 border border-zinc-800 text-zinc-300 outline-none" />
      </div>
      <button onClick={onClose} className="mt-3 w-full py-1.5 rounded-md text-xs text-zinc-400 hover:text-zinc-200 border border-zinc-800 cursor-pointer">Done</button>
    </div>
  );
}

function CreativeDetail({ asset, onClose, onSaved }: {
  asset: ProjectAsset;
  onClose: () => void;
  onSaved: (updated: ProjectAsset) => void;
}) {
  const ext = asset.filename.includes('.') ? '.' + asset.filename.split('.').pop() : '';
  const baseName = (asset.title || asset.filename).replace(ext, '');
  const [title, setTitle] = useState(baseName);
  const [description, setDescription] = useState(asset.description || '');
  const [status, setStatus] = useState<'draft' | 'ready'>(asset.status === 'ready' ? 'ready' : 'draft');
  const [tags, setTags] = useState<string[]>(asset.tags?.filter(t => t !== 'creative') || []);
  const [newTag, setNewTag] = useState('');
  const [saving, setSaving] = useState(false);

  const addTag = () => {
    const t = newTag.trim();
    if (t && !tags.includes(t)) setTags([...tags, t]);
    setNewTag('');
  };
  const save = async () => {
    setSaving(true);
    try {
      const updated = await updateCreativeMeta(asset.project_id, asset.id, {
        title: (title.trim() || baseName) + ext,
        description,
        status,
        tags: [...tags, 'creative'],
      });
      onSaved(updated);
    } finally {
      setSaving(false);
    }
  };

  const Row = ({ label, value }: { label: string; value: string }) => (
    <div className="flex items-center justify-between py-1.5 border-b border-zinc-800/40">
      <span className="text-[11px] text-zinc-500">{label}</span>
      <span className="text-[11px] text-zinc-300">{value}</span>
    </div>
  );

  return (
    <div className="fixed inset-0 z-[70] flex justify-end bg-black/70" onClick={onClose}>
      <div className="w-full max-w-[480px] h-full overflow-y-auto flex flex-col" style={{ background: '#0c0c0f' }} onClick={e => e.stopPropagation()}>
        <div className="flex items-center justify-between px-5 py-4 border-b border-zinc-800/40 shrink-0">
          <span className="text-sm font-semibold text-white">Creative Detail</span>
          <button onClick={onClose} className="text-zinc-500 hover:text-zinc-200 cursor-pointer"><X size={18} /></button>
        </div>

        <div className="p-5 space-y-5 flex-1">
          <div className="flex justify-center bg-zinc-900/40 rounded-lg p-3">
            <AssetMediaPreview asset={asset} />
          </div>

          {/* Status toggle */}
          <div>
            <p className="text-[10px] font-semibold text-zinc-500 uppercase tracking-wider mb-1.5">Status</p>
            <div className="flex gap-2">
              {(['draft', 'ready'] as const).map(s => (
                <button key={s} onClick={() => setStatus(s)}
                  className="flex-1 py-1.5 rounded-md text-xs font-medium cursor-pointer border"
                  style={{ background: status === s ? '#0e749015' : '#0a0a0d', borderColor: status === s ? '#0e749050' : '#27272a80', color: status === s ? '#22d3ee' : '#a1a1aa' }}>
                  {s === 'draft' ? 'Draft' : 'Ready'}
                </button>
              ))}
            </div>
          </div>

          {/* Basic info */}
          <div>
            <p className="text-[10px] font-semibold text-zinc-500 uppercase tracking-wider mb-1.5">Basic Information</p>
            <div className="relative mb-2">
              <input value={title} onChange={e => setTitle(e.target.value.slice(0, 50))}
                className="w-full pl-2.5 pr-16 py-1.5 rounded-md text-xs bg-zinc-950 border border-zinc-800 text-zinc-200 outline-none focus:border-cyan-700/50" />
              <span className="absolute right-2 top-1/2 -translate-y-1/2 text-[10px] text-zinc-600">{title.length}/50 {ext}</span>
            </div>
            <Row label="Type" value={asset.content_type} />
            <Row label="Ratio" value={asset.aspect_ratio || '—'} />
            <Row label="Date" value={fmtDate(asset)} />
            <Row label="Origin" value={asset.origin === 'ai_generated' ? 'AI Generated' : 'Local Upload'} />
          </div>

          {/* Tags */}
          <div>
            <p className="text-[10px] font-semibold text-zinc-500 uppercase tracking-wider mb-1.5">Tags</p>
            <div className="flex flex-wrap gap-1.5 items-center">
              {tags.map(t => (
                <span key={t} className="flex items-center gap-1 text-[11px] px-2 py-0.5 rounded bg-cyan-900/30 text-cyan-300 border border-cyan-700/40">
                  {t}
                  <button onClick={() => setTags(tags.filter(x => x !== t))} className="cursor-pointer hover:text-white"><X size={10} /></button>
                </span>
              ))}
              <span className="flex items-center gap-1">
                <input value={newTag} onChange={e => setNewTag(e.target.value)}
                  onKeyDown={e => { if (e.key === 'Enter') { e.preventDefault(); addTag(); } }}
                  placeholder="Add tag"
                  className="w-20 px-2 py-0.5 rounded text-[11px] bg-zinc-950 border border-zinc-800 text-zinc-300 outline-none" />
                <button onClick={addTag} className="text-zinc-500 hover:text-cyan-400 cursor-pointer"><Plus size={12} /></button>
              </span>
            </div>
          </div>

          {/* Description */}
          <div>
            <p className="text-[10px] font-semibold text-zinc-500 uppercase tracking-wider mb-1.5">Description</p>
            <textarea value={description} onChange={e => setDescription(e.target.value)} rows={3} placeholder="Add description"
              className="w-full px-2.5 py-2 rounded-md text-xs bg-zinc-950 border border-zinc-800 text-zinc-200 outline-none resize-none focus:border-cyan-700/50" />
          </div>
        </div>

        <div className="flex items-center gap-3 px-5 py-4 border-t border-zinc-800/40 shrink-0">
          <a href={assetContentUrl(asset.project_id, asset.id)} download={asset.filename}
            className="flex items-center gap-1.5 px-4 py-2 rounded-lg text-xs font-medium text-zinc-300 border border-zinc-800 hover:border-zinc-700 cursor-pointer">
            <Download size={14} /> Download
          </a>
          <button onClick={save} disabled={saving}
            className="flex-1 flex items-center justify-center gap-1.5 px-4 py-2 rounded-lg text-xs font-medium text-white cursor-pointer disabled:opacity-50"
            style={{ background: 'linear-gradient(135deg, #0e7490, #164e63)' }}>
            {saving ? <Loader2 size={14} className="animate-spin" /> : <Check size={14} />} Save
          </button>
        </div>
      </div>
    </div>
  );
}

function errMsg(e: unknown): string {
  const ax = e as { response?: { data?: string }; message?: string };
  return ax.response?.data || ax.message || 'Request failed';
}

// AddModal adds creatives from one of two sources, selected by a dropdown: the
// local computer (multi-file upload) or an existing project (multi-select its
// eligible assets). Only one source body is shown at a time.
function AddModal({ onClose, onAdded }: { onClose: () => void; onAdded: () => void }) {
  const [mode, setMode] = useState<'local' | 'project'>('local');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [progress, setProgress] = useState('');

  // Local-computer source.
  const [files, setFiles] = useState<File[]>([]);

  // Project source.
  const [projects, setProjects] = useState<Project[]>([]);
  const [projectId, setProjectId] = useState('');
  const [assets, setAssets] = useState<ProjectAsset[]>([]);
  const [loadingAssets, setLoadingAssets] = useState(false);
  const [selected, setSelected] = useState<Set<string>>(new Set());

  useEffect(() => {
    if (mode === 'project' && projects.length === 0) {
      listProjects().then(ps => { setProjects(ps); if (ps[0]) setProjectId(ps[0].id); }).catch(() => {});
    }
  }, [mode, projects.length]);

  useEffect(() => {
    if (mode !== 'project' || !projectId) return;
    setLoadingAssets(true);
    setSelected(new Set());
    listProjectAssets(projectId)
      .then(a => setAssets(a.filter(isCreativeEligible)))
      .catch(() => setAssets([]))
      .finally(() => setLoadingAssets(false));
  }, [mode, projectId]);

  const inCreatives = (a: ProjectAsset) => a.tags?.includes('creative') ?? false;
  const toggle = (id: string) => setSelected(prev => {
    const next = new Set(prev);
    if (next.has(id)) next.delete(id); else next.add(id);
    return next;
  });

  const canSubmit = mode === 'local' ? files.length > 0 : selected.size > 0;

  const submit = async () => {
    setBusy(true);
    setError('');
    let done = 0;
    let firstErr = '';
    const total = mode === 'local' ? files.length : selected.size;
    try {
      if (mode === 'local') {
        for (const f of files) {
          setProgress(`Uploading ${done + 1} of ${total}…`);
          try { await uploadCreative(f); done++; } catch (e) { if (!firstErr) firstErr = errMsg(e); }
        }
      } else {
        for (const id of [...selected]) {
          setProgress(`Adding ${done + 1} of ${total}…`);
          try { await addAssetToCreatives(projectId, id); done++; } catch (e) { if (!firstErr) firstErr = errMsg(e); }
        }
      }
      if (done > 0) onAdded();
      if (firstErr) setError(`${done} of ${total} added. ${firstErr}`);
      else onClose();
    } finally {
      setBusy(false);
      setProgress('');
    }
  };

  return (
    <div className="fixed inset-0 z-[80] flex items-center justify-center bg-black/70 p-4" onClick={onClose}>
      <div className="w-full max-w-lg rounded-xl border border-zinc-800/60 p-6 flex flex-col max-h-[88vh]" style={{ background: '#111114' }} onClick={e => e.stopPropagation()}>
        <div className="flex items-center justify-between mb-5 shrink-0">
          <h2 className="text-sm font-semibold text-zinc-200">Add Creative</h2>
          <button onClick={onClose} className="text-zinc-500 hover:text-zinc-300 cursor-pointer"><X size={16} /></button>
        </div>

        {/* Source selector */}
        <div className="mb-4 shrink-0">
          <label className="text-xs text-zinc-500 uppercase tracking-wider block mb-1">Source</label>
          <select value={mode} onChange={e => { setMode(e.target.value as 'local' | 'project'); setError(''); }}
            className="w-full px-3 py-2 rounded-lg border border-zinc-800 bg-zinc-900/50 text-sm text-zinc-200 outline-none cursor-pointer">
            <option value="local">Local Computer</option>
            <option value="project">Project</option>
          </select>
        </div>

        <div className="flex-1 overflow-y-auto min-h-0">
          {mode === 'local' ? (
            <div>
              <label className="flex flex-col items-center justify-center gap-2 px-4 py-8 rounded-lg border border-dashed border-zinc-700/60 cursor-pointer hover:border-cyan-600/50 text-zinc-400">
                <Monitor size={24} className="text-zinc-600" />
                <span className="text-xs">Choose images, videos, or playable ads</span>
                <span className="text-[10px] text-zinc-600">Multiple files supported</span>
                <input type="file" multiple accept="image/*,video/*,text/html" className="hidden"
                  onChange={e => setFiles(Array.from(e.target.files || []))} />
              </label>
              {files.length > 0 && (
                <ul className="mt-3 space-y-1">
                  {files.map((f, i) => (
                    <li key={i} className="flex items-center justify-between text-xs text-zinc-400 px-2 py-1 rounded bg-zinc-900/50">
                      <span className="truncate">{f.name}</span>
                      <button onClick={() => setFiles(files.filter((_, j) => j !== i))} className="text-zinc-600 hover:text-zinc-300 cursor-pointer"><X size={12} /></button>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          ) : (
            <div>
              <select value={projectId} onChange={e => setProjectId(e.target.value)}
                className="w-full px-3 py-2 mb-3 rounded-lg border border-zinc-800 bg-zinc-900/50 text-sm text-zinc-200 outline-none cursor-pointer">
                {projects.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
              </select>
              {loadingAssets ? (
                <div className="flex items-center justify-center py-10 text-zinc-600"><Loader2 size={16} className="animate-spin" /></div>
              ) : assets.length === 0 ? (
                <p className="text-xs text-zinc-600 text-center py-10">No images, videos, or playable ads in this project.</p>
              ) : (
                <div className="grid grid-cols-3 gap-2">
                  {assets.map(a => {
                    const already = inCreatives(a);
                    const sel = selected.has(a.id);
                    return (
                      <button key={a.id} disabled={already} onClick={() => toggle(a.id)}
                        className="relative rounded-lg border overflow-hidden text-left disabled:opacity-50 disabled:cursor-not-allowed cursor-pointer"
                        style={{ borderColor: sel ? '#0e7490' : '#27272a80', background: '#0a0a0d' }}>
                        <div className="aspect-square flex items-center justify-center bg-zinc-900/50">
                          {isImageAsset(a) ? (
                            <img src={assetContentUrl(a.project_id, a.id)} alt={a.filename} className="w-full h-full object-cover" loading="lazy" />
                          ) : isPlayableAsset(a) ? (
                            <Gamepad2 size={22} className="text-cyan-500/70" />
                          ) : (
                            <Film size={22} className="text-zinc-600" />
                          )}
                        </div>
                        <p className="text-[10px] text-zinc-400 truncate px-1.5 py-1">{a.title || a.filename}</p>
                        {sel && <span className="absolute top-1 right-1 bg-cyan-600 rounded-full p-0.5"><Check size={10} className="text-white" /></span>}
                        {already && <span className="absolute top-1 left-1 text-[8px] px-1 py-0.5 rounded bg-black/70 text-zinc-300">In Creatives</span>}
                      </button>
                    );
                  })}
                </div>
              )}
            </div>
          )}
        </div>

        {error && <p className="text-xs text-red-400 mt-3 shrink-0">{error}</p>}
        <div className="flex items-center justify-between gap-3 pt-4 shrink-0">
          <span className="text-[11px] text-zinc-500">{busy ? progress : canSubmit ? `${mode === 'local' ? files.length : selected.size} selected` : ''}</span>
          <div className="flex gap-3">
            <button onClick={onClose} className="px-4 py-2 rounded-lg text-zinc-400 hover:text-zinc-200 text-sm cursor-pointer">Cancel</button>
            <button onClick={submit} disabled={busy || !canSubmit}
              className="px-4 py-2 rounded-lg bg-cyan-600 hover:bg-cyan-500 text-white text-sm font-medium cursor-pointer disabled:opacity-40 flex items-center gap-1.5">
              {busy ? <Loader2 size={14} className="animate-spin" /> : mode === 'project' ? <FolderOpen size={14} /> : <Plus size={14} />} Add
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
