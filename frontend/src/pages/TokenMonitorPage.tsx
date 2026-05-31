import { useState, useEffect, useCallback } from 'react';
import { AreaChart, Area, BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer } from 'recharts';
import { ChevronDown, ChevronUp, ArrowUpRight, ArrowDownRight } from 'lucide-react';
import {
  fetchTokenSummary, fetchTokenTimeseries, fetchTokenBreakdown, fetchTokenDetails,
} from '../api';
import type { TokenFilters } from '../api';
import type { SearchResult, TokenSummary, TokenTimeseriesPoint, TokenBreakdownItem, TokenDetailRow } from '../types';
import SearchSelect from '../components/SearchSelect';

type DateRange = '24h' | '7d' | '30d';

function dateRangeToISO(dr: DateRange): { since: string; until: string } {
  const now = new Date();
  const until = now.toISOString();
  const ms = { '24h': 86400000, '7d': 604800000, '30d': 2592000000 }[dr];
  const since = new Date(now.getTime() - ms).toISOString();
  return { since, until };
}

function formatNum(n: number): string {
  if (n >= 1_000_000) return (n / 1_000_000).toFixed(1) + 'M';
  if (n >= 1_000) return (n / 1_000).toFixed(1) + 'K';
  return String(n);
}

function pctChange(current: number, previous: number): { value: string; positive: boolean } | null {
  if (previous === 0) return null;
  const pct = ((current - previous) / previous) * 100;
  return { value: Math.abs(pct).toFixed(0) + '%', positive: pct >= 0 };
}

// --- KPI Card ---

function KPICard({ label, value, change }: {
  label: string;
  value: string;
  change: { value: string; positive: boolean } | null;
}) {
  return (
    <div className="bg-zinc-800/50 border border-zinc-700/50 rounded-xl p-4">
      <p className="text-xs text-zinc-400 mb-1">{label}</p>
      <p className="text-2xl font-semibold text-zinc-100">{value}</p>
      {change && (
        <div className={`flex items-center gap-0.5 mt-1 text-xs ${change.positive ? 'text-emerald-400' : 'text-red-400'}`}>
          {change.positive ? <ArrowUpRight size={12} /> : <ArrowDownRight size={12} />}
          {change.value}
        </div>
      )}
    </div>
  );
}

// --- Main Page ---

