import axios from 'axios';
import type { Event } from '../types';

// Events / Activity
export async function listEvents(filters?: { event_type?: string; actor_id?: string; project_id?: string; task_id?: string; limit?: number }): Promise<Event[]> {
  const params = new URLSearchParams();
  if (filters?.event_type) params.set('event_type', filters.event_type);
  if (filters?.actor_id) params.set('actor_id', filters.actor_id);
  if (filters?.project_id) params.set('project_id', filters.project_id);
  if (filters?.task_id) params.set('task_id', filters.task_id);
  if (filters?.limit) params.set('limit', String(filters.limit));
  const qs = params.toString();
  const { data } = await axios.get(`/api/events${qs ? `?${qs}` : ''}`);
  return data.events ?? [];
}

// subscribeEvents opens the live event stream (/api/events/ws) and invokes
// onEvent for every backend event. Reconnects with capped exponential backoff;
// onStatus reports connectivity so callers can fall back to polling while the
// socket is down. Returns an unsubscribe function. Auth rides on the
// mobius_token cookie set in client.ts (browser WebSockets cannot set headers).
export function subscribeEvents(
  onEvent: (event: Event) => void,
  onStatus?: (connected: boolean) => void,
): () => void {
  let ws: WebSocket | null = null;
  let closed = false;
  let retryMs = 1000;
  let retryTimer: ReturnType<typeof setTimeout> | null = null;

  const connect = () => {
    if (closed) return;
    const proto = window.location.protocol === 'https:' ? 'wss' : 'ws';
    ws = new WebSocket(`${proto}://${window.location.host}/api/events/ws`);
    ws.onopen = () => {
      retryMs = 1000;
      onStatus?.(true);
    };
    ws.onmessage = (msg) => {
      try {
        onEvent(JSON.parse(msg.data));
      } catch {
        // malformed frame — ignore
      }
    };
    ws.onclose = () => {
      // After unsubscribe, the browser still fires onclose asynchronously —
      // notifying then would restart the caller's poll fallback in a dead
      // effect closure (unclearable timer leak).
      if (closed) return;
      onStatus?.(false);
      retryTimer = setTimeout(connect, retryMs);
      retryMs = Math.min(retryMs * 2, 30000);
    };
    ws.onerror = () => {
      ws?.close();
    };
  };

  connect();
  return () => {
    closed = true;
    if (retryTimer) clearTimeout(retryTimer);
    ws?.close();
  };
}
