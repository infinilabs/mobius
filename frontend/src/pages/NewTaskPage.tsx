import { useState, useEffect, useRef, useCallback } from 'react';
import {
  Sparkles, BarChart3, ImagePlus, Activity,
  Rocket, Target, Zap,
} from 'lucide-react';
import { createConversation, getConversation, sendChatMessage, uploadFile, truncateConversation, listEmployees, listModels, fetchSettings, listTasks, listTaskComments, updateTaskStatus } from '../api';
import type { ChatMessage, FileRef, Employee, VertexModel, Task } from '../types';
import { log } from '../logger';
import { ChatInput, type ChatTarget } from './newtask/ChatInput';
import { MessageBubble, ThinkingIndicator, ToolActivity } from './newtask/MessageBubble';
import { TaskStatusStrip, BlockedTasksCallout } from './newtask/TaskStatus';
import { TASK_NOTICE, latestSystemError } from './newtask/taskNotices';

export type { ChatTarget };

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
  onOpenProjectTasks?: (projectId: string) => void;
  onOpenTask?: (taskId: string) => void;
}

export default function NewTaskPage({ conversationId, onConversationCreated, initialAgentId, initialProjectId, onOpenProjectTasks, onOpenTask }: Props) {
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [linkedTasks, setLinkedTasks] = useState<Task[]>([]);
  const [blockedReasons, setBlockedReasons] = useState<Record<string, string>>({});
  const [projectId, setProjectId] = useState<string | undefined>(initialProjectId);
  const [input, setInput] = useState('');
  const [streaming, setStreaming] = useState(false);
  const [thinking, setThinking] = useState(false);
  const [toolEvents, setToolEvents] = useState<{ name: string; status: string }[]>([]);
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
  const prevTaskStatusRef = useRef<Record<string, string>>({});
  const taskBaselineRef = useRef(false);

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
        .then(c => {
          setMessages(c.messages || []);
          setProjectId(c.project_id || initialProjectId || undefined);
        })
        .catch(() => setMessages([]));
    } else {
      setMessages([]);
      setProjectId(initialProjectId);
    }
  }, [conversationId, initialProjectId]);

  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages]);

  // Reconstruct live project/task status on (re)open and keep it fresh while the
  // chat is mounted, so switching views and coming back never loses "what is
  // happening". Scope by project when known (matches the Tasks filter and fixes
  // the count); fall back to the conversation link for project-less chats.
  useEffect(() => {
    const filter = projectId
      ? { project_id: projectId }
      : conversationId
      ? { conversation_id: conversationId }
      : null;
    // Reset the transition baseline whenever the scope changes so we don't
    // replay stale notices after a view switch.
    prevTaskStatusRef.current = {};
    taskBaselineRef.current = false;
    if (!filter) { setLinkedTasks([]); return; }
    let cancelled = false;
    const announce = (tasks: Task[]) => {
      const prev = prevTaskStatusRef.current;
      const firstRun = !taskBaselineRef.current;
      const notices: string[] = [];
      for (const t of tasks) {
        const before = prev[t.id];
        if (before && before !== t.status && TASK_NOTICE[t.status]) {
          notices.push(TASK_NOTICE[t.status](t));
        }
      }
      prevTaskStatusRef.current = Object.fromEntries(tasks.map(t => [t.id, t.status]));
      taskBaselineRef.current = true;
      // First poll just establishes the baseline — never announce on open.
      if (!firstRun && notices.length > 0) {
        setMessages(m => [
          ...m,
          ...notices.map(content => ({ role: 'model' as const, content, timestamp: Date.now() })),
        ]);
      }
    };
    const refresh = () => {
      listTasks(filter)
        .then(tasks => { if (!cancelled) { setLinkedTasks(tasks); announce(tasks); } })
        .catch(() => {});
    };
    refresh();
    const interval = setInterval(refresh, 6000);
    return () => { cancelled = true; clearInterval(interval); };
  }, [projectId, conversationId]);

  // Fetch the latest failure reason for blocked tasks so the chat callout can
  // explain *why* a task is stuck. Keyed on the blocked-id set, so it only
  // refetches when something actually enters or leaves the blocked state.
  const blockedKey = linkedTasks.filter(t => t.status === 'blocked').map(t => t.id).sort().join(',');
  useEffect(() => {
    if (!blockedKey) { setBlockedReasons({}); return; }
    const ids = blockedKey.split(',');
    let cancelled = false;
    Promise.all(ids.map(id =>
      listTaskComments(id)
        .then(cs => [id, latestSystemError(cs)] as const)
        .catch(() => [id, ''] as const)
    )).then(pairs => { if (!cancelled) setBlockedReasons(Object.fromEntries(pairs)); });
    return () => { cancelled = true; };
  }, [blockedKey]);

  const handleUnblock = useCallback(async (taskId: string) => {
    // Optimistic flip to 'ready'; the next poll reconciles with the server.
    setLinkedTasks(prev => prev.map(t => (t.id === taskId ? { ...t, status: 'ready' } : t)));
    try {
      await updateTaskStatus(taskId, 'ready');
    } catch {
      // Revert on failure — the task is still blocked server-side.
      setLinkedTasks(prev => prev.map(t => (t.id === taskId ? { ...t, status: 'blocked' } : t)));
    }
  }, []);

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
    setToolEvents([]);
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
      (name) => {
        setThinking(false);
        setToolEvents(prev => [...prev, { name, status: 'executed' }]);
      },
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
          locked={messages.length > 0}
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

      {/* Live project/task status — survives view switches via polling */}
      <TaskStatusStrip
        tasks={linkedTasks}
        onOpen={projectId && onOpenProjectTasks ? () => onOpenProjectTasks(projectId) : undefined}
      />
      <BlockedTasksCallout
        tasks={linkedTasks}
        reasons={blockedReasons}
        onOpen={onOpenTask}
        onUnblock={handleUnblock}
      />

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
          {toolEvents.length > 0 && <ToolActivity events={toolEvents} />}
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
            locked={messages.length > 0}
          />
        </div>
      </div>
    </div>
  );
}