export default function TokenMonitorPage() {
  const [dateRange, setDateRange] = useState<DateRange>('7d');
  const [selEmployees, setSelEmployees] = useState<SearchResult[]>([]);
  const [selProjects, setSelProjects] = useState<SearchResult[]>([]);
  const [selTasks, setSelTasks] = useState<SearchResult[]>([]);

  const [summary, setSummary] = useState<{ current: TokenSummary; previous: TokenSummary | null } | null>(null);
  const [timeseries, setTimeseries] = useState<TokenTimeseriesPoint[]>([]);
  const [byModel, setByModel] = useState<TokenBreakdownItem[]>([]);
  const [byEmployee, setByEmployee] = useState<TokenBreakdownItem[]>([]);
  const [details, setDetails] = useState<TokenDetailRow[]>([]);
  const [detailPage, setDetailPage] = useState(0);
  const [sortCol, setSortCol] = useState('timestamp');
  const [sortDir, setSortDir] = useState<'asc' | 'desc'>('desc');
  const [loading, setLoading] = useState(false);

  const buildFilters = useCallback((): TokenFilters => {
    const { since, until } = dateRangeToISO(dateRange);
    return {
      since, until,
      employee_id: selEmployees.map(e => e.id),
      project_id: selProjects.map(p => p.id),
      task_id: selTasks.map(t => t.id),
    };
  }, [dateRange, selEmployees, selProjects, selTasks]);

  const loadData = useCallback(async () => {
    setLoading(true);
    const filters = buildFilters();
    const interval = dateRange === '24h' ? 'HOUR' : 'DAY';
    try {
      const [s, ts, bm, be, d] = await Promise.all([
        fetchTokenSummary(filters),
        fetchTokenTimeseries(filters, interval),
        fetchTokenBreakdown(filters, 'model_id'),
        fetchTokenBreakdown(filters, 'employee_name'),
        fetchTokenDetails(filters, 50, detailPage * 50, sortCol, sortDir),
      ]);
      setSummary(s);
      setTimeseries(ts);
      setByModel(bm);
      setByEmployee(be);
      setDetails(d);
    } catch { /* ignore */ }
    setLoading(false);
  }, [buildFilters, dateRange, detailPage, sortCol, sortDir]);

  useEffect(() => { loadData(); }, [loadData]);

  const handleSort = (col: string) => {
    if (col === sortCol) {
      setSortDir(d => d === 'asc' ? 'desc' : 'asc');
    } else {
      setSortCol(col);
      setSortDir('desc');
    }
    setDetailPage(0);
  };

  const SortIcon = ({ col }: { col: string }) => {
    if (col !== sortCol) return null;
    return sortDir === 'asc' ? <ChevronUp size={12} /> : <ChevronDown size={12} />;
  };

  const cur = summary?.current;
  const prev = summary?.previous;

  const tsData = timeseries.map(p => ({
    ...p,
    label: dateRange === '24h'
      ? new Date(p.bucket).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
      : new Date(p.bucket).toLocaleDateString([], { month: 'short', day: 'numeric' }),
  }));

  return (
    <div className="p-6 space-y-6 max-w-7xl mx-auto">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold text-zinc-100">Token Monitor</h1>
        {loading && <span className="text-xs text-zinc-500">Loading...</span>}
      </div>

      {/* Filters */}
      <div className="flex flex-wrap items-center gap-2">
        {(['24h', '7d', '30d'] as DateRange[]).map(r => (
          <button key={r} onClick={() => { setDateRange(r); setDetailPage(0); }}
            className={`px-3 py-1.5 rounded-lg text-xs font-medium cursor-pointer transition-colors ${dateRange === r ? 'bg-cyan-600/30 text-cyan-300 border border-cyan-600/50' : 'bg-zinc-800 text-zinc-400 border border-zinc-700 hover:border-zinc-600'}`}>
            {r}
          </button>
        ))}
        <div className="w-px h-6 bg-zinc-700 mx-1" />
        <SearchSelect type="employees" placeholder="Employee" selected={selEmployees} onChange={setSelEmployees} />
        <SearchSelect type="projects" placeholder="Project" selected={selProjects} onChange={setSelProjects} />
        <SearchSelect type="tasks" placeholder="Task" selected={selTasks} onChange={setSelTasks} />
      </div>

      {/* KPI Cards */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        <KPICard label="Total Tokens" value={cur ? formatNum(cur.total_tokens) : '—'} change={cur && prev ? pctChange(cur.total_tokens, prev.total_tokens) : null} />
        <KPICard label="API Calls" value={cur ? formatNum(cur.total_calls) : '—'} change={cur && prev ? pctChange(cur.total_calls, prev.total_calls) : null} />
        <KPICard label="Errors" value={cur ? String(cur.error_count) : '—'} change={cur && prev ? pctChange(cur.error_count, prev.error_count) : null} />
        <KPICard label="Active Agents" value={cur ? String(cur.active_agents) : '—'} change={null} />
      </div>

      {/* Time Series */}
      <div className="bg-zinc-800/50 border border-zinc-700/50 rounded-xl p-4">
        <h2 className="text-sm font-medium text-zinc-300 mb-3">Token Usage Over Time</h2>
        <ResponsiveContainer width="100%" height={260}>
          <AreaChart data={tsData}>
            <CartesianGrid strokeDasharray="3 3" stroke="#3f3f46" />
            <XAxis dataKey="label" tick={{ fontSize: 11, fill: '#a1a1aa' }} />
            <YAxis tick={{ fontSize: 11, fill: '#a1a1aa' }} tickFormatter={formatNum} />
            <Tooltip contentStyle={{ background: '#27272a', border: '1px solid #3f3f46', borderRadius: 8, fontSize: 12 }}
              labelStyle={{ color: '#a1a1aa' }} formatter={(v) => formatNum(Number(v))} />
            <Area type="monotone" dataKey="prompt_tokens" stackId="1" stroke="#06b6d4" fill="#06b6d4" fillOpacity={0.3} name="Prompt" />
            <Area type="monotone" dataKey="completion_tokens" stackId="1" stroke="#8b5cf6" fill="#8b5cf6" fillOpacity={0.3} name="Completion" />
          </AreaChart>
        </ResponsiveContainer>
      </div>

      {/* Breakdown Charts */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        <div className="bg-zinc-800/50 border border-zinc-700/50 rounded-xl p-4">
          <h2 className="text-sm font-medium text-zinc-300 mb-3">By Model</h2>
          <ResponsiveContainer width="100%" height={200}>
            <BarChart data={byModel} layout="vertical">
              <CartesianGrid strokeDasharray="3 3" stroke="#3f3f46" />
              <XAxis type="number" tick={{ fontSize: 11, fill: '#a1a1aa' }} tickFormatter={formatNum} />
              <YAxis type="category" dataKey="dimension" tick={{ fontSize: 11, fill: '#a1a1aa' }} width={120} />
              <Tooltip contentStyle={{ background: '#27272a', border: '1px solid #3f3f46', borderRadius: 8, fontSize: 12 }}
                formatter={(v) => formatNum(Number(v))} />
              <Bar dataKey="total_tokens" fill="#06b6d4" radius={[0, 4, 4, 0]} name="Tokens" />
            </BarChart>
          </ResponsiveContainer>
        </div>
        <div className="bg-zinc-800/50 border border-zinc-700/50 rounded-xl p-4">
          <h2 className="text-sm font-medium text-zinc-300 mb-3">By Employee</h2>
          <ResponsiveContainer width="100%" height={200}>
            <BarChart data={byEmployee} layout="vertical">
              <CartesianGrid strokeDasharray="3 3" stroke="#3f3f46" />
              <XAxis type="number" tick={{ fontSize: 11, fill: '#a1a1aa' }} tickFormatter={formatNum} />
              <YAxis type="category" dataKey="dimension" tick={{ fontSize: 11, fill: '#a1a1aa' }} width={120} />
              <Tooltip contentStyle={{ background: '#27272a', border: '1px solid #3f3f46', borderRadius: 8, fontSize: 12 }}
                formatter={(v) => formatNum(Number(v))} />
              <Bar dataKey="total_tokens" fill="#8b5cf6" radius={[0, 4, 4, 0]} name="Tokens" />
            </BarChart>
          </ResponsiveContainer>
        </div>
      </div>

      {/* Detail Table */}
      <div className="bg-zinc-800/50 border border-zinc-700/50 rounded-xl overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full text-xs">
            <thead>
              <tr className="border-b border-zinc-700 text-zinc-400">
                {[
                  { key: 'timestamp', label: 'Time' },
                  { key: 'model_id', label: 'Model' },
                  { key: 'employee_name', label: 'Employee' },
                  { key: 'source', label: 'Source' },
                  { key: 'prompt_tokens', label: 'Prompt' },
                  { key: 'completion_tokens', label: 'Completion' },
                  { key: 'total_tokens', label: 'Total' },
                  { key: 'latency_ms', label: 'Latency' },
                  { key: 'status', label: 'Status' },
                ].map(col => (
                  <th key={col.key} onClick={() => handleSort(col.key)}
                    className="px-3 py-2.5 text-left font-medium cursor-pointer hover:text-zinc-200 select-none whitespace-nowrap">
                    <span className="inline-flex items-center gap-1">{col.label}<SortIcon col={col.key} /></span>
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {details.map(row => (
                <tr key={row.id} className="border-b border-zinc-800 hover:bg-zinc-800/80 text-zinc-300">
                  <td className="px-3 py-2 whitespace-nowrap" title={row.timestamp}>
                    {new Date(row.timestamp).toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })}
                  </td>
                  <td className="px-3 py-2 whitespace-nowrap">{row.model_id}</td>
                  <td className="px-3 py-2 whitespace-nowrap">{row.employee_name || '—'}</td>
                  <td className="px-3 py-2 whitespace-nowrap">{row.source}</td>
                  <td className="px-3 py-2 text-right tabular-nums">{formatNum(row.prompt_tokens)}</td>
                  <td className="px-3 py-2 text-right tabular-nums">{formatNum(row.completion_tokens)}</td>
                  <td className="px-3 py-2 text-right tabular-nums font-medium">{formatNum(row.total_tokens)}</td>
                  <td className="px-3 py-2 text-right tabular-nums">{row.latency_ms < 1000 ? `${row.latency_ms}ms` : `${(row.latency_ms / 1000).toFixed(1)}s`}</td>
                  <td className="px-3 py-2">
                    <span className={`inline-block w-2 h-2 rounded-full ${row.status === 'success' ? 'bg-emerald-400' : 'bg-red-400'}`} title={row.error_message || row.status} />
                  </td>
                </tr>
              ))}
              {details.length === 0 && (
                <tr><td colSpan={9} className="px-3 py-8 text-center text-zinc-500">No data for the selected filters</td></tr>
              )}
            </tbody>
          </table>
        </div>
        <div className="flex items-center justify-between px-3 py-2 border-t border-zinc-700 text-xs text-zinc-400">
          <span>Page {detailPage + 1}</span>
          <div className="flex gap-2">
            <button onClick={() => setDetailPage(p => Math.max(0, p - 1))} disabled={detailPage === 0}
              className="px-2 py-1 rounded bg-zinc-700 hover:bg-zinc-600 disabled:opacity-40 cursor-pointer disabled:cursor-default">Prev</button>
            <button onClick={() => setDetailPage(p => p + 1)} disabled={details.length < 50}
              className="px-2 py-1 rounded bg-zinc-700 hover:bg-zinc-600 disabled:opacity-40 cursor-pointer disabled:cursor-default">Next</button>
          </div>
        </div>
      </div>
    </div>
  );
}
