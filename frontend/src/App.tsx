import React, { useState, useEffect, useRef, useCallback } from 'react';
import {
  PenLine, Gauge, Bot, Palette, Radio, Settings,
  ChevronLeft, ChevronRight, Server, Layers,
  MessageSquare, MoreHorizontal, Pencil, Trash2, Check, X,
  FileText, Users, BookOpen,
} from 'lucide-react';
import { fetchConfig, listConversations, renameConversation, deleteConversation } from './api';
import type { ConversationSummary } from './types';
import { log } from './logger';
import NewTaskPage from './pages/NewTaskPage';
import CockpitPage from './pages/CockpitPage';
import AgentWorkspacePage from './pages/AgentWorkspacePage';
import CreativesPage from './pages/CreativesPage';
import AutopilotPage from './pages/AutopilotPage';
import PromptsPage from './pages/PromptsPage';
import HRPage from './pages/HRPage';
import SkillsPage from './pages/SkillsPage';
import SettingsPage from './pages/SettingsPage';

type Page = 'new-task' | 'cockpit' | 'hr' | 'skills' | 'agent-workspace' | 'creatives' | 'prompts' | 'autopilot' | 'settings';

const NAV_ITEMS: { id: Page; label: string; icon: React.ReactNode }[] = [
  { id: 'new-task',         label: 'New Task',          icon: <PenLine size={18} /> },
  { id: 'cockpit',          label: 'Cockpit',           icon: <Gauge size={18} /> },
  { id: 'hr',               label: 'HR',                icon: <Users size={18} /> },
  { id: 'skills',           label: 'Skills',            icon: <BookOpen size={18} /> },
  { id: 'agent-workspace',  label: 'Agent Workspace',   icon: <Bot size={18} /> },
  { id: 'creatives',        label: 'Creatives',         icon: <Palette size={18} /> },
  { id: 'prompts',          label: 'Prompts',           icon: <FileText size={18} /> },
  { id: 'autopilot',        label: 'Autopilot',         icon: <Radio size={18} /> },
  { id: 'settings',         label: 'Settings',          icon: <Settings size={18} /> },
];

