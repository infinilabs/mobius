import { useState, useEffect, useRef } from 'react';
import { Bot, User, Copy, Pencil, Check, ThumbsUp, ThumbsDown, RefreshCw, Printer } from 'lucide-react';
import type { ChatMessage } from '../../types';
import { MessageContent } from '../../components/MobiusViz';

const TOOL_LABELS: Record<string, string> = {
  delegate_task: 'Created a task for the team',
  suggest_tasks: 'Proposed tasks',
  create_project: 'Created a project',
  update_project: 'Updated the project',
  hire_employee: 'Hired a specialist',
  generate_image: 'Generated an image',
  generate_audio: 'Generated audio',
  playable_write_html: 'Wrote the playable HTML',
  publish_playable_ad: 'Published the playable ad',
  save_upload_to_assets: 'Saved upload to assets',
  write_project_file: 'Wrote a project file',
};

function toolLabel(name: string): string {
  return TOOL_LABELS[name] || name.replace(/_/g, ' ');
}

export function ToolActivity({ events }: { events: { name: string; status: string }[] }) {
  return (
    <div className="flex flex-col gap-1.5">
      {events.map((e, i) => (
        <div key={i} className="flex items-center gap-2 text-xs text-zinc-400 pl-1">
          <span className="shrink-0 p-1 rounded-md border border-cyan-500/20" style={{ background: '#0e749020' }}>
            <Check size={11} className="text-cyan-400" />
          </span>
          <span>{toolLabel(e.name)}</span>
        </div>
      ))}
    </div>
  );
}

export function ThinkingIndicator() {
  return (
    <div className="flex gap-3 justify-start">
      <div className="shrink-0 p-2 rounded-lg border border-cyan-500/20 h-fit" style={{ background: '#0e749020' }}>
        <Bot size={14} className="text-cyan-400 animate-pulse" />
      </div>
      <div
        className="rounded-2xl rounded-bl-md px-4 py-3 border border-zinc-800/40 flex items-center gap-2"
        style={{ background: '#111114' }}
      >
        <div className="flex items-center gap-1">
          <span className="block w-2 h-2 rounded-full bg-cyan-400 animate-bounce" style={{ animationDelay: '0ms' }} />
          <span className="block w-2 h-2 rounded-full bg-cyan-400 animate-bounce" style={{ animationDelay: '150ms' }} />
          <span className="block w-2 h-2 rounded-full bg-cyan-400 animate-bounce" style={{ animationDelay: '300ms' }} />
        </div>
        <span className="text-xs text-zinc-500 ml-1">Thinking...</span>
      </div>
    </div>
  );
}

