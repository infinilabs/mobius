import { useState, useEffect, useCallback } from 'react';
import { Search, ImageOff, Gamepad2, X, Film } from 'lucide-react';
import { listCreatives, assetContentUrl } from '../api';
import type { ProjectAsset } from '../types';

type FilterKey = 'All' | 'Playable' | 'Images' | 'Videos';

const FILTERS: { key: FilterKey; type?: string; tag?: string }[] = [
  { key: 'All' },
  { key: 'Playable', tag: 'playable' },
  { key: 'Images', type: 'image' },
  { key: 'Videos', type: 'video' },
];

function isPlayable(a: ProjectAsset): boolean {
  return a.tags?.includes('playable') || a.mime_type === 'text/html';
}

function isImage(a: ProjectAsset): boolean {
  return a.content_type === 'image' || a.mime_type?.startsWith('image/');
}

export default function CreativesPage() {
  const [search, setSearch] = useState('');
  const [activeFilter, setActiveFilter] = useState<FilterKey>('All');
  const [creatives, setCreatives] = useState<ProjectAsset[]>([]);
  const [loading, setLoading] = useState(false);
  const [preview, setPreview] = useState<ProjectAsset | null>(null);

  const refresh = useCallback(() => {
    const f = FILTERS.find(x => x.key === activeFilter);
    setLoading(true);
    listCreatives(search || undefined, f?.type, f?.tag)
      .then(setCreatives)
      .catch(() => setCreatives([]))
      .finally(() => setLoading(false));
  }, [search, activeFilter]);

  useEffect(() => {
    const t = setTimeout(refresh, 200);
    return () => clearTimeout(t);
  }, [refresh]);

  return (
    <div className="p-8 max-w-[1400px] mx-auto">
      <header className="mb-6">
        <h2 className="text-2xl font-bold tracking-tight text-white">Creatives</h2>
      </header>

      {/* Toolbar */}
      <div className="flex items-center gap-3 mb-5 flex-wrap">
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
      </div>

      {/* Filter Tabs */}
      <div className="flex items-center gap-2 mb-8">
        {FILTERS.map(f => (
          <button
            key={f.key}
            onClick={() => setActiveFilter(f.key)}
            className="px-3 py-1 rounded-lg text-xs font-medium transition-colors cursor-pointer flex items-center gap-1.5"
            style={{
              background: activeFilter === f.key ? '#0e749015' : '#111114',
              border: `1px solid ${activeFilter === f.key ? '#0e749050' : '#27272a40'}`,
              color: activeFilter === f.key ? '#22d3ee' : '#71717a',
            }}
          >
            {f.key === 'Playable' && <Gamepad2 size={12} />}
            {f.key}
          </button>
        ))}
      </div>

      {loading ? (
        <p className="text-zinc-600 text-xs text-center py-12">Loading creatives...</p>
      ) : creatives.length === 0 ? (
        <div
          className="rounded-xl border border-zinc-800/40 flex flex-col items-center justify-center"
          style={{ background: '#111114', minHeight: 400 }}
        >
          <ImageOff size={40} className="text-zinc-800 mb-4" />
          <p className="text-sm font-semibold text-zinc-300 mb-2">No creatives yet.</p>
          <p className="text-xs text-zinc-600 text-center max-w-sm leading-relaxed">
            Generate ad images or publish a playable ad in any conversation,
            or upload your own to a project. They appear here for quick reuse.
          </p>
        </div>
      ) : (
        <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 gap-4">
          {creatives.map(a => (
            <button
              key={a.id}
              onClick={() => setPreview(a)}
              className="group rounded-xl border border-zinc-800/40 overflow-hidden cursor-pointer hover:border-cyan-500/40 transition-colors text-left"
              style={{ background: '#111114' }}
            >
              <div className="aspect-square flex items-center justify-center bg-zinc-900/50 overflow-hidden">
                {isImage(a) ? (
                  <img src={assetContentUrl(a.project_id, a.id)} alt={a.filename} className="w-full h-full object-cover" loading="lazy" />
                ) : isPlayable(a) ? (
                  <Gamepad2 size={36} className="text-cyan-500/70" />
                ) : (
                  <Film size={36} className="text-zinc-700" />
                )}
              </div>
              <div className="p-2.5">
                <p className="text-xs text-zinc-300 truncate">{a.filename}</p>
                <div className="flex items-center gap-1 mt-1 flex-wrap">
                  {isPlayable(a) && (
                    <span className="text-[9px] px-1.5 py-0.5 rounded bg-cyan-900/40 text-cyan-300 border border-cyan-700/40">playable</span>
                  )}
                  {a.tags?.filter(t => t !== '' && t !== 'playable').slice(0, 2).map(t => (
                    <span key={t} className="text-[9px] px-1.5 py-0.5 rounded bg-zinc-800/60 text-zinc-500">{t}</span>
                  ))}
                </div>
              </div>
            </button>
          ))}
        </div>
      )}

      {preview && <CreativePreviewModal asset={preview} onClose={() => setPreview(null)} />}
    </div>
  );
}

function CreativePreviewModal({ asset, onClose }: { asset: ProjectAsset; onClose: () => void }) {
  const url = assetContentUrl(asset.project_id, asset.id);
  const playable = isPlayable(asset);
  return (
    <div className="fixed inset-0 z-[70] flex items-center justify-center bg-black/80 p-6" onClick={onClose}>
      <div className="relative max-w-[90vw] max-h-[90vh] flex flex-col" onClick={e => e.stopPropagation()}>
        <div className="flex items-center justify-between mb-2">
          <span className="text-xs text-zinc-300 font-mono truncate">{asset.relative_path}</span>
          <button onClick={onClose} className="p-1 rounded text-zinc-400 hover:text-white cursor-pointer"><X size={18} /></button>
        </div>
        {playable ? (
          <iframe src={url} title={asset.filename} className="bg-white rounded-lg border border-zinc-700" style={{ width: '420px', height: '720px', maxHeight: '80vh' }} />
        ) : (
          <img src={url} alt={asset.filename} className="rounded-lg object-contain" style={{ maxWidth: '90vw', maxHeight: '80vh' }} />
        )}
      </div>
    </div>
  );
}
