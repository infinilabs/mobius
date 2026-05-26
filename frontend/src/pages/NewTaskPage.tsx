import { useState, useEffect, useRef, useCallback } from 'react';
import { createPortal } from 'react-dom';
import {
  Send, Sparkles, BarChart3, ImagePlus, Activity,
  Rocket, Target, Zap, Paperclip, X, Bot, User, Loader2,
  Copy, Pencil, Check, ThumbsUp, ThumbsDown, RefreshCw, Printer,
  Camera, Mic,
} from 'lucide-react';
import { createConversation, getConversation, sendChatMessage, uploadFile, truncateConversation, listEmployees, listModels, fetchSettings } from '../api';
import type { ChatMessage, FileRef, Employee, VertexModel } from '../types';

export type ChatTarget =
  | { kind: 'agent'; agent: Employee }
  | { kind: 'model'; model: VertexModel };
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
  initialAgentId?: string;
  initialProjectId?: string;
}

export default function NewTaskPage({ conversationId, onConversationCreated, initialAgentId, initialProjectId }: Props) {
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [input, setInput] = useState('');
  const [streaming, setStreaming] = useState(false);
  const [thinking, setThinking] = useState(false);
  const [attachedFiles, setAttachedFiles] = useState<FileRef[]>([]);
  const [uploading, setUploading] = useState(false);
  const [agents, setAgents] = useState<Employee[]>([]);
  const [registeredModels, setRegisteredModels] = useState<VertexModel[]>([]);
  const [chatTarget, setChatTarget] = useState<ChatTarget | null>(null);
  const [maxFileSizeMB, setMaxFileSizeMB] = useState(20);
  const [previewUrls, setPreviewUrls] = useState<Record<string, string>>({});
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const streamingRef = useRef(false);

  useEffect(() => {
    listEmployees().then(emps => {
      const chatEligible = emps.filter(e =>
        e.role === 'CEO' || e.tags.includes('manager') || e.tags.includes('founder')
      ).sort((a, b) => {
        if (a.role === 'CEO') return -1;
        if (b.role === 'CEO') return 1;
        return a.name.localeCompare(b.name);
      });
      setAgents(chatEligible);
      const targetId = initialAgentId;
      if (targetId) {
        const target = chatEligible.find(e => e.id === targetId);
        if (target) { setChatTarget({ kind: 'agent', agent: target }); return; }
      }
      setChatTarget(prev => {
        if (prev) return prev;
        const ceo = chatEligible.find(e => e.role === 'CEO');
        return ceo ? { kind: 'agent', agent: ceo } : prev;
      });
    }).catch(() => {});
    listModels().then(setRegisteredModels).catch(() => {});
    fetchSettings().then(s => { if (s.chat_upload?.max_file_size_mb) setMaxFileSizeMB(s.chat_upload.max_file_size_mb); }).catch(() => {});
  }, [initialAgentId]);

  useEffect(() => {
    return () => { Object.values(previewUrls).forEach(url => URL.revokeObjectURL(url)); };
  }, [previewUrls]);

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

  const handleSend = useCallback(async (text?: string, overrideFiles?: FileRef[]) => {
    const msg = text || input.trim();
    const files = overrideFiles || attachedFiles;
    if ((!msg && files.length === 0) || streamingRef.current) return;

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
      files: files.length > 0 ? [...files] : undefined,
    };

    setMessages(prev => [...prev, userMsg]);
    setInput('');
    setAttachedFiles([]);
    setStreaming(true);
    setThinking(true);
    streamingRef.current = true;

    const filesToSend = files.length > 0 ? [...files] : undefined;

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
      chatTarget?.kind === 'agent' ? chatTarget.agent.id : undefined,
      chatTarget?.kind === 'model' ? chatTarget.model.model_id : undefined,
      initialProjectId,
    );
  }, [input, conversationId, attachedFiles, onConversationCreated, thinking, chatTarget, initialProjectId]);

  const addFileWithPreview = useCallback(async (file: File): Promise<FileRef | null> => {
    if (file.size > maxFileSizeMB * 1024 * 1024) {
      alert(`File too large: max ${maxFileSizeMB} MB`);
      return null;
    }
    const isImage = file.type.startsWith('image/');
    const ref = await uploadFile(file);
    setAttachedFiles(prev => [...prev, ref]);
    if (isImage) {
      const url = URL.createObjectURL(file);
      setPreviewUrls(prev => ({ ...prev, [ref.id]: url }));
    }
    return ref;
  }, [maxFileSizeMB]);

  const handleFileUpload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const files = e.target.files;
    if (!files || files.length === 0) return;
    setUploading(true);
    try {
      for (const file of Array.from(files)) {
        await addFileWithPreview(file);
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
      <div className="h-full overflow-y-auto">
      <div className="flex flex-col items-center pt-16 px-8 pb-12 max-w-[820px] mx-auto">
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
          agents={agents}
          registeredModels={registeredModels}
          chatTarget={chatTarget}
          onSelectTarget={setChatTarget}
          previewUrls={previewUrls}
          onAddFile={addFileWithPreview}
          onAutoSend={(ref) => handleSend('', [ref])}
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
      </div>
    );
  }

  const showMissingModelWarning = chatTarget?.kind === 'agent'
    && !chatTarget.agent.models?.some(m => m.purpose === 'primary_llm');

  return (
    <div className="flex flex-col h-full">
      {/* Missing LLM warning */}
      {showMissingModelWarning && (
        <div className="shrink-0 px-8 pt-3">
          <div className="max-w-[800px] mx-auto flex items-center gap-2 px-4 py-2 rounded-lg border border-amber-500/20 text-xs text-amber-400"
            style={{ background: '#78350f15' }}>
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className="shrink-0">
              <path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/>
            </svg>
            <span><strong>{chatTarget.agent.name}</strong> has no LLM model assigned — using the default model. Assign one in HR Management.</span>
          </div>
        </div>
      )}

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
            agents={agents}
            registeredModels={registeredModels}
            chatTarget={chatTarget}
            onSelectTarget={setChatTarget}
            previewUrls={previewUrls}
            onAddFile={addFileWithPreview}
            onAutoSend={(ref) => handleSend('', [ref])}
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

const ROLE_COLORS: Record<string, string> = {
  CEO: '#38bdf8', PM: '#c084fc', Engineer: '#4ade80',
  QA: '#fbbf24', Designer: '#fb7185', Custom: '#a1a1aa',
};

const MODEL_TYPE_COLORS: Record<string, string> = {
  llm: '#38bdf8', image: '#c084fc', video: '#fbbf24',
};

function getTargetDisplay(target: ChatTarget | null): { label: string; initial: string; color: string } {
  if (!target) return { label: 'Select', initial: '?', color: '#a1a1aa' };
  if (target.kind === 'agent') {
    return {
      label: target.agent.name,
      initial: target.agent.name[0],
      color: ROLE_COLORS[target.agent.role] || ROLE_COLORS.Custom,
    };
  }
  return {
    label: target.model.name || target.model.model_id,
    initial: target.model.name?.[0]?.toUpperCase() || 'M',
    color: MODEL_TYPE_COLORS[target.model.type] || '#a1a1aa',
  };
}

function getPlaceholder(target: ChatTarget | null, fallback?: string): string {
  if (!target) return fallback || 'Describe your product, goal, or paste a URL...';
  if (target.kind === 'agent') return `Talk to ${target.agent.name} (${target.agent.title})...`;
  return `Chat with ${target.model.name || target.model.model_id}...`;
}

function ChatInput({ input, setInput, onSend, streaming, attachedFiles, setAttachedFiles, uploading, onFileUpload, fileInputRef, placeholder, agents, registeredModels, chatTarget, onSelectTarget, previewUrls, onAddFile, onAutoSend }: {
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
  agents: Employee[];
  registeredModels: VertexModel[];
  chatTarget: ChatTarget | null;
  onSelectTarget: (t: ChatTarget) => void;
  previewUrls: Record<string, string>;
  onAddFile: (file: File) => Promise<FileRef | null>;
  onAutoSend: (ref: FileRef) => void;
}) {
  const [menuOpen, setMenuOpen] = useState(false);
  const [menuPos, setMenuPos] = useState<{ left: number; top?: number; bottom?: number } | null>(null);
  const [showCamera, setShowCamera] = useState(false);
  const [pasting, setPasting] = useState(false);
  const menuRef = useRef<HTMLDivElement>(null);
  const dropdownRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const cameraInputRef = useRef<HTMLInputElement>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  useEffect(() => {
    const handler = (e: MouseEvent) => {
      const target = e.target as Node;
      if (menuRef.current?.contains(target)) return;
      if (dropdownRef.current?.contains(target)) return;
      setMenuOpen(false);
    };
    document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, []);

  const handlePaste = async (e: React.ClipboardEvent) => {
    const items = e.clipboardData?.items;
    if (!items) return;
    for (const item of Array.from(items)) {
      if (item.type.startsWith('image/')) {
        e.preventDefault();
        const file = item.getAsFile();
        if (!file) continue;
        setPasting(true);
        try { await onAddFile(file); } finally { setPasting(false); }
        return;
      }
    }
  };

  const handleCameraCapture = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const files = e.target.files;
    if (!files || files.length === 0) return;
    for (const file of Array.from(files)) {
      await onAddFile(file);
    }
    if (cameraInputRef.current) cameraInputRef.current.value = '';
  };

  const handleVoiceComplete = async (blob: Blob) => {
    const ext = blob.type.includes('mp4') ? 'mp4' : 'webm';
    const file = new File([blob], `voice-message.${ext}`, { type: blob.type });
    const ref = await onAddFile(file);
    if (ref) onAutoSend(ref);
  };

  const autoResizeTextarea = (el: HTMLTextAreaElement) => {
    el.style.height = 'auto';
    el.style.height = Math.min(el.scrollHeight, 120) + 'px';
  };

  const display = getTargetDisplay(chatTarget);
  const hasItems = agents.length > 0 || registeredModels.length > 0;
  const hasMediaDevices = typeof navigator !== 'undefined' && !!navigator.mediaDevices?.getUserMedia;
  const canSend = !streaming && (input.trim() || attachedFiles.length > 0);

  return (
    <div
      className="w-full rounded-2xl border border-zinc-800/60 p-4 transition-colors focus-within:border-cyan-500/30"
      style={{ background: '#111114' }}
    >
      {/* Attachment previews */}
      {attachedFiles.length > 0 && (
        <div className="flex flex-wrap gap-1.5 mb-2">
          {attachedFiles.map(f => {
            const preview = previewUrls[f.id];
            return (
              <span key={f.id} className="relative flex items-center gap-1 text-[11px] px-2 py-1 rounded-lg border border-zinc-800/50 text-zinc-400 group" style={{ background: '#09090b' }}>
                {preview ? (
                  <img src={preview} alt={f.name} className="w-8 h-8 rounded object-cover" />
                ) : null}
                <span className="truncate max-w-[100px]">{f.name}</span>
                <button onClick={() => setAttachedFiles(prev => prev.filter(x => x.id !== f.id))} className="text-zinc-600 hover:text-zinc-300 cursor-pointer">
                  <X size={10} />
                </button>
              </span>
            );
          })}
          {pasting && <span className="text-[10px] text-cyan-400 flex items-center gap-1"><Loader2 size={10} className="animate-spin" /> Pasting...</span>}
        </div>
      )}

      <textarea
        ref={textareaRef}
        value={input}
        onChange={e => { setInput(e.target.value); autoResizeTextarea(e.target); }}
        onKeyDown={e => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); onSend(); } }}
        onPaste={handlePaste}
        placeholder={getPlaceholder(chatTarget, placeholder)}
        disabled={streaming}
        className="w-full bg-transparent text-sm text-zinc-200 outline-none placeholder:text-zinc-600 mb-3 disabled:opacity-50 resize-none"
        rows={1}
        style={{ maxHeight: 120 }}
      />
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          {/* Target Selector */}
          {hasItems && (
            <div className="relative" ref={menuRef}>
              <button
                ref={triggerRef}
                onClick={() => {
                  if (!menuOpen && triggerRef.current) {
                    const rect = triggerRef.current.getBoundingClientRect();
                    const spaceAbove = rect.top;
                    const spaceBelow = window.innerHeight - rect.bottom;
                    if (spaceAbove > spaceBelow) {
                      setMenuPos({ left: rect.left, bottom: window.innerHeight - rect.top + 8, top: undefined });
                    } else {
                      setMenuPos({ left: rect.left, bottom: undefined, top: rect.bottom + 8 });
                    }
                  }
                  setMenuOpen(!menuOpen);
                }}
                className="flex items-center gap-1.5 px-2.5 py-1.5 rounded-lg text-xs font-medium border border-zinc-800/50 hover:border-zinc-700/60 transition-colors cursor-pointer"
                style={{ background: '#09090b' }}
              >
                <div className="w-5 h-5 rounded-full flex items-center justify-center text-[9px] font-bold shrink-0"
                  style={{ background: `${display.color}25`, color: display.color, border: `1.5px solid ${display.color}40` }}>
                  {display.initial}
                </div>
                <span className="text-zinc-300 max-w-[100px] truncate">{display.label}</span>
                <svg width="8" height="8" viewBox="0 0 8 8" className="text-zinc-600 shrink-0">
                  <path d="M1 3L4 6L7 3" stroke="currentColor" strokeWidth="1.5" fill="none" strokeLinecap="round" />
                </svg>
              </button>

              {menuOpen && menuPos && createPortal(
                <div ref={dropdownRef} className="fixed z-[9999] rounded-xl border border-zinc-800/60 shadow-xl min-w-[220px] overflow-y-auto"
                  style={{ background: '#0a0a0d', left: menuPos.left, top: menuPos.top, bottom: menuPos.bottom, maxHeight: menuPos.top != null ? `calc(100vh - ${menuPos.top}px - 16px)` : menuPos.bottom != null ? `calc(100vh - ${menuPos.bottom}px - 16px)` : 400 }}>
                  {agents.length > 0 && (
                    <>
                      <div className="px-3 py-2 border-b border-zinc-800/40">
                        <p className="text-[9px] font-semibold text-zinc-600 uppercase tracking-wider">Employees</p>
                      </div>
                      <div className="py-1">
                        {agents.map(agent => {
                          const color = ROLE_COLORS[agent.role] || ROLE_COLORS.Custom;
                          const isActive = chatTarget?.kind === 'agent' && chatTarget.agent.id === agent.id;
                          return (
                            <button key={agent.id} onClick={() => { onSelectTarget({ kind: 'agent', agent }); setMenuOpen(false); }}
                              className={`w-full flex items-center gap-2.5 px-3 py-2 text-left cursor-pointer transition-colors ${isActive ? 'bg-zinc-800/50' : 'hover:bg-zinc-800/30'}`}>
                              <div className="w-7 h-7 rounded-full flex items-center justify-center text-[10px] font-bold shrink-0"
                                style={{ background: `${color}20`, color, border: `1.5px solid ${color}40` }}>{agent.name[0]}</div>
                              <div className="flex-1 min-w-0">
                                <p className={`text-xs font-medium truncate ${isActive ? 'text-white' : 'text-zinc-300'}`}>{agent.name}</p>
                                <p className="text-[10px] text-zinc-600 truncate">{agent.title}</p>
                              </div>
                              <span className="px-1.5 py-0.5 rounded text-[8px] font-bold uppercase shrink-0" style={{ color, background: `${color}12`, border: `1px solid ${color}25` }}>{agent.role}</span>
                            </button>
                          );
                        })}
                      </div>
                    </>
                  )}
                  {registeredModels.length > 0 && (
                    <>
                      <div className={`px-3 py-2 border-b border-zinc-800/40 ${agents.length > 0 ? 'border-t' : ''}`}>
                        <p className="text-[9px] font-semibold text-zinc-600 uppercase tracking-wider">Models (Direct)</p>
                      </div>
                      <div className="py-1">
                        {registeredModels.map(model => {
                          const color = MODEL_TYPE_COLORS[model.type] || '#a1a1aa';
                          const isActive = chatTarget?.kind === 'model' && chatTarget.model.id === model.id;
                          return (
                            <button key={model.id} onClick={() => { onSelectTarget({ kind: 'model', model }); setMenuOpen(false); }}
                              className={`w-full flex items-center gap-2.5 px-3 py-2 text-left cursor-pointer transition-colors ${isActive ? 'bg-zinc-800/50' : 'hover:bg-zinc-800/30'}`}>
                              <div className="w-7 h-7 rounded-lg flex items-center justify-center text-[10px] font-bold shrink-0"
                                style={{ background: `${color}15`, color, border: `1.5px solid ${color}30` }}>{(model.name || model.model_id)[0].toUpperCase()}</div>
                              <div className="flex-1 min-w-0">
                                <p className={`text-xs font-medium truncate ${isActive ? 'text-white' : 'text-zinc-300'}`}>{model.name || model.model_id}</p>
                                <p className="text-[10px] text-zinc-600 truncate font-mono">{model.model_id}</p>
                              </div>
                              <span className="px-1.5 py-0.5 rounded text-[8px] font-bold uppercase shrink-0" style={{ color, background: `${color}12`, border: `1px solid ${color}25` }}>{model.type}</span>
                            </button>
                          );
                        })}
                      </div>
                    </>
                  )}
                </div>,
                document.body,
              )}
            </div>
          )}

          <input type="file" ref={fileInputRef} onChange={onFileUpload} className="hidden" multiple />
          <button onClick={() => fileInputRef.current?.click()} disabled={uploading || streaming}
            className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium border border-zinc-800/50 text-zinc-400 hover:text-zinc-200 hover:border-zinc-700/60 transition-colors cursor-pointer disabled:opacity-30"
            style={{ background: '#09090b' }} title="Attach file">
            {uploading ? <Loader2 size={12} className="animate-spin" /> : <Paperclip size={12} />}
          </button>

          {/* Camera */}
          <input type="file" accept="image/*" capture="environment" ref={cameraInputRef} onChange={handleCameraCapture} className="hidden" />
          <button
            onClick={() => {
              const isMobile = /iPhone|iPad|iPod|Android/i.test(navigator.userAgent);
              if (isMobile) { cameraInputRef.current?.click(); }
              else if (hasMediaDevices) { setShowCamera(true); }
              else { cameraInputRef.current?.click(); }
            }}
            disabled={streaming}
            className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium border border-zinc-800/50 text-zinc-400 hover:text-zinc-200 hover:border-zinc-700/60 transition-colors cursor-pointer disabled:opacity-30"
            style={{ background: '#09090b' }} title="Take photo">
            <Camera size={12} />
          </button>

          {/* Voice */}
          {hasMediaDevices && (
            <VoiceRecordButton disabled={streaming} onComplete={handleVoiceComplete} />
          )}

          <button className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium border border-zinc-800/50 text-zinc-400 hover:text-zinc-200 hover:border-zinc-700/60 transition-colors cursor-pointer"
            style={{ background: '#09090b' }}>
            <Sparkles size={12} className="text-cyan-400" />
            AI Creatives
          </button>
        </div>
        <button
          onClick={onSend}
          disabled={!canSend}
          className="p-2 rounded-lg transition-all cursor-pointer disabled:opacity-30 disabled:cursor-not-allowed"
          style={{ background: canSend ? 'linear-gradient(135deg, #0e7490, #164e63)' : '#18181b' }}
        >
          {streaming ? <Loader2 size={16} className="text-cyan-300 animate-spin" /> : <Send size={16} className={canSend ? 'text-cyan-200' : 'text-zinc-600'} />}
        </button>
      </div>

      {showCamera && <CameraCaptureModal onCapture={async (file) => { await onAddFile(file); setShowCamera(false); }} onClose={() => setShowCamera(false)} />}
    </div>
  );
}

