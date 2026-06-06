// Renders chat message content, turning ```mobius-viz``` fenced JSON blocks into a
// Top-N table + a recharts chart (video_tagging.md §11). Plain text renders as
// before; everything outside the block is unchanged.
import {
  BarChart, Bar, PieChart, Pie, Cell, LineChart, Line,
  XAxis, YAxis, CartesianGrid, Tooltip, Legend, ResponsiveContainer,
} from 'recharts';

type VizType = 'table' | 'bar' | 'pie' | 'line';

interface VizSpec {
  type?: VizType;
  title?: string;
  x?: string;
  y?: string;
  rows?: Array<Record<string, unknown>>;
}

type Segment =
  | { kind: 'text'; text: string }
  | { kind: 'viz'; spec: VizSpec };

const VIZ_RE = /```mobius-viz\s*([\s\S]*?)```/g;
const COLORS = ['#22d3ee', '#0e7490', '#a78bfa', '#f472b6', '#34d399', '#fbbf24', '#fb7185', '#60a5fa', '#f87171', '#4ade80'];

function safeParse(s: string): VizSpec | null {
  try {
    const o: unknown = JSON.parse(s.trim());
    if (o && typeof o === 'object') return o as VizSpec;
  } catch {
    // unparseable (or still streaming) → caller falls back to raw text
  }
  return null;
}

function parseMessage(content: string): Segment[] {
  const segments: Segment[] = [];
  let last = 0;
  VIZ_RE.lastIndex = 0;
  let m: RegExpExecArray | null;
  while ((m = VIZ_RE.exec(content)) !== null) {
    if (m.index > last) segments.push({ kind: 'text', text: content.slice(last, m.index) });
    const spec = safeParse(m[1]);
    if (spec) segments.push({ kind: 'viz', spec });
    else segments.push({ kind: 'text', text: m[0] });
    last = m.index + m[0].length;
  }
  if (last < content.length) segments.push({ kind: 'text', text: content.slice(last) });
  return segments;
}

function fmtCell(v: unknown): string {
  if (Array.isArray(v)) return v.join(', ');
  if (v === null || v === undefined) return '';
  return String(v);
}

export function MessageContent({ content }: { content: string }) {
  const segments = parseMessage(content);
  // Fast path: no viz blocks → identical to the previous plain-text render.
  if (segments.length === 1 && segments[0].kind === 'text') {
    return <div className="whitespace-pre-wrap">{content}</div>;
  }
  return (
    <div className="flex flex-col gap-3">
      {segments.map((seg, i) => {
        if (seg.kind === 'text') {
          const t = seg.text.trim();
          return t ? <div key={i} className="whitespace-pre-wrap">{t}</div> : null;
        }
        return <MobiusViz key={i} spec={seg.spec} />;
      })}
    </div>
  );
}

function MobiusViz({ spec }: { spec: VizSpec }) {
  const rows = Array.isArray(spec.rows) ? spec.rows : [];
  const type: VizType = spec.type ?? 'bar';
  const cols = rows.length ? Object.keys(rows[0]) : [];
  const xKey = spec.x ?? cols[0] ?? 'x';
  const yKey = spec.y ?? cols.find((c) => c !== xKey) ?? cols[1] ?? 'y';

  const chartData = rows.map((r) => ({ ...r, [yKey]: Number(r[yKey]) }));

  return (
    <div className="rounded-xl border border-zinc-800/60 bg-zinc-900/40 p-3">
      {spec.title && <div className="text-xs font-medium text-zinc-300 mb-2">{spec.title}</div>}

      {rows.length > 0 && (
        <div className="overflow-x-auto mb-3">
          <table className="w-full text-xs">
            <thead>
              <tr>
                {cols.map((c) => (
                  <th key={c} className="text-left px-2 py-1 text-zinc-500 font-medium border-b border-zinc-800">{c}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.map((r, i) => (
                <tr key={i} className="border-b border-zinc-900">
                  {cols.map((c) => (
                    <td key={c} className="px-2 py-1 text-zinc-300">{fmtCell(r[c])}</td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {type !== 'table' && chartData.length > 0 && (
        <ResponsiveContainer width="100%" height={260}>
          {type === 'pie' ? (
            <PieChart>
              <Pie data={chartData} dataKey={yKey} nameKey={xKey} cx="50%" cy="50%" outerRadius={90} label>
                {chartData.map((_, i) => (
                  <Cell key={i} fill={COLORS[i % COLORS.length]} />
                ))}
              </Pie>
              <Tooltip />
              <Legend />
            </PieChart>
          ) : type === 'line' ? (
            <LineChart data={chartData}>
              <CartesianGrid strokeDasharray="3 3" stroke="#27272a" />
              <XAxis dataKey={xKey} stroke="#71717a" fontSize={11} />
              <YAxis stroke="#71717a" fontSize={11} />
              <Tooltip />
              <Line type="monotone" dataKey={yKey} stroke="#22d3ee" strokeWidth={2} dot={false} />
            </LineChart>
          ) : (
            <BarChart data={chartData}>
              <CartesianGrid strokeDasharray="3 3" stroke="#27272a" />
              <XAxis dataKey={xKey} stroke="#71717a" fontSize={11} />
              <YAxis stroke="#71717a" fontSize={11} />
              <Tooltip />
              <Bar dataKey={yKey} fill="#22d3ee" radius={[4, 4, 0, 0]} />
            </BarChart>
          )}
        </ResponsiveContainer>
      )}
    </div>
  );
}