function App() {
  const [activePage, setActivePage] = useState<Page>('new-task');
  const [config, setConfig] = useState<{ project_id: string } | null>(null);
  const [loading, setLoading] = useState(true);
  const [sidebarOpen, setSidebarOpen] = useState(true);

  const [conversations, setConversations] = useState<ConversationSummary[]>([]);
  const [activeConversationId, setActiveConversationId] = useState<string | null>(null);

  const refreshConversations = useCallback(() => {
    listConversations().then(setConversations).catch(() => {});
  }, []);

  useEffect(() => {
    log.info('app', 'initializing');
    fetchConfig()
      .then(setConfig)
      .catch(() => { log.warn('app', 'config fetch failed, using fallback'); setConfig({ project_id: 'Not connected' }); })
      .finally(() => setLoading(false));
    refreshConversations();
  }, [refreshConversations]);

  const handleNewTask = () => {
    log.info('app', 'new task');
    setActivePage('new-task');
    setActiveConversationId(null);
  };

  const handleConversationCreated = (id: string) => {
    log.info('app', 'conversation created', { id });
    setActiveConversationId(id);
    refreshConversations();
  };

  const handleSelectConversation = (id: string) => {
    log.info('app', 'selected conversation', { id });
    setActivePage('new-task');
    setActiveConversationId(id);
  };

  const handleRename = async (id: string, title: string) => {
    log.info('app', 'renaming conversation', { id, title });
    await renameConversation(id, title);
    refreshConversations();
  };

  const handleDelete = async (id: string) => {
    await deleteConversation(id);
    if (activeConversationId === id) setActiveConversationId(null);
    refreshConversations();
  };

  if (loading) {
    return (
      <div className="flex h-screen w-full items-center justify-center" style={{ background: '#09090b' }}>
        <div className="flex flex-col items-center gap-8">
          <div className="relative">
            <div className="absolute inset-0 rounded-full blur-2xl opacity-30 animate-pulse" style={{ background: 'radial-gradient(circle, #38bdf8, transparent)' }} />
            <div className="relative z-10 h-14 w-14 rounded-full border-2 border-transparent animate-spin" style={{ borderTopColor: '#38bdf8', borderRightColor: '#38bdf820' }} />
          </div>
          <div className="text-center">
            <p className="text-zinc-400 text-sm font-medium tracking-wide">Initializing Mobius</p>
            <p className="text-zinc-600 text-xs mt-1 font-mono">Connecting to GCP...</p>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="flex h-screen w-full text-zinc-100 overflow-hidden" style={{ background: '#09090b' }}>

      {/* Sidebar */}
      <aside
        className="border-r border-zinc-800/60 shrink-0 flex flex-col overflow-hidden transition-all duration-300 ease-in-out"
        style={{ background: '#0a0a0d', width: sidebarOpen ? 240 : 56 }}
      >
        {/* Logo */}
        <div className={`flex items-center gap-3 shrink-0 ${sidebarOpen ? 'px-5' : 'px-0 justify-center'} pt-5 pb-4`}>
          <div className="relative shrink-0">
            <div className="absolute inset-0 rounded-xl blur-md opacity-40" style={{ background: '#38bdf8' }} />
            <div className="relative p-2 rounded-xl border border-cyan-500/20" style={{ background: 'linear-gradient(135deg, #0e7490, #164e63)' }}>
              <Layers className="text-cyan-300" size={18} strokeWidth={2.5} />
            </div>
          </div>
          {sidebarOpen && (
            <span className="text-sm font-bold text-white tracking-tight whitespace-nowrap">Mobius</span>
          )}
        </div>

        <div className={`${sidebarOpen ? 'mx-4' : 'mx-2'} border-t border-zinc-800/40 mb-3`} />

        {/* Nav Items */}
        <nav className={`flex flex-col gap-0.5 ${sidebarOpen ? 'px-3' : 'px-1.5'}`}>
          {NAV_ITEMS.map(item => {
            const isActive = activePage === item.id && !(item.id === 'new-task' && activeConversationId);
            return (
              <button
                key={item.id}
                onClick={() => {
                  if (item.id === 'new-task') {
                    handleNewTask();
                  } else {
                    setActivePage(item.id);
                  }
                }}
                className={`flex items-center gap-2.5 rounded-lg cursor-pointer transition-all text-left ${
                  sidebarOpen ? 'px-3 py-2.5' : 'p-2.5 justify-center'
                } ${
                  isActive
                    ? 'text-white font-medium'
                    : 'text-zinc-500 hover:text-zinc-300 hover:bg-zinc-800/30'
                }`}
                style={isActive ? { background: '#18181b', boxShadow: 'inset 0 0 0 1px rgba(63,63,70,0.4)' } : undefined}
                title={sidebarOpen ? undefined : item.label}
              >
                <span className={`shrink-0 ${isActive ? 'text-cyan-400' : ''}`}>{item.icon}</span>
                {sidebarOpen && <span className="text-sm whitespace-nowrap">{item.label}</span>}
              </button>
            );
          })}
        </nav>

        {/* Recents */}
        {sidebarOpen && conversations.length > 0 && (
          <div className="mt-4 flex-1 overflow-y-auto min-h-0">
            <div className="flex items-center gap-2 px-6 mb-2">
              <p className="text-[9px] font-semibold text-zinc-600 uppercase tracking-[0.15em]">Recents</p>
            </div>
            <div className="flex flex-col gap-0.5 px-3">
              {conversations.map(c => (
                <RecentItem
                  key={c.id}
                  conversation={c}
                  isActive={activeConversationId === c.id}
                  onSelect={() => handleSelectConversation(c.id)}
                  onRename={(title) => handleRename(c.id, title)}
                  onDelete={() => handleDelete(c.id)}
                />
              ))}
            </div>
          </div>
        )}

        {!sidebarOpen && conversations.length > 0 && (
          <div className="mt-4 flex flex-col items-center gap-1 px-1.5">
            {conversations.slice(0, 5).map(c => (
              <button
                key={c.id}
                onClick={() => handleSelectConversation(c.id)}
                className={`p-2 rounded-lg cursor-pointer transition-all ${
                  activeConversationId === c.id
                    ? 'text-cyan-400 bg-zinc-800/50'
                    : 'text-zinc-600 hover:text-zinc-300 hover:bg-zinc-800/30'
                }`}
                title={c.title}
              >
                <MessageSquare size={14} />
              </button>
            ))}
          </div>
        )}

        {/* Footer */}
        <div className={`${sidebarOpen ? 'px-3' : 'px-1.5'} pb-4 shrink-0 mt-auto`}>
          {sidebarOpen ? (
            <div className="p-3 rounded-xl border border-zinc-800/40 flex items-center gap-3 mb-3" style={{ background: '#111114' }}>
              <div className="p-2 rounded-lg border border-emerald-500/15 shrink-0" style={{ background: '#052e1610' }}>
                <div className="relative">
                  <span className="absolute -top-0.5 -right-0.5 h-2 w-2 rounded-full bg-emerald-400 animate-pulse" />
                  <Server size={15} className="text-emerald-400" />
                </div>
              </div>
              <div className="flex-1 min-w-0">
                <p className="text-[9px] text-zinc-600 uppercase font-semibold tracking-wider">Project</p>
                <p className="text-xs font-mono truncate text-zinc-400">{config?.project_id || 'Not connected'}</p>
              </div>
            </div>
          ) : (
            <div className="flex justify-center mb-3" title={config?.project_id || 'Not connected'}>
              <div className="p-2 rounded-lg border border-emerald-500/15" style={{ background: '#052e1610' }}>
                <div className="relative">
                  <span className="absolute -top-0.5 -right-0.5 h-2 w-2 rounded-full bg-emerald-400 animate-pulse" />
                  <Server size={15} className="text-emerald-400" />
                </div>
              </div>
            </div>
          )}

          <button
            onClick={() => setSidebarOpen(!sidebarOpen)}
            className={`w-full flex items-center gap-2 rounded-lg text-zinc-600 hover:text-zinc-300 hover:bg-zinc-800/30 cursor-pointer transition-all ${
              sidebarOpen ? 'px-3 py-2' : 'p-2 justify-center'
            }`}
            title={sidebarOpen ? 'Collapse sidebar' : 'Expand sidebar'}
          >
            {sidebarOpen ? (
              <>
                <ChevronLeft size={16} />
                <span className="text-xs">Collapse</span>
              </>
            ) : (
              <ChevronRight size={16} />
            )}
          </button>
        </div>
      </aside>

      {/* Main Content */}
      <main className="flex-1 overflow-hidden relative flex flex-col" style={{ background: '#09090b' }}>
        <div className="absolute top-0 left-1/2 -translate-x-1/2 w-[800px] h-[600px] pointer-events-none" style={{ background: 'radial-gradient(ellipse at center top, rgba(56,189,248,0.06) 0%, transparent 60%)' }} />

        <div className="relative z-10 flex-1 overflow-hidden">
          {activePage === 'new-task' && (
            <NewTaskPage
              conversationId={activeConversationId}
              onConversationCreated={handleConversationCreated}
            />
          )}
          {activePage === 'cockpit' && <div className="overflow-y-auto h-full"><CockpitPage /></div>}
          {activePage === 'hr' && <div className="overflow-y-auto h-full"><HRPage /></div>}
          {activePage === 'skills' && <div className="overflow-y-auto h-full"><SkillsPage /></div>}
          {activePage === 'agent-workspace' && <div className="overflow-y-auto h-full"><AgentWorkspacePage /></div>}
          {activePage === 'creatives' && <div className="overflow-y-auto h-full"><CreativesPage /></div>}
          {activePage === 'prompts' && <div className="overflow-y-auto h-full"><PromptsPage /></div>}
          {activePage === 'autopilot' && <div className="overflow-y-auto h-full"><AutopilotPage /></div>}
          {activePage === 'settings' && <div className="overflow-y-auto h-full"><SettingsPage /></div>}
        </div>
      </main>
    </div>
  );
}

function RecentItem({ conversation, isActive, onSelect, onRename, onDelete }: {
  conversation: ConversationSummary;
  isActive: boolean;
  onSelect: () => void;
  onRename: (title: string) => void;
  onDelete: () => void;
}) {
  const [menuOpen, setMenuOpen] = useState(false);
  const [renaming, setRenaming] = useState(false);
  const [renameValue, setRenameValue] = useState(conversation.title);
  const menuRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) setMenuOpen(false);
    };
    document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, []);

  if (renaming) {
    return (
      <div className="flex items-center gap-1 px-2 py-1.5 rounded-lg" style={{ background: '#18181b' }}>
        <input
          autoFocus
          value={renameValue}
          onChange={e => setRenameValue(e.target.value)}
          onKeyDown={e => {
            if (e.key === 'Enter') { onRename(renameValue); setRenaming(false); }
            if (e.key === 'Escape') setRenaming(false);
          }}
          className="flex-1 text-xs text-zinc-300 bg-transparent outline-none min-w-0"
        />
        <button onClick={() => { onRename(renameValue); setRenaming(false); }} className="text-emerald-400 hover:text-emerald-300 cursor-pointer p-0.5">
          <Check size={12} />
        </button>
        <button onClick={() => setRenaming(false)} className="text-zinc-500 hover:text-zinc-300 cursor-pointer p-0.5">
          <X size={12} />
        </button>
      </div>
    );
  }

  return (
    <div className="relative group" ref={menuRef}>
      <button
        onClick={onSelect}
        className={`w-full flex items-center gap-2 px-2 py-1.5 rounded-lg text-left cursor-pointer transition-all text-xs ${
          isActive ? 'text-white bg-zinc-800/60' : 'text-zinc-500 hover:text-zinc-300 hover:bg-zinc-800/30'
        }`}
      >
        <MessageSquare size={12} className="shrink-0" />
        <span className="truncate flex-1">{conversation.title}</span>
      </button>

      <button
        onClick={(e) => { e.stopPropagation(); setMenuOpen(!menuOpen); }}
        className="absolute right-1 top-1/2 -translate-y-1/2 p-1 rounded text-zinc-700 opacity-0 group-hover:opacity-100 hover:text-zinc-300 hover:bg-zinc-800/50 cursor-pointer transition-all"
      >
        <MoreHorizontal size={12} />
      </button>

      {menuOpen && (
        <div
          className="absolute right-0 top-full mt-1 z-50 rounded-lg border border-zinc-800/60 shadow-xl overflow-hidden"
          style={{ background: '#111114' }}
        >
          <button
            onClick={() => { setMenuOpen(false); setRenaming(true); setRenameValue(conversation.title); }}
            className="flex items-center gap-2 w-full px-3 py-2 text-xs text-zinc-400 hover:text-zinc-200 hover:bg-zinc-800/50 cursor-pointer transition-colors"
          >
            <Pencil size={11} /> Rename
          </button>
          <button
            onClick={() => { setMenuOpen(false); onDelete(); }}
            className="flex items-center gap-2 w-full px-3 py-2 text-xs text-red-400 hover:text-red-300 hover:bg-red-500/10 cursor-pointer transition-colors"
          >
            <Trash2 size={11} /> Delete
          </button>
        </div>
      )}
    </div>
  );
}

export default App;