function CameraCaptureModal({ onCapture, onClose }: { onCapture: (file: File) => Promise<void>; onClose: () => void }) {
  const videoRef = useRef<HTMLVideoElement>(null);
  const streamRef = useRef<MediaStream | null>(null);
  const [ready, setReady] = useState(false);
  const [capturing, setCapturing] = useState(false);

  useEffect(() => {
    let cancelled = false;
    navigator.mediaDevices.getUserMedia({ video: { facingMode: 'environment' } }).then(stream => {
      if (cancelled) { stream.getTracks().forEach(t => t.stop()); return; }
      streamRef.current = stream;
      if (videoRef.current) { videoRef.current.srcObject = stream; }
      setReady(true);
    }).catch(() => onClose());
    return () => { cancelled = true; streamRef.current?.getTracks().forEach(t => t.stop()); };
  }, [onClose]);

  const takePhoto = async () => {
    if (!videoRef.current) return;
    setCapturing(true);
    const video = videoRef.current;
    const canvas = document.createElement('canvas');
    canvas.width = video.videoWidth;
    canvas.height = video.videoHeight;
    canvas.getContext('2d')?.drawImage(video, 0, 0);
    canvas.toBlob(async (blob) => {
      if (blob) {
        const file = new File([blob], `photo-${Date.now()}.jpg`, { type: 'image/jpeg' });
        await onCapture(file);
      }
      streamRef.current?.getTracks().forEach(t => t.stop());
    }, 'image/jpeg', 0.9);
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4" style={{ background: 'rgba(0,0,0,0.8)' }}>
      <div className="w-full max-w-md rounded-xl border border-zinc-800/60 overflow-hidden" style={{ background: '#0f0f12' }}>
        <div className="flex items-center justify-between px-4 py-3 border-b border-zinc-800/40">
          <span className="text-sm font-semibold text-white">Camera</span>
          <button onClick={() => { streamRef.current?.getTracks().forEach(t => t.stop()); onClose(); }} className="text-zinc-500 hover:text-zinc-300 cursor-pointer"><X size={16} /></button>
        </div>
        <div className="relative aspect-[4/3] bg-black">
          <video ref={videoRef} autoPlay playsInline muted className="w-full h-full object-cover" />
          {!ready && <div className="absolute inset-0 flex items-center justify-center"><Loader2 className="text-zinc-500 animate-spin" /></div>}
        </div>
        <div className="flex justify-center py-4">
          <button onClick={takePhoto} disabled={!ready || capturing}
            className="w-14 h-14 rounded-full border-4 border-white/80 cursor-pointer transition-all hover:scale-105 active:scale-95 disabled:opacity-30"
            style={{ background: 'linear-gradient(135deg, #0e7490, #164e63)' }} />
        </div>
      </div>
    </div>
  );
}

