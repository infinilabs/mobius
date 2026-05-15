import { useState, useEffect, useRef, useCallback } from 'react';
import {
  Send, Sparkles, BarChart3, ImagePlus, Activity,
  Rocket, Target, Zap, Paperclip, X, Bot, User, Loader2,
  Copy, Pencil, Check, ThumbsUp, ThumbsDown, RefreshCw, Printer,
} from 'lucide-react';
import { createConversation, getConversation, sendChatMessage, uploadFile, truncateConversation } from '../api';
import type { ChatMessage, FileRef } from '../types';
import { log } from '../logger';

const QUICK_ACTIONS = [
  { icon: <BarChart3 size={18} />, title: 'Analyze Competitor Ads', desc: 'See what\'s winning in your niche', tag: 'Name or URL', color: '#38bdf8', prompt: 'Analyze my competitors\' ad strategies and tell me what\'s working in my niche.' },
  { icon: <ImagePlus size={18} />, title: 'Generate Ad Creatives', desc: 'Ad copy + visuals, ready to test', tag: 'URL + images', color: '#c084fc', prompt: 'Generate ad creative concepts with copy and visual direction for my product.' },
  { icon: <Activity size={18} />, title: 'Diagnose Ad Performance', desc: 'Find what\'s draining your budget', tag: 'Connect Ad account', color: '#fb7185', prompt: 'Help me diagnose my ad performance and find what\'s draining my budget.' },
  { icon: <Rocket size={18} />, title: 'Launch a Campaign', desc: 'From brief to live in 5 minutes', tag: 'Ready in 5 min', color: '#4ade80', prompt: 'Help me plan and launch a new ad campaign from scratch.' },
  { icon: <Target size={18} />, title: 'Plan My Growth Strategy', desc: 'Budget, audience & channel plan', tag: 'Describe your product', color: '#fbbf24', prompt: 'Help me create a growth strategy with budget allocation, audience targeting, and channel planning.' },
  { icon: <Zap size={18} />, title: 'Optimize Running Ads', desc: 'Cut waste, scale what\'s working', tag: 'Connect Ad account', color: '#60a5fa', prompt: 'Analyze my running ads and suggest optimizations to cut waste and scale what\'s working.' },
];

interface Props {
  conversationId: string | null;
  onConversationCreated: (id: string) => void;
}

