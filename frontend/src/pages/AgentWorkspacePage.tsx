import { Bot, Cpu, Play, CircleDot } from 'lucide-react';

const AGENTS = [
  { name: 'Creative Strategist', status: 'idle', desc: 'Generates ad copy and creative concepts based on your brand and goals.' },
  { name: 'Audience Analyst', status: 'idle', desc: 'Analyzes target demographics and finds high-value audience segments.' },
  { name: 'Bid Optimizer', status: 'idle', desc: 'Monitors and adjusts bidding strategies for maximum ROAS.' },
  { name: 'Performance Monitor', status: 'idle', desc: 'Tracks campaign KPIs and triggers alerts on anomalies.' },
];

export default function AgentWorkspacePage() {
  return (
    <div className="p-8 max-w-[1400px] mx-auto">
      <header className="mb-8">
        <div className="flex items-center gap-2.5 mb-1">
          <Bot size={20} className="text-cyan-400" />
          <h2 className="text-2xl font-bold tracking-tight text-white">Agent Workspace</h2>
        </div>
        <p className="text-xs text-zinc-500">Configure and monitor your AI agents. Each agent handles a specific aspect of campaign management.</p>
      </header>

      {/* Agent Cards */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4 mb-8">
        {AGENTS.map(agent => (
          <div
            key={agent.name}
            className="p-5 rounded-xl border border-zinc-800/40 transition-colors hover:border-zinc-700/60"
            style={{ background: '#111114' }}
          >
            <div className="flex items-start justify-between mb-3">
              <div className="flex items-center gap-3">
                <div className="p-2 rounded-lg border border-zinc-800/50" style={{ background: '#18181b' }}>
                  <Cpu size={16} className="text-cyan-400" />
                </div>
                <div>
                  <h3 className="text-sm font-semibold text-zinc-200">{agent.name}</h3>
                  <div className="flex items-center gap-1.5 mt-0.5">
                    <CircleDot size={8} className="text-zinc-600" />
                    <span className="text-[10px] text-zinc-600 uppercase tracking-wider font-medium">{agent.status}</span>
                  </div>
                </div>
              </div>
              <button
                className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium border border-zinc-800/50 text-zinc-500 hover:text-cyan-400 hover:border-cyan-500/30 transition-colors cursor-pointer"
                style={{ background: '#09090b' }}
              >
                <Play size={10} />
                Start
              </button>
            </div>
            <p className="text-xs text-zinc-500 leading-relaxed">{agent.desc}</p>
          </div>
        ))}
      </div>

      {/* Workspace Log */}
      <div
        className="rounded-xl border border-zinc-800/40 p-8 flex flex-col items-center justify-center"
        style={{ background: '#111114', minHeight: 200 }}
      >
        <Bot size={36} className="text-zinc-800 mb-4" />
        <p className="text-sm font-medium text-zinc-400 mb-1">Agent activity log</p>
        <p className="text-xs text-zinc-600 text-center max-w-md">
          Start an agent to see its activity, decisions, and actions in real time.
        </p>
      </div>
    </div>
  );
}
