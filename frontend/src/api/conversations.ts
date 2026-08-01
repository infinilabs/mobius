import axios from 'axios';
import type { Conversation, ConversationSummary, FileRef } from '../types';
import { log } from '../logger';

export async function listConversations(): Promise<ConversationSummary[]> {
  log.debug('api', 'listConversations');
  const { data } = await axios.get('/api/conversations');
  log.debug('api', 'conversations listed', { count: data.length });
  return data;
}

export async function createConversation(): Promise<Conversation> {
  log.info('api', 'creating conversation');
  const { data } = await axios.post('/api/conversations');
  log.info('api', 'conversation created', { id: data.id });
  return data;
}

export async function getConversation(id: string): Promise<Conversation> {
  log.debug('api', 'getConversation', { id });
  const { data } = await axios.get(`/api/conversations/${id}`);
  log.debug('api', 'conversation loaded', { id, messages: data.messages?.length ?? 0 });
  return data;
}

export async function renameConversation(id: string, title: string): Promise<void> {
  log.info('api', 'renaming conversation', { id, title });
  await axios.put(`/api/conversations/${id}`, { title });
}

export async function deleteConversation(id: string): Promise<void> {
  log.info('api', 'deleting conversation', { id });
  await axios.delete(`/api/conversations/${id}`);
}

export async function truncateConversation(id: string, keepCount: number): Promise<Conversation> {
  log.info('api', 'truncating conversation', { id, keepCount });
  const { data } = await axios.post(`/api/conversations/${id}/truncate`, { keep_count: keepCount });
  return data;
}

export async function uploadFile(file: File): Promise<FileRef> {
  log.info('api', 'uploading file', { name: file.name, size: file.size });
  const form = new FormData();
  form.append('file', file);
  const { data } = await axios.post('/api/upload', form);
  log.info('api', 'file uploaded', { id: data.id, name: data.name });
  return data;
}

export async function sendChatMessage(
  conversationId: string,
  message: string,
  onChunk: (text: string) => void,
  onDone: () => void,
  onError: (error: string) => void,
  files?: FileRef[],
  agentId?: string,
  modelId?: string,
  projectId?: string,
  onToolEvent?: (name: string, status: string) => void,
): Promise<void> {
  log.info('chat', 'sending message', { conversationId, length: message.length, files: files?.length ?? 0, agentId, modelId, projectId });

  let response: Response;
  try {
    response = await fetch('/api/chat', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        conversation_id: conversationId,
        message,
        files: files?.length ? files : undefined,
        agent_id: agentId || undefined,
        model_id: modelId || undefined,
        project_id: projectId || undefined,
      }),
    });
  } catch (err) {
    const msg = err instanceof Error ? err.message : 'Network error';
    log.error('chat', 'fetch failed', { error: msg });
    onError(msg);
    return;
  }

  if (!response.ok) {
    log.error('chat', 'HTTP error', { status: response.status });
    onError(`HTTP ${response.status}`);
    return;
  }

  const reader = response.body?.getReader();
  if (!reader) {
    log.error('chat', 'no response stream');
    onError('No response stream');
    return;
  }

  const decoder = new TextDecoder();
  let buffer = '';
  let chunkCount = 0;

  while (true) {
    const { done, value } = await reader.read();
    if (done) break;

    buffer += decoder.decode(value, { stream: true });
    const lines = buffer.split('\n');
    buffer = lines.pop() || '';

    for (const line of lines) {
      if (!line.startsWith('data: ')) continue;
      const json_str = line.slice(6).trim();
      if (!json_str) continue;

      try {
        const parsed = JSON.parse(json_str);
        if (parsed.done) {
          log.info('chat', 'stream complete', { conversationId, chunks: chunkCount });
          onDone();
          return;
        }
        if (parsed.error) {
          log.error('chat', 'stream error from server', { error: parsed.error });
          onError(parsed.error);
          return;
        }
        if (parsed.tool_call) {
          onToolEvent?.(parsed.tool_call, parsed.status || '');
          continue;
        }
        if (parsed.text) {
          chunkCount++;
          onChunk(parsed.text);
        }
      } catch {
        log.warn('chat', 'malformed SSE chunk', { raw: json_str.slice(0, 100) });
      }
    }
  }
  log.info('chat', 'stream ended', { conversationId, chunks: chunkCount });
  onDone();
}
