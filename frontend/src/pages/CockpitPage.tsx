import { Gauge, TrendingUp, DollarSign, Eye, MousePointerClick, ArrowUpRight } from 'lucide-react';

const METRICS = [
  { label: 'Active Campaigns', value: '--', icon: <TrendingUp size={16} />, color: '#38bdf8' },
  { label: 'Total Spend', value: '--', icon: <DollarSign size={16} />, color: '#4ade80' },
  { label: 'Impressions', value: '--', icon: <Eye size={16} />, color: '#c084fc' },
  { label: 'Clicks', value: '--', icon: <MousePointerClick size={16} />, color: '#fbbf24' },
];

export default function CockpitPage() {
  return (
    <div className="p-8 max-w-[1400px] mx-auto">
      <header className="mb-8">
        <div className="flex items-center gap-2.5 mb-1">
          <Gauge size={20} className="text-cyan-400" />
          <h2 className="text-2xl font-bold tracking-tight text-white">Cockpit</h2>
        </div>
        <p className="text-xs text-zinc-500">Real-time overview of your campaigns and performance metrics.</p>
      </header>

      {/* Metric Cards */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4 mb-8">
        {METRICS.map(m => (
          <div
            key={m.label}
            className="p-5 rounded-xl border border-zinc-800/40 group"
            style={{ background: '#111114' }}
          >
            <div className="flex items-center justify-between mb-4">
              <div className="p-2 rounded-lg border border-zinc-800/50" style={{ background: '#18181b' }}>
                <span style={{ color: m.color }}>{m.icon}</span>
              </div>
              <ArrowUpRight size={14} className="text-zinc-700" />
            </div>
            <p className="text-2xl font-bold text-zinc-300 mb-1 font-mono">{m.value}</p>
            <p className="text-[11px] text-zinc-600 uppercase tracking-wider">{m.label}</p>
          </div>
        ))}
      </div>

      {/* Chart Placeholder */}
      <div
        className="rounded-xl border border-zinc-800/40 p-8 mb-4 flex flex-col items-center justify-center"
        style={{ background: '#111114', minHeight: 320 }}
      >
        <Gauge size={40} className="text-zinc-800 mb-4" />
        <p className="text-sm font-medium text-zinc-400 mb-1">Performance charts will appear here</p>
        <p className="text-xs text-zinc-600 text-center max-w-md">
          Connect your ad accounts and launch campaigns to see real-time performance data, spend trends, and conversion metrics.
        </p>
      </div>
    </div>
  );
}
