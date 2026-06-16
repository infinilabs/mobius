import { RefreshCw } from 'lucide-react';

interface RefreshButtonProps {
  onClick: () => void;
  loading?: boolean;
  title?: string;
}

export default function RefreshButton({ onClick, loading = false, title = 'Refresh' }: RefreshButtonProps) {
  return (
    <button
      onClick={onClick}
      disabled={loading}
      title={title}
      className="flex items-center justify-center p-2 rounded-lg border border-zinc-800/60 text-zinc-400 hover:text-cyan-300 hover:border-cyan-500/30 cursor-pointer transition-colors disabled:opacity-50"
      style={{ background: '#111114' }}
    >
      <RefreshCw size={15} className={loading ? 'animate-spin' : ''} />
    </button>
  );
}