export default function NewTaskPage({ conversationId, onConversationCreated }: Props) {
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [input, setInput] = useState('');
  const [streaming, setStreaming] = useState(false);
  const [thinking, setThinking] = useState(false);
  const [attachedFiles, setAttachedFiles] = useState<FileRef[]>([]);
  const [uploading, setUploading] = useState(false);
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const streamingRef = useRef(false);

  useEffect(() => {
    if (streamingRef.current) return;
    if (conversationId) {
      getConversation(conversationId)
        .then(c => setMessages(c.messages || []))
        .catch(() => setMessages([]));
    } else {
      setMessages([]);
    }
  }, [conversationId]);

  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages]);

  const handleSend = useCallback(async (text?: string) => {
    const msg = text || input.trim();
    if (!msg || streamingRef.current) return;

    log.info('chat', 'sending', { length: msg.length });

    let convId = conversationId;
    if (!convId) {
      const conv = await createConversation();
      convId = conv.id;
      onConversationCreated(conv.id);
    }

    const userMsg: ChatMessage = {
      role: 'user',
      content: msg,
      timestamp: Date.now(),
      files: attachedFiles.length > 0 ? [...attachedFiles] : undefined,
    };

    setMessages(prev => [...prev, userMsg]);
    setInput('');
    setAttachedFiles([]);
    setStreaming(true);
    setThinking(true);
    streamingRef.current = true;

    const filesToSend = attachedFiles.length > 0 ? [...attachedFiles] : undefined;

    await sendChatMessage(
      convId,
      msg,
      (chunk) => {
        if (thinking) setThinking(false);
        setThinking(false);
        setMessages(prev => {
          const last = prev[prev.length - 1];
          if (last?.role === 'model') {
            const updated = [...prev];
            updated[updated.length - 1] = { ...last, content: last.content + chunk };
            return updated;
          }
          return [...prev, { role: 'model', content: chunk, timestamp: Date.now() }];
        });
      },
      () => {
        setStreaming(false);
        setThinking(false);
        streamingRef.current = false;
        log.info('chat', 'stream complete');
      },
      (error) => {
        log.error('chat', 'stream error', { error });
        setMessages(prev => {
          const last = prev[prev.length - 1];
          if (last?.role === 'model') {
            const updated = [...prev];
            updated[updated.length - 1] = { ...last, content: `Error: ${error}` };
            return updated;
          }
          return [...prev, { role: 'model', content: `Error: ${error}`, timestamp: Date.now() }];
        });
        setStreaming(false);
        setThinking(false);
        streamingRef.current = false;
      },
      filesToSend,
    );
  }, [input, conversationId, attachedFiles, onConversationCreated, thinking]);

  const handleFileUpload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const files = e.target.files;
    if (!files || files.length === 0) return;
    setUploading(true);
    try {
      for (const file of Array.from(files)) {
        const ref = await uploadFile(file);
        setAttachedFiles(prev => [...prev, ref]);
      }
    } finally {
      setUploading(false);
      if (fileInputRef.current) fileInputRef.current.value = '';
    }
  };

  const handleEditMessage = useCallback(async (index: number, newContent: string) => {
    if (!conversationId || streamingRef.current) return;
    log.info('chat', 'editing message', { index, newContent: newContent.slice(0, 50) });

    await truncateConversation(conversationId, index);
    setMessages(prev => prev.slice(0, index));

    setTimeout(() => handleSend(newContent), 50);
  }, [conversationId, handleSend]);

  const handleRegenerate = useCallback(async (modelMsgIndex: number) => {
    if (!conversationId || streamingRef.current) return;

    const userMsg = messages.slice(0, modelMsgIndex).reverse().find(m => m.role === 'user');
    if (!userMsg) return;

    log.info('chat', 'regenerating', { modelMsgIndex });

    await truncateConversation(conversationId, modelMsgIndex);
    setMessages(prev => prev.slice(0, modelMsgIndex));

    setTimeout(() => handleSend(userMsg.content), 50);
  }, [conversationId, messages, handleSend]);

  const isEmptyState = !conversationId && messages.length === 0;

  if (isEmptyState) {
    return (
      <div className="flex flex-col items-center pt-16 px-8 max-w-[820px] mx-auto">
        <div className="mb-8 text-center">
          <div className="flex items-center justify-center gap-2 mb-6">
            <div className="p-3 rounded-2xl" style={{ background: 'linear-gradient(135deg, #0e7490, #164e63)' }}>
              <Sparkles size={28} className="text-cyan-300" />
            </div>
          </div>
          <h1 className="text-3xl font-bold text-white tracking-tight mb-2">
            What can I help you grow today?
          </h1>
          <p className="text-sm text-zinc-500">
            Strategy, insights, creatives, and campaign execution. Instant launch available for Meta Ads.
          </p>
        </div>

        <ChatInput
          input={input}
          setInput={setInput}
          onSend={() => handleSend()}
          streaming={streaming}
          attachedFiles={attachedFiles}
          setAttachedFiles={setAttachedFiles}
          uploading={uploading}
          onFileUpload={handleFileUpload}
          fileInputRef={fileInputRef}
        />

        <div className="flex items-center gap-3 w-full my-6">
          <div className="flex-1 border-t border-zinc-800/40" />
          <span className="text-[10px] text-zinc-600 uppercase tracking-widest font-medium">Choose a scene and start immediately</span>
          <div className="flex-1 border-t border-zinc-800/40" />
        </div>

        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3 w-full">
          {QUICK_ACTIONS.map(action => (
            <button
              key={action.title}
              onClick={() => handleSend(action.prompt)}
              className="p-4 rounded-xl border border-zinc-800/40 text-left transition-all hover:border-zinc-700/60 cursor-pointer group"
              style={{ background: '#111114' }}
            >
              <div className="flex items-start gap-3 mb-3">
                <div className="p-1.5 rounded-lg border border-zinc-800/50" style={{ background: '#18181b' }}>
                  <span style={{ color: action.color }}>{action.icon}</span>
                </div>
              </div>
              <h3 className="text-sm font-medium text-zinc-200 mb-1 group-hover:text-white transition-colors">{action.title}</h3>
              <p className="text-xs text-zinc-600 mb-3">{action.desc}</p>
              <span className="text-[10px] font-mono px-2 py-0.5 rounded-md border border-zinc-800/50 text-zinc-500" style={{ background: '#09090b' }}>
                {action.tag}
              </span>
            </button>
          ))}
        </div>
      </div>
    );
  }

  return (
    <div className="flex flex-col h-full">
      {/* Messages */}
      <div className="flex-1 overflow-y-auto px-8 pt-6 pb-4">
        <div className="max-w-[800px] mx-auto space-y-4">
          {messages.map((msg, i) => (
            <MessageBubble
              key={i}
              message={msg}
              isStreaming={streaming && i === messages.length - 1 && msg.role === 'model'}
              onEdit={msg.role === 'user' && !streaming ? (newContent) => handleEditMessage(i, newContent) : undefined}
              onRegenerate={msg.role === 'model' && !streaming ? () => handleRegenerate(i) : undefined}
            />
          ))}
          {thinking && <ThinkingIndicator />}
          <div ref={messagesEndRef} />
        </div>
      </div>

      {/* Input */}
      <div className="shrink-0 border-t border-zinc-800/40 px-8 py-4" style={{ background: '#0c0c0f' }}>
        <div className="max-w-[800px] mx-auto">
          <ChatInput
            input={input}
            setInput={setInput}
            onSend={() => handleSend()}
            streaming={streaming}
            attachedFiles={attachedFiles}
            setAttachedFiles={setAttachedFiles}
            uploading={uploading}
            onFileUpload={handleFileUpload}
            fileInputRef={fileInputRef}
            placeholder="Continue or ask something new..."
          />
        </div>
      </div>
    </div>
  );
}

