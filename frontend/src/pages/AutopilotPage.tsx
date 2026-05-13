import { Radio, CircleDot } from 'lucide-react';

export default function AutopilotPage() {
  return (
    <div className="p-8 max-w-[1400px] mx-auto">
      <header className="mb-8">
        <div className="flex items-center gap-2.5 mb-1">
          <Radio size={20} className="text-cyan-400" />
          <h2 className="text-2xl font-bold tracking-tight text-white">Autopilot</h2>
        </div>
        <p className="text-xs text-zinc-500">Continuous monitoring and automatic optimization of your campaigns.</p>
      </header>

      {/* Two-Panel Layout */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        {/* Left: Activity Timeline */}
        <div
          className="rounded-xl border border-zinc-800/40 flex flex-col items-center justify-center"
          style={{ background: '#111114', minHeight: 440 }}
        >
          <div className="flex flex-col items-center px-8 text-center">
            <div className="p-3 rounded-full border border-zinc-800/50 mb-4" style={{ background: '#18181b' }}>
              <CircleDot size={20} className="text-zinc-700" />
            </div>
            <p className="text-xs text-zinc-500 leading-relaxed max-w-[260px]">
              Create a campaign and enable Autopilot to get started.
            </p>
          </div>
        </div>

        {/* Right: Status */}
        <div
          className="rounded-xl border border-zinc-800/40 flex flex-col items-center justify-center"
          style={{ background: '#111114', minHeight: 440 }}
        >
          <div className="flex flex-col items-center px-8 text-center">
            <div className="p-3 rounded-full border border-zinc-800/50 mb-4" style={{ background: '#18181b' }}>
              <Radio size={20} className="text-zinc-700" />
            </div>
            <p className="text-sm font-semibold text-zinc-300 mb-2">
              Enable Autopilot to get started.
            </p>
            <p className="text-xs text-zinc-600 leading-relaxed max-w-sm">
              Autopilot will continuously monitor your ad performance
              and automatically trigger adjustments when data meets
              the preset criteria.
            </p>
          </div>
        </div>
      </div>
    </div>
  );
}
