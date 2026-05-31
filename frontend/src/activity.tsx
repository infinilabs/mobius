import type { ReactNode } from 'react';
import {
  Activity, FileText, Terminal, Database, MessageSquare, UserPlus,
  Send, CheckCircle2, XCircle, Share2,
} from 'lucide-react';
import type { Event } from './types';

const ICONS: Record<string, ReactNode> = {
  task_delegated:       <Share2 size={13} />,
  employee_hired:       <UserPlus size={13} />,
  task_submitted:       <Send size={13} />,
  task_approved:        <CheckCircle2 size={13} />,
  task_rejected:        <XCircle size={13} />,
  file_written:         <FileText size={13} />,
  command_execution:    <Terminal size={13} />,
  memory_stored:        <Database size={13} />,
  conversation_started: <MessageSquare size={13} />,
};

export function eventIcon(type: string): ReactNode {
  return ICONS[type] ?? <Activity size={13} />;
}

// Map an event to a human-readable verb plus an optional detail line. Keys come
// from the payloads published in backend/adapter_internal_tools.go / chat.go.
export function activityVerb(ev: Event): { verb: string; detail?: string } {
  const p = ev.payload || {};
  const s = (k: string) => (typeof p[k] === 'string' ? (p[k] as string) : undefined);
  switch (ev.event_type) {
    case 'task_delegated':       return { verb: 'delegated a task', detail: s('title') };
    case 'employee_hired':       return { verb: 'hired', detail: s('name') };
    case 'task_submitted':       return { verb: 'submitted work for review', detail: s('result_preview') };
    case 'task_approved':        return { verb: 'approved a task' };
    case 'task_rejected':        return { verb: 'requested changes', detail: s('feedback') };
    case 'file_written':         return { verb: 'wrote a file', detail: s('path') };
    case 'command_execution':    return { verb: 'ran a command', detail: s('command') };
    case 'memory_stored':        return { verb: 'stored a memory', detail: s('memory_text') };
    case 'conversation_started': return { verb: 'started a conversation' };
    default:                     return { verb: ev.event_type.replace(/[._]/g, ' ') };
  }
}

export function activityTimeAgo(ts: string): string {
  const d = Date.now() - new Date(ts).getTime();
  const mins = Math.floor(d / 60000);
  if (mins < 1) return 'just now';
  if (mins < 60) return `${mins}m ago`;
  const hrs = Math.floor(mins / 60);
  if (hrs < 24) return `${hrs}h ago`;
  return `${Math.floor(hrs / 24)}d ago`;
}