function VoiceRecordButton({ disabled, onComplete }: { disabled: boolean; onComplete: (blob: Blob) => Promise<void> }) {
  const [recording, setRecording] = useState(false);
  const [cancelled, setCancelled] = useState(false);
  const [elapsed, setElapsed] = useState(0);
  const recorderRef = useRef<MediaRecorder | null>(null);
  const streamRef = useRef<MediaStream | null>(null);
  const chunksRef = useRef<Blob[]>([]);
  const startYRef = useRef(0);
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null);

  useEffect(() => {
    return () => {
      if (timerRef.current) clearInterval(timerRef.current);
      streamRef.current?.getTracks().forEach(t => t.stop());
    };
  }, []);

  const stopRecording = useCallback(() => {
    if (timerRef.current) { clearInterval(timerRef.current); timerRef.current = null; }
    recorderRef.current?.stop();
    streamRef.current?.getTracks().forEach(t => t.stop());
    setRecording(false);
    setElapsed(0);
  }, []);

  const handlePointerDown = async (e: React.PointerEvent) => {
    if (disabled || recording) return;
    e.preventDefault();
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    startYRef.current = e.clientY;
    setCancelled(false);
    chunksRef.current = [];

    try {
      const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
      streamRef.current = stream;
      const mimeType = MediaRecorder.isTypeSupported('audio/webm') ? 'audio/webm' : 'audio/mp4';
      const recorder = new MediaRecorder(stream, { mimeType });
      recorderRef.current = recorder;
      recorder.ondataavailable = (ev) => { if (ev.data.size > 0) chunksRef.current.push(ev.data); };
      recorder.start();
      setRecording(true);
      timerRef.current = setInterval(() => setElapsed(prev => prev + 1), 1000);
    } catch {
      // microphone permission denied
    }
  };

  const handlePointerMove = (e: React.PointerEvent) => {
    if (!recording) return;
    const delta = startYRef.current - e.clientY;
    setCancelled(delta > 60);
  };

  const handlePointerUp = () => {
    if (!recording) return;
    const wasCancelled = cancelled;
    stopRecording();

    if (wasCancelled) {
      chunksRef.current = [];
      setCancelled(false);
      return;
    }

    const recorder = recorderRef.current;
    if (recorder) {
      recorder.onstop = () => {
        if (chunksRef.current.length > 0) {
          const blob = new Blob(chunksRef.current, { type: chunksRef.current[0].type });
          onComplete(blob);
        }
      };
    }
  };

  const fmtTime = (s: number) => `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`;

  const btnClass = "flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium border transition-colors cursor-pointer disabled:opacity-30 select-none touch-none";

  return (
    <button
      onPointerDown={handlePointerDown}
      onPointerMove={handlePointerMove}
      onPointerUp={handlePointerUp}
      onPointerCancel={() => { if (recording) { stopRecording(); setCancelled(false); } }}
      disabled={disabled}
      className={`${btnClass} ${recording
        ? cancelled
          ? 'border-red-700/50 text-red-400 bg-red-900/20'
          : 'border-red-500/50 text-red-300 bg-red-900/30 animate-pulse'
        : 'border-zinc-800/50 text-zinc-400 hover:text-zinc-200 hover:border-zinc-700/60'
      }`}
      style={recording ? undefined : { background: '#09090b' }}
      title={recording ? (cancelled ? 'Release to cancel' : 'Release to send') : 'Hold to talk'}
    >
      <Mic size={12} />
      {recording && (
        <span className="text-[10px] font-mono">
          {cancelled ? '↑ Cancel' : fmtTime(elapsed)}
        </span>
      )}
    </button>
  );
}
