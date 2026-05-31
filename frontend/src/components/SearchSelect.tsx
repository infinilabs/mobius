import { useState, useEffect, useRef } from 'react';
import { Search, ChevronDown } from 'lucide-react';
import { searchEntities } from '../api';
import type { SearchResult } from '../types';

// SearchSelect is a searchable multi-select dropdown backed by /api/search.
// Used by the Token Monitor and Task board filters.
export default function SearchSelect({ type, placeholder, selected, onChange }: {
  type: string;
  placeholder: string;
  selected: SearchResult[];
  onChange: (items: SearchResult[]) => void;
}) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState('');
  const [results, setResults] = useState<SearchResult[]>([]);
  const debounceRef = useRef<ReturnType<typeof setTimeout>>(undefined);
  const containerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    clearTimeout(debounceRef.current);
    debounceRef.current = setTimeout(() => {
      searchEntities(type, query, 10).then(setResults).catch(() => {});
    }, query ? 300 : 0);
    return () => clearTimeout(debounceRef.current);
  }, [query, open, type]);

  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, []);

  const toggle = (item: SearchResult) => {
    const exists = selected.find(s => s.id === item.id);
    onChange(exists ? selected.filter(s => s.id !== item.id) : [...selected, item]);
  };

  const allItems = [...selected.filter(s => !results.find(r => r.id === s.id)), ...results];

  return (
    <div ref={containerRef} className="relative">
      <button onClick={() => setOpen(!open)} className="px-3 py-1.5 rounded-lg bg-zinc-800 border border-zinc-700 text-xs text-zinc-300 hover:border-zinc-600 flex items-center gap-1.5 cursor-pointer min-w-[120px]">
        {selected.length > 0 ? `${selected.length} selected` : placeholder}
        <ChevronDown size={12} />
      </button>
      {open && (
        <div className="absolute z-50 mt-1 w-64 bg-zinc-800 border border-zinc-700 rounded-lg shadow-xl">
          <div className="p-2 border-b border-zinc-700">
            <div className="flex items-center gap-1.5 bg-zinc-900 rounded px-2 py-1">
              <Search size={12} className="text-zinc-500" />
              <input value={query} onChange={e => setQuery(e.target.value)} placeholder={`Search ${type}...`}
                className="bg-transparent text-xs text-zinc-200 outline-none w-full" autoFocus />
            </div>
          </div>
          <div className="max-h-48 overflow-y-auto p-1">
            {allItems.map(item => (
              <label key={item.id} className="flex items-center gap-2 px-2 py-1.5 rounded hover:bg-zinc-700 cursor-pointer">
                <input type="checkbox" checked={!!selected.find(s => s.id === item.id)} onChange={() => toggle(item)}
                  className="rounded border-zinc-600 bg-zinc-900 text-cyan-500" />
                <span className="text-xs text-zinc-300 truncate">{item.label}</span>
              </label>
            ))}
            {allItems.length === 0 && <p className="text-xs text-zinc-500 px-2 py-2">No results</p>}
          </div>
        </div>
      )}
    </div>
  );
}
