import { useState, useEffect } from 'react';
import { X, Download, Pencil, Check, Sparkles, Loader2 } from 'lucide-react';
import { assetContentUrl, updateProjectAssetContent, addAssetToCreatives } from '../api';
import type { ProjectAsset } from '../types';
import {
  isPlayableAsset, isImageAsset, isVideoAsset, isAudioAsset, isPdfAsset, isTextAsset, isCreativeEligible,
} from './assetGuards';

// TextPreview lazily fetches a text/code asset's content for read-only display.
function TextPreview({ url }: { url: string }) {
  const [text, setText] = useState<string | null>(null);
  useEffect(() => {
    let cancelled = false;
    fetch(url)
      .then(r => r.text())
      .then(t => { if (!cancelled) setText(t); })
      .catch(() => { if (!cancelled) setText('(failed to load content)'); });
    return () => { cancelled = true; };
  }, [url]);
  if (text === null) {
    return <div className="flex items-center justify-center p-8 text-zinc-500"><Loader2 size={16} className="animate-spin" /></div>;
  }
  return <pre className="text-xs text-zinc-300 whitespace-pre-wrap p-4 overflow-auto max-h-[70vh]">{text}</pre>;
}

// AssetMediaPreview renders an asset by type (read-only). Reused by the project
// preview modal and the creatives detail drawer.
export function AssetMediaPreview({ asset }: { asset: ProjectAsset }) {
  const url = assetContentUrl(asset.project_id, asset.id);
  if (isPlayableAsset(asset)) {
    return <iframe src={url} title={asset.filename} className="bg-white rounded-lg border border-zinc-700" style={{ width: 360, height: 640, maxHeight: '72vh' }} />;
  }
  if (isImageAsset(asset)) {
    return <img src={url} alt={asset.filename} className="rounded-lg object-contain" style={{ maxWidth: '88vw', maxHeight: '74vh' }} />;
  }
  if (isVideoAsset(asset)) {
    return <video src={url} controls className="rounded-lg" style={{ maxWidth: '88vw', maxHeight: '74vh' }} />;
  }
  if (isAudioAsset(asset)) {
    return <audio src={url} controls className="w-[420px] max-w-[88vw]" />;
  }
  if (isPdfAsset(asset)) {
    return <iframe src={url} title={asset.filename} className="rounded-lg border border-zinc-700 bg-white" style={{ width: '80vw', height: '78vh' }} />;
  }
  if (isTextAsset(asset)) {
    return <div className="w-[70vw] max-w-[800px] rounded-lg border border-zinc-800 bg-zinc-950"><TextPreview url={url} /></div>;
  }
  return (
    <div className="flex flex-col items-center gap-3 p-10 text-zinc-500">
      <p className="text-sm">Not previewable in the browser.</p>
      <a href={url} download={asset.filename} className="flex items-center gap-1.5 text-xs px-3 py-1.5 rounded-lg border border-zinc-700 text-zinc-300 hover:border-zinc-600">
        <Download size={13} /> Download
      </a>
    </div>
  );
}

// AssetPreviewModal — preview any asset, optionally edit text/code, optionally add to
// Creatives. Used by the project Assets tab.
export function AssetPreviewModal({ asset, onClose, editable = false, onSaved, allowAddToCreatives = false }: {
  asset: ProjectAsset;
  onClose: () => void;
  editable?: boolean;
  onSaved?: (updated: ProjectAsset) => void;
  allowAddToCreatives?: boolean;
}) {
  const url = assetContentUrl(asset.project_id, asset.id);
  const showTextEdit = editable && isTextAsset(asset);
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState('');
  const [saving, setSaving] = useState(false);
  const [adding, setAdding] = useState(false);
  const [added, setAdded] = useState(asset.tags?.includes('creative') ?? false);

  const startEdit = async () => {
    try {
      const t = await fetch(url).then(r => r.text());
      setDraft(t);
      setEditing(true);
    } catch { /* ignore */ }
  };
  const save = async () => {
    setSaving(true);
    try {
      const updated = await updateProjectAssetContent(asset.project_id, asset.id, draft);
      setEditing(false);
      onSaved?.(updated);
    } finally {
      setSaving(false);
    }
  };
  const add = async () => {
    setAdding(true);
    try {
      const updated = await addAssetToCreatives(asset.project_id, asset.id);
      setAdded(true);
      onSaved?.(updated);
    } finally {
      setAdding(false);
    }
  };

  const btn = 'flex items-center gap-1 text-[11px] px-2 py-1 rounded-md border cursor-pointer disabled:opacity-50';

  return (
    <div className="fixed inset-0 z-[70] flex items-center justify-center bg-black/80 p-6" onClick={onClose}>
      <div className="relative max-w-[92vw] max-h-[92vh] flex flex-col" onClick={e => e.stopPropagation()}>
        <div className="flex items-center justify-between mb-2 gap-3">
          <span className="text-xs text-zinc-300 font-mono truncate">{asset.relative_path}</span>
          <div className="flex items-center gap-2 shrink-0">
            {allowAddToCreatives && isCreativeEligible(asset) && (
              <button onClick={add} disabled={adding || added} className={`${btn} border-cyan-700/50 text-cyan-300 hover:bg-cyan-900/20`}>
                <Sparkles size={12} /> {added ? 'In Creatives' : adding ? 'Adding…' : 'Add to Creatives'}
              </button>
            )}
            {showTextEdit && !editing && (
              <button onClick={startEdit} className={`${btn} border-zinc-700/50 text-zinc-300 hover:border-zinc-600`}><Pencil size={12} /> Edit</button>
            )}
            {editing && (
              <>
                <button onClick={save} disabled={saving} className={`${btn} border-cyan-700/50 text-cyan-300 hover:bg-cyan-900/20`}><Check size={12} /> {saving ? 'Saving…' : 'Save'}</button>
                <button onClick={() => setEditing(false)} className={`${btn} border-zinc-700/50 text-zinc-400`}>Cancel</button>
              </>
            )}
            <a href={url} download={asset.filename} className={`${btn} border-zinc-700/50 text-zinc-400 hover:border-zinc-600`}><Download size={12} /></a>
            <button onClick={onClose} className="p-1 rounded text-zinc-400 hover:text-white cursor-pointer"><X size={18} /></button>
          </div>
        </div>
        <div className="bg-zinc-950/40 rounded-lg flex items-center justify-center overflow-auto">
          {editing ? (
            <textarea
              value={draft}
              onChange={e => setDraft(e.target.value)}
              className="w-[70vw] max-w-[800px] h-[70vh] p-4 rounded-lg border border-zinc-800 bg-zinc-950 text-sm text-zinc-200 font-mono outline-none resize-none"
            />
          ) : (
            <AssetMediaPreview asset={asset} />
          )}
        </div>
      </div>
    </div>
  );
}