function ThinkingIndicator() {
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

function MessageBubble({ message, isStreaming, onEdit, onRegenerate }: {
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
            <div className="whitespace-pre-wrap">{message.content}</div>
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

function ChatInput({ input, setInput, onSend, streaming, attachedFiles, setAttachedFiles, uploading, onFileUpload, fileInputRef, placeholder }: {
  input: string;
  setInput: (v: string) => void;
  onSend: () => void;
  streaming: boolean;
  attachedFiles: FileRef[];
  setAttachedFiles: React.Dispatch<React.SetStateAction<FileRef[]>>;
  uploading: boolean;
  onFileUpload: (e: React.ChangeEvent<HTMLInputElement>) => void;
  fileInputRef: React.RefObject<HTMLInputElement | null>;
  placeholder?: string;
}) {
  return (
    <div
      className="w-full rounded-2xl border border-zinc-800/60 p-4 transition-colors focus-within:border-cyan-500/30"
      style={{ background: '#111114' }}
    >
      {attachedFiles.length > 0 && (
        <div className="flex flex-wrap gap-1.5 mb-2">
          {attachedFiles.map(f => (
            <span key={f.id} className="flex items-center gap-1 text-[11px] px-2 py-1 rounded-lg border border-zinc-800/50 text-zinc-400" style={{ background: '#09090b' }}>
              {f.name}
              <button onClick={() => setAttachedFiles(prev => prev.filter(x => x.id !== f.id))} className="text-zinc-600 hover:text-zinc-300 cursor-pointer">
                <X size={10} />
              </button>
            </span>
          ))}
        </div>
      )}
      <input
        type="text"
        value={input}
        onChange={e => setInput(e.target.value)}
        onKeyDown={e => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); onSend(); } }}
        placeholder={placeholder || "Describe your product, goal, or paste a URL..."}
        disabled={streaming}
        className="w-full bg-transparent text-sm text-zinc-200 outline-none placeholder:text-zinc-600 mb-3 disabled:opacity-50"
      />
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <input type="file" ref={fileInputRef} onChange={onFileUpload} className="hidden" multiple />
          <button
            onClick={() => fileInputRef.current?.click()}
            disabled={uploading || streaming}
            className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium border border-zinc-800/50 text-zinc-400 hover:text-zinc-200 hover:border-zinc-700/60 transition-colors cursor-pointer disabled:opacity-30"
            style={{ background: '#09090b' }}
          >
            {uploading ? <Loader2 size={12} className="animate-spin" /> : <Paperclip size={12} />}
          </button>
          <button className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium border border-zinc-800/50 text-zinc-400 hover:text-zinc-200 hover:border-zinc-700/60 transition-colors cursor-pointer"
            style={{ background: '#09090b' }}>
            <Sparkles size={12} className="text-cyan-400" />
            AI Creatives
          </button>
        </div>
        <button
          onClick={onSend}
          disabled={streaming || !input.trim()}
          className="p-2 rounded-lg transition-all cursor-pointer disabled:opacity-30 disabled:cursor-not-allowed"
          style={{ background: input.trim() && !streaming ? 'linear-gradient(135deg, #0e7490, #164e63)' : '#18181b' }}
        >
          {streaming ? <Loader2 size={16} className="text-cyan-300 animate-spin" /> : <Send size={16} className={input.trim() ? 'text-cyan-200' : 'text-zinc-600'} />}
        </button>
      </div>
    </div>
  );
}
