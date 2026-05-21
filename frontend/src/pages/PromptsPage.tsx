import { useState, useEffect, useCallback, useRef } from 'react';
import { Search, Plus, Trash2, X, Tag, Copy, Check } from 'lucide-react';
import { listPrompts, createPrompt, deletePrompt } from '../api';
import type { Prompt } from '../types';

export default function PromptsPage() {
  const [prompts, setPrompts] = useState<Prompt[]>([]);
  const [search, setSearch] = useState('');
  const [loading, setLoading] = useState(true);
  const [showCreate, setShowCreate] = useState(false);
  const [expanded, setExpanded] = useState<string | null>(null);
  const [copiedId, setCopiedId] = useState<string | null>(null);

  const refresh = useCallback((query?: string) => {
    setLoading(true);
    listPrompts(query)
      .then(setPrompts)
      .catch(() => setPrompts([]))
      .finally(() => setLoading(false));
  }, []);

  const debounceRef = useRef<ReturnType<typeof setTimeout>>(null);

  useEffect(() => { refresh(); }, [refresh]);

  useEffect(() => {
    if (debounceRef.current) clearTimeout(debounceRef.current);
    debounceRef.current = setTimeout(() => {
      refresh(search || undefined);
    }, 250);
    return () => { if (debounceRef.current) clearTimeout(debounceRef.current); };
  }, [search, refresh]);

  const handleDelete = async (id: string) => {
    await deletePrompt(id);
    setPrompts(prev => prev.filter(p => p.id !== id));
    if (expanded === id) setExpanded(null);
  };

  const handleCopy = (id: string, content: string) => {
    navigator.clipboard.writeText(content);
    setCopiedId(id);
    setTimeout(() => setCopiedId(null), 1500);
  };

  const handleCreated = (p: Prompt) => {
    setPrompts(prev => [p, ...prev]);
    setShowCreate(false);
  };

  return (
    <div className="p-8 max-w-[1400px] mx-auto">
      <header className="mb-6">
        <h2 className="text-2xl font-bold tracking-tight text-white">Prompts</h2>
        <p className="text-xs text-zinc-600 mt-1">Manage reusable prompt templates</p>
      </header>

      {/* Toolbar */}
      <div className="flex items-center gap-3 mb-6 flex-wrap">
        <div className="relative flex-1 min-w-[240px] max-w-[520px]">
          <Search size={14} className="absolute left-3 top-1/2 -translate-y-1/2 text-zinc-600" />
          <input
            type="text"
            value={search}
            onChange={e => setSearch(e.target.value)}
            placeholder="Search prompts by title, content, or tag..."
            className="w-full text-xs text-zinc-300 rounded-lg pl-9 pr-8 py-2.5 outline-none border border-zinc-800/50 transition-colors focus:border-cyan-500/30 placeholder:text-zinc-700"
            style={{ background: '#111114' }}
          />
          {search && (
            <button
              onClick={() => setSearch('')}
              className="absolute right-2.5 top-1/2 -translate-y-1/2 text-zinc-600 hover:text-zinc-300 cursor-pointer transition-colors"
            >
              <X size={12} />
            </button>
          )}
        </div>

        <div className="flex-1" />

        <button
          onClick={() => setShowCreate(true)}
          className="flex items-center gap-2 px-4 py-2 rounded-lg text-xs font-medium text-white transition-colors cursor-pointer"
          style={{ background: 'linear-gradient(135deg, #0e7490, #164e63)', border: '1px solid #0e749050' }}
        >
          <Plus size={14} />
          New Prompt
        </button>
      </div>

      {/* Create Modal */}
      {showCreate && (
        <CreatePromptModal
          onClose={() => setShowCreate(false)}
          onCreated={handleCreated}
        />
      )}

      {/* Content */}
      {loading ? (
        <div className="flex items-center justify-center" style={{ minHeight: 300 }}>
          <div className="h-8 w-8 rounded-full border-2 border-transparent animate-spin" style={{ borderTopColor: '#38bdf8' }} />
        </div>
      ) : prompts.length === 0 ? (
        <div
          className="rounded-xl border border-zinc-800/40 flex flex-col items-center justify-center"
          style={{ background: '#111114', minHeight: 400 }}
        >
          <Tag size={40} className="text-zinc-800 mb-4" />
          <p className="text-sm font-semibold text-zinc-300 mb-2">No prompts yet</p>
          <p className="text-xs text-zinc-600 text-center max-w-sm leading-relaxed">
            Create reusable prompt templates to streamline your workflows.
            Add tags to organize and quickly find them later.
          </p>
        </div>
      ) : (
        <div className="flex flex-col gap-2">
          {prompts.map(p => (
            <div
              key={p.id}
              className="rounded-xl border border-zinc-800/40 transition-colors hover:border-zinc-700/60"
              style={{ background: '#111114' }}
            >
              <div
                className="flex items-center gap-3 px-5 py-3.5 cursor-pointer"
                onClick={() => setExpanded(expanded === p.id ? null : p.id)}
              >
                <div className="flex-1 min-w-0">
                  <p className="text-sm font-medium text-zinc-200 truncate">{p.title}</p>
                  <p className="text-xs text-zinc-600 mt-0.5 truncate">{p.content.slice(0, 120)}</p>
                </div>

                {p.tags.length > 0 && (
                  <div className="flex items-center gap-1.5 shrink-0">
                    {p.tags.slice(0, 3).map(tag => (
                      <button
                        key={tag}
                        onClick={e => { e.stopPropagation(); setSearch(tag); refresh(tag); }}
                        className="px-2 py-0.5 rounded text-[10px] font-medium text-cyan-400 hover:text-cyan-300 cursor-pointer transition-colors"
                        style={{ background: '#0e749015', border: '1px solid #0e749030' }}
                      >
                        {tag}
                      </button>
                    ))}
                    {p.tags.length > 3 && (
                      <span className="text-[10px] text-zinc-600">+{p.tags.length - 3}</span>
                    )}
                  </div>
                )}

                <span className="text-[10px] text-zinc-700 shrink-0">
                  {new Date(p.updated_at).toLocaleDateString()}
                </span>

                <button
                  onClick={e => { e.stopPropagation(); handleCopy(p.id, p.content); }}
                  className="p-1.5 rounded-lg text-zinc-600 hover:text-zinc-300 hover:bg-zinc-800/50 cursor-pointer transition-colors shrink-0"
                  title="Copy content"
                >
                  {copiedId === p.id ? <Check size={14} className="text-emerald-400" /> : <Copy size={14} />}
                </button>

                <button
                  onClick={e => { e.stopPropagation(); handleDelete(p.id); }}
                  className="p-1.5 rounded-lg text-zinc-600 hover:text-red-400 hover:bg-red-500/10 cursor-pointer transition-colors shrink-0"
                  title="Delete"
                >
                  <Trash2 size={14} />
                </button>
              </div>

              {expanded === p.id && (
                <div className="px-5 pb-4 border-t border-zinc-800/40">
                  <pre className="text-xs text-zinc-400 mt-3 whitespace-pre-wrap leading-relaxed font-mono">{p.content}</pre>
                  {p.tags.length > 0 && (
                    <div className="flex items-center gap-1.5 mt-3">
                      <Tag size={11} className="text-zinc-600" />
                      {p.tags.map(tag => (
                        <button
                          key={tag}
                          onClick={() => { setSearch(tag); refresh(tag); }}
                          className="px-2 py-0.5 rounded text-[10px] font-medium text-cyan-400 hover:text-cyan-300 cursor-pointer transition-colors"
                          style={{ background: '#0e749015', border: '1px solid #0e749030' }}
                        >
                          {tag}
                        </button>
                      ))}
                    </div>
                  )}
                  <p className="text-[10px] text-zinc-700 mt-2">
                    Created {new Date(p.created_at).toLocaleString()} · Updated {new Date(p.updated_at).toLocaleString()}
                  </p>
                </div>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

function CreatePromptModal({ onClose, onCreated }: {
  onClose: () => void;
  onCreated: (p: Prompt) => void;
}) {
  const [title, setTitle] = useState('');
  const [content, setContent] = useState('');
  const [tagInput, setTagInput] = useState('');
  const [tags, setTags] = useState<string[]>([]);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  const addTags = () => {
    const newTags = tagInput
      .split(',')
      .map(t => t.trim().toLowerCase())
      .filter(t => t && !tags.includes(t));
    if (newTags.length) setTags([...tags, ...newTags]);
    setTagInput('');
  };

  const handleSubmit = async () => {
    if (!title.trim() || !content.trim()) {
      setError('Title and content are required');
      return;
    }
    setSaving(true);
    setError('');
    try {
      const p = await createPrompt({ title: title.trim(), content: content.trim(), tags });
      onCreated(p);
    } catch {
      setError('Failed to save prompt');
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center" style={{ background: 'rgba(0,0,0,0.6)' }}>
      <div
        className="rounded-2xl border border-zinc-800/60 shadow-2xl w-full max-w-lg mx-4"
        style={{ background: '#0a0a0d' }}
      >
        <div className="flex items-center justify-between px-6 py-4 border-b border-zinc-800/40">
          <h3 className="text-sm font-semibold text-white">New Prompt</h3>
          <button onClick={onClose} className="text-zinc-600 hover:text-zinc-300 cursor-pointer transition-colors">
            <X size={16} />
          </button>
        </div>

        <div className="px-6 py-4 flex flex-col gap-4">
          <div>
            <label className="text-[10px] font-semibold text-zinc-600 uppercase tracking-wider mb-1.5 block">Title</label>
            <input
              value={title}
              onChange={e => setTitle(e.target.value)}
              placeholder="e.g. Ad Copy Generator"
              className="w-full text-xs text-zinc-300 rounded-lg px-3 py-2.5 outline-none border border-zinc-800/50 focus:border-cyan-500/30 placeholder:text-zinc-700"
              style={{ background: '#111114' }}
            />
          </div>

          <div>
            <label className="text-[10px] font-semibold text-zinc-600 uppercase tracking-wider mb-1.5 block">Content</label>
            <textarea
              value={content}
              onChange={e => setContent(e.target.value)}
              placeholder="Enter your prompt template..."
              rows={6}
              className="w-full text-xs text-zinc-300 rounded-lg px-3 py-2.5 outline-none border border-zinc-800/50 focus:border-cyan-500/30 placeholder:text-zinc-700 resize-none font-mono leading-relaxed"
              style={{ background: '#111114' }}
            />
          </div>

          <div>
            <label className="text-[10px] font-semibold text-zinc-600 uppercase tracking-wider mb-1.5 block">Tags</label>
            <div className="flex items-center gap-2">
              <input
                value={tagInput}
                onChange={e => setTagInput(e.target.value)}
                onKeyDown={e => { if (e.key === 'Enter') { e.preventDefault(); addTags(); } }}
                onBlur={addTags}
                placeholder="Type tags separated by commas, press Enter"
                className="flex-1 text-xs text-zinc-300 rounded-lg px-3 py-2.5 outline-none border border-zinc-800/50 focus:border-cyan-500/30 placeholder:text-zinc-700"
                style={{ background: '#111114' }}
              />
            </div>
            {tags.length > 0 && (
              <div className="flex items-center gap-1.5 mt-2 flex-wrap">
                {tags.map(tag => (
                  <span
                    key={tag}
                    className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-[10px] font-medium text-cyan-400"
                    style={{ background: '#0e749015', border: '1px solid #0e749030' }}
                  >
                    {tag}
                    <button
                      onClick={() => setTags(tags.filter(t => t !== tag))}
                      className="text-cyan-600 hover:text-cyan-300 cursor-pointer"
                    >
                      <X size={10} />
                    </button>
                  </span>
                ))}
              </div>
            )}
          </div>

          {error && <p className="text-xs text-red-400">{error}</p>}
        </div>

        <div className="flex items-center justify-end gap-3 px-6 py-4 border-t border-zinc-800/40">
          <button
            onClick={onClose}
            className="px-4 py-2 rounded-lg text-xs font-medium text-zinc-400 hover:text-zinc-200 border border-zinc-800/50 hover:border-zinc-700/60 transition-colors cursor-pointer"
            style={{ background: '#111114' }}
          >
            Cancel
          </button>
          <button
            onClick={handleSubmit}
            disabled={saving}
            className="px-4 py-2 rounded-lg text-xs font-medium text-white transition-colors cursor-pointer disabled:opacity-50"
            style={{ background: 'linear-gradient(135deg, #0e7490, #164e63)', border: '1px solid #0e749050' }}
          >
            {saving ? 'Saving...' : 'Create Prompt'}
          </button>
        </div>
      </div>
    </div>
  );
}
