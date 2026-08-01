import { useEffect, useRef } from 'react';
import { subscribeEvents } from '../api';

const DEBOUNCE_MS = 500;

// useLiveRefresh calls refresh when backend events arrive over the WebSocket
// stream (debounced against bursts), replacing fixed-interval polling
// (plan 7.3). While the socket is down it falls back to polling every pollMs.
export function useLiveRefresh(refresh: () => void, pollMs = 15000) {
  const refreshRef = useRef(refresh);
  useEffect(() => {
    refreshRef.current = refresh;
  }, [refresh]);

  useEffect(() => {
    let debounce: ReturnType<typeof setTimeout> | null = null;
    let poll: ReturnType<typeof setInterval> | null = null;

    const unsubscribe = subscribeEvents(
      () => {
        if (debounce) clearTimeout(debounce);
        debounce = setTimeout(() => refreshRef.current(), DEBOUNCE_MS);
      },
      (connected) => {
        if (connected) {
          if (poll) { clearInterval(poll); poll = null; }
          // Catch up on anything missed while disconnected.
          refreshRef.current();
        } else if (!poll) {
          poll = setInterval(() => refreshRef.current(), pollMs);
        }
      },
    );

    return () => {
      unsubscribe();
      if (debounce) clearTimeout(debounce);
      if (poll) clearInterval(poll);
    };
  }, [pollMs]);
}
