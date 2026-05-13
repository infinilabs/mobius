import { useState } from 'react';
import { Search, Upload, SlidersHorizontal, ImageOff } from 'lucide-react';

const FILTERS = ['All', 'AI Generated'];

export default function CreativesPage() {
  const [search, setSearch] = useState('');
  const [activeFilter, setActiveFilter] = useState('All');

  return (
    <div className="p-8 max-w-[1400px] mx-auto">
      <header className="mb-6">
        <h2 className="text-2xl font-bold tracking-tight text-white">Creatives</h2>
      </header>

      {/* Toolbar */}
      <div className="flex items-center gap-3 mb-5 flex-wrap">
        {/* Search */}
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

        <div className="flex-1" />

        {/* Actions */}
        <button
          className="flex items-center gap-2 px-4 py-2 rounded-lg text-xs font-medium border border-zinc-800/50 text-zinc-400 hover:text-zinc-200 hover:border-zinc-700/60 transition-colors cursor-pointer"
          style={{ background: '#111114' }}
        >
          <Upload size={14} />
          Upload
        </button>
        <button
          className="flex items-center gap-2 px-4 py-2 rounded-lg text-xs font-medium border border-zinc-800/50 text-zinc-400 hover:text-zinc-200 hover:border-zinc-700/60 transition-colors cursor-pointer"
          style={{ background: '#111114' }}
        >
          <SlidersHorizontal size={14} />
          Filter
        </button>
      </div>

      {/* Filter Tabs */}
      <div className="flex items-center gap-2 mb-8">
        {FILTERS.map(f => (
          <button
            key={f}
            onClick={() => setActiveFilter(f)}
            className={`px-3 py-1 rounded-lg text-xs font-medium transition-colors cursor-pointer ${
              activeFilter === f
                ? 'text-cyan-400 border-cyan-500/30'
                : 'text-zinc-500 border-zinc-800/50 hover:text-zinc-300 hover:border-zinc-700/60'
            }`}
            style={{
              background: activeFilter === f ? '#0e749015' : '#111114',
              border: `1px solid ${activeFilter === f ? '#0e749050' : '#27272a40'}`,
            }}
          >
            {f}
          </button>
        ))}
      </div>

      {/* Empty State */}
      <div
        className="rounded-xl border border-zinc-800/40 flex flex-col items-center justify-center"
        style={{ background: '#111114', minHeight: 400 }}
      >
        <ImageOff size={40} className="text-zinc-800 mb-4" />
        <p className="text-sm font-semibold text-zinc-300 mb-2">Your creatives will appear here.</p>
        <p className="text-xs text-zinc-600 text-center max-w-sm leading-relaxed">
          Generate ad images with AI Creatives in any conversation,
          or upload your own. All creatives are saved here for easy
          reuse across campaigns.
        </p>
      </div>
    </div>
  );
}