export function MessageBubble({ message, isStreaming, onEdit, onRegenerate }: {
  message: ChatMessage; isStreaming: boolean;
  onEdit?: (newContent: string) => void;
  onRegenerate?: () => void;
}) {
  const isUser = message.role === 'user';
  const [editing, setEditing] = useState(false);
  const [editValue, setEditValue] = useState(message.content);
  const [copied, setCopied] = useState(false);
  const [voted, setVoted] = useState<'up' | 'down' | null>(null);
  const editRef = useRef<HTMLTextAreaElement>(null);

  useEffect(() => {
    if (editing && editRef.current) {
      editRef.current.focus();
      editRef.current.style.height = 'auto';
      editRef.current.style.height = editRef.current.scrollHeight + 'px';
    }
  }, [editing]);

  const handleCopy = () => {
    navigator.clipboard.writeText(message.content);
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  };

  const handlePrint = () => {
    const printWindow = window.open('', '_blank');
    if (!printWindow) return;
    const doc = printWindow.document;
    const style = doc.createElement('style');
    style.textContent = 'body{font-family:system-ui,sans-serif;padding:2rem;max-width:700px;line-height:1.6;color:#222}pre{white-space:pre-wrap}';
    doc.head.appendChild(style);
    doc.title = 'Mobius Chat';
    const pre = doc.createElement('pre');
    pre.textContent = message.content;
    doc.body.appendChild(pre);
    printWindow.print();
  };

  const handleEditConfirm = () => {
    const trimmed = editValue.trim();
    if (trimmed && trimmed !== message.content && onEdit) {
      onEdit(trimmed);
    }
    setEditing(false);
  };

  const ts = new Date(message.timestamp).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });

  const actionBtnClass = "p-1 rounded text-zinc-600 hover:text-zinc-300 hover:bg-zinc-800/50 cursor-pointer transition-colors";

  return (
    <div className={`group flex gap-3 ${isUser ? 'justify-end' : 'justify-start'}`}>
      {!isUser && (
        <div className="shrink-0 p-2 rounded-lg border border-cyan-500/20 h-fit" style={{ background: '#0e749020' }}>
          <Bot size={14} className="text-cyan-400" />
        </div>
      )}

      <div className={`flex flex-col max-w-[80%] ${isUser ? 'items-end' : 'items-start'}`}>
        {/* Bubble */}
        <div
          className={`w-full rounded-2xl px-4 py-3 text-sm leading-relaxed ${
            isUser
              ? 'text-cyan-100 rounded-br-md'
              : 'text-zinc-300 rounded-bl-md border border-zinc-800/40'
          }`}
          style={{
            background: isUser
              ? 'linear-gradient(135deg, #0e7490, #164e63)'
              : '#111114',
          }}
        >
          {message.files && message.files.length > 0 && (
            <div className="flex flex-wrap gap-1.5 mb-2">
              {message.files.map(f => (
                <span key={f.id} className="text-[10px] px-2 py-0.5 rounded-md border border-zinc-700/50 text-zinc-400 bg-zinc-800/50">
                  {f.name}
                </span>
              ))}
            </div>
          )}

          {editing ? (
            <div className="flex flex-col gap-2">
              <textarea
                ref={editRef}
                value={editValue}
                onChange={e => {
                  setEditValue(e.target.value);
                  e.target.style.height = 'auto';
                  e.target.style.height = e.target.scrollHeight + 'px';
                }}
                onKeyDown={e => {
                  if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); handleEditConfirm(); }
                  if (e.key === 'Escape') { setEditing(false); setEditValue(message.content); }
                }}
                className="w-full bg-transparent text-sm text-cyan-100 outline-none resize-none"
                rows={1}
              />
              <div className="flex items-center gap-2 justify-end">
                <button
                  onClick={() => { setEditing(false); setEditValue(message.content); }}
                  className="text-[10px] text-zinc-400 hover:text-zinc-200 cursor-pointer px-2 py-0.5"
                >
                  Cancel
                </button>
                <button
                  onClick={handleEditConfirm}
                  className="flex items-center gap-1 text-[10px] text-cyan-300 hover:text-cyan-100 cursor-pointer px-2 py-0.5 rounded border border-cyan-500/30"
                  style={{ background: '#0e749030' }}
                >
                  <Check size={10} />
                  Save & Regenerate
                </button>
              </div>
            </div>
          ) : (
            <MessageContent content={message.content} />
          )}

          {isStreaming && message.content && !editing && (
            <span className="inline-block w-1.5 h-4 bg-cyan-400/60 animate-pulse ml-0.5 -mb-0.5 rounded-sm" />
          )}
        </div>

        {/* User toolbar — appears on hover */}
        {isUser && !editing && (
          <div className="flex items-center gap-1 mt-1 opacity-0 group-hover:opacity-100 transition-opacity">
            <span className="text-[10px] text-zinc-600 font-mono mr-1">{ts}</span>
            <button onClick={handleCopy} className={actionBtnClass} title="Copy">
              {copied ? <Check size={12} className="text-emerald-400" /> : <Copy size={12} />}
            </button>
            {onEdit && (
              <button onClick={() => { setEditValue(message.content); setEditing(true); }} className={actionBtnClass} title="Edit & regenerate">
                <Pencil size={12} />
              </button>
            )}
          </div>
        )}

        {/* Bot toolbar — always visible after streaming completes */}
        {!isUser && !isStreaming && message.content && (
          <div className="flex items-center gap-1 mt-1.5">
            <button onClick={handleCopy} className={actionBtnClass} title="Copy">
              {copied ? <Check size={12} className="text-emerald-400" /> : <Copy size={12} />}
            </button>
            <button
              onClick={() => setVoted(voted === 'up' ? null : 'up')}
              className={`${actionBtnClass} ${voted === 'up' ? '!text-emerald-400' : ''}`}
              title="Good response"
            >
              <ThumbsUp size={12} />
            </button>
            <button
              onClick={() => setVoted(voted === 'down' ? null : 'down')}
              className={`${actionBtnClass} ${voted === 'down' ? '!text-red-400' : ''}`}
              title="Bad response"
            >
              <ThumbsDown size={12} />
            </button>
            {onRegenerate && (
              <button onClick={onRegenerate} className={actionBtnClass} title="Regenerate">
                <RefreshCw size={12} />
              </button>
            )}
            <button onClick={handlePrint} className={actionBtnClass} title="Print">
              <Printer size={12} />
            </button>
          </div>
        )}
      </div>

      {isUser && !editing && (
        <div className="shrink-0 p-2 rounded-lg border border-zinc-800/50 h-fit" style={{ background: '#18181b' }}>
          <User size={14} className="text-zinc-400" />
        </div>
      )}
    </div>
  );
}
