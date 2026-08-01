import { useState, useEffect, useRef, useCallback } from 'react';
import { createPortal } from 'react-dom';
import { Send, Sparkles, Paperclip, X, Loader2, Camera, Mic } from 'lucide-react';
import type { Employee, VertexModel, FileRef } from '../../types';

export type ChatTarget =
  | { kind: 'agent'; agent: Employee }
  | { kind: 'model'; model: VertexModel };

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

export function ChatInput({ input, setInput, onSend, streaming, attachedFiles, setAttachedFiles, uploading, onFileUpload, fileInputRef, placeholder, agents, registeredModels, chatTarget, onSelectTarget, previewUrls, onAddFile, onAutoSend, locked }: {
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
  locked?: boolean;
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
                disabled={locked}
                onClick={() => {
                  if (locked) return;
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
                className={`flex items-center gap-1.5 px-2.5 py-1.5 rounded-lg text-xs font-medium border border-zinc-800/50 transition-colors ${locked ? 'opacity-60 cursor-not-allowed' : 'hover:border-zinc-700/60 cursor-pointer'}`}
                style={{ background: '#09090b' }}
                title={locked ? `Talking to ${display.label} — start a New Task to switch` : undefined}
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
