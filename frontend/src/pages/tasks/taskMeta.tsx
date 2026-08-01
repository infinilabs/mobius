import { X, ChevronRight, AlertCircle, CheckCircle2, Clock, ArrowRight } from 'lucide-react';
import type { Task } from '../../types';

export const STATUS_COLUMNS: { key: Task['status']; label: string; color: string }[] = [
  { key: 'scheduled',    label: 'Scheduled',     color: 'text-amber-400' },
  { key: 'todo',         label: 'Todo',          color: 'text-zinc-400' },
  { key: 'ready',        label: 'Ready',         color: 'text-cyan-400' },
  { key: 'in_progress',  label: 'In Progress',   color: 'text-blue-400' },
  { key: 'needs_review', label: 'Needs Review',  color: 'text-violet-400' },
  { key: 'done',         label: 'Done',          color: 'text-emerald-400' },
  { key: 'blocked',      label: 'Blocked',       color: 'text-red-400' },
];

export const PRIORITY_STYLES: Record<string, string> = {
  urgent: 'bg-red-900/50 text-red-300 border-red-700/50',
  high:   'bg-orange-900/50 text-orange-300 border-orange-700/50',
  medium: 'bg-zinc-800 text-zinc-400 border-zinc-600',
  low:    'bg-zinc-800/50 text-zinc-500 border-zinc-700/50',
};

export const PRIORITY_ORDER: Record<string, number> = { urgent: 0, high: 1, medium: 2, low: 3 };

export function initials(name: string) {
  return name.split(' ').map(w => w[0]).join('').slice(0, 2).toUpperCase();
}

export function timeAgo(ts: string) {
  const d = Date.now() - new Date(ts).getTime();
  const mins = Math.floor(d / 60000);
  if (mins < 1) return 'just now';
  if (mins < 60) return `${mins}m ago`;
  const hrs = Math.floor(mins / 60);
  if (hrs < 24) return `${hrs}h ago`;
  return `${Math.floor(hrs / 24)}d ago`;
}

export interface TransitionAction {
  status: string;
  label: string;
  icon: React.ReactNode;
  style: string;
}

export function getAvailableTransitions(task: Task): TransitionAction[] {
  const actions: TransitionAction[] = [];

  switch (task.status) {
    case 'ready':
      actions.push({
        status: 'in_progress', label: 'Start Work', icon: <ArrowRight size={12} />,
        style: 'bg-blue-900/30 text-blue-300 border-blue-700/50 hover:bg-blue-900/50',
      });
      actions.push({
        status: 'blocked', label: 'Block', icon: <AlertCircle size={12} />,
        style: 'bg-red-900/20 text-red-400 border-red-800/50 hover:bg-red-900/40',
      });
      break;
    case 'in_progress':
      if (task.result) {
        actions.push({
          status: 'needs_review', label: 'Submit for Review', icon: <ChevronRight size={12} />,
          style: 'bg-violet-900/30 text-violet-300 border-violet-700/50 hover:bg-violet-900/50',
        });
      }
      actions.push({
        status: 'ready', label: 'Pause', icon: <Clock size={12} />,
        style: 'bg-zinc-800/50 text-zinc-400 border-zinc-700/50 hover:bg-zinc-800',
      });
      actions.push({
        status: 'blocked', label: 'Block', icon: <AlertCircle size={12} />,
        style: 'bg-red-900/20 text-red-400 border-red-800/50 hover:bg-red-900/40',
      });
      break;
    case 'needs_review':
      actions.push({
        status: 'done', label: 'Approve', icon: <CheckCircle2 size={12} />,
        style: 'bg-emerald-900/30 text-emerald-300 border-emerald-700/50 hover:bg-emerald-900/50',
      });
      actions.push({
        status: 'ready', label: 'Reject', icon: <X size={12} />,
        style: 'bg-red-900/20 text-red-400 border-red-800/50 hover:bg-red-900/40',
      });
      break;
    case 'blocked':
      actions.push({
        status: 'ready', label: 'Unblock', icon: <ArrowRight size={12} />,
        style: 'bg-cyan-900/30 text-cyan-300 border-cyan-700/50 hover:bg-cyan-900/50',
      });
      break;
    case 'todo':
      actions.push({
        status: 'blocked', label: 'Block', icon: <AlertCircle size={12} />,
        style: 'bg-red-900/20 text-red-400 border-red-800/50 hover:bg-red-900/40',
      });
      break;
  }

  return actions;
}
