import { useEffect, useRef, useState } from 'react';
import { parseWsFrames } from '../utils/terminalParser';
import type { TaskSessionResponse } from '../schemas/taskSchemas';

interface LiveUpdatesOptions {
  taskId: string;
  status?: string;
  isSteeringActive: boolean;
  setSession: (session: TaskSessionResponse) => void;
  refresh: () => Promise<unknown>;
  onSteeringSettled: () => void;
  onPolled: () => void;
}

const POLL_INTERVAL_MS = 800;
const MAX_POLLS = 60;

function socketUrl(path: string, taskId: string): string {
  const isHttps = typeof window !== 'undefined' && window.location.protocol === 'https:';
  const host = typeof window !== 'undefined' ? window.location.host : 'localhost:8080';
  return `${isHttps ? 'wss:' : 'ws:'}//${host}/api/v1/ws/${path}/${encodeURIComponent(taskId)}`;
}

// Keeps the session fresh. The server relays the polled OpenCode session over a WebSocket and pushes only changes.
// While that stream is connected the browser does not poll; when it drops, a running session is polled instead.
// Agent events on the terminal socket refresh the session when the stream is not live.
export function useSessionLiveUpdates({
  taskId,
  status,
  isSteeringActive,
  setSession,
  refresh,
  onSteeringSettled,
  onPolled,
}: LiveUpdatesOptions) {
  const [streamLive, setStreamLive] = useState(false);
  // Mirrors streamLive for the terminal socket handler, which is set up once per task.
  const streamLiveRef = useRef(false);

  // The effects below are set up once per task, so they read the latest callbacks through a ref.
  const latest = useRef({ setSession, refresh, onSteeringSettled, onPolled, status });
  useEffect(() => {
    latest.current = { setSession, refresh, onSteeringSettled, onPolled, status };
  });

  useEffect(() => {
    if (!taskId || typeof WebSocket === 'undefined') return;
    const setLive = (live: boolean) => {
      streamLiveRef.current = live;
      setStreamLive(live);
    };
    let socket: WebSocket | null = null;
    try {
      socket = new WebSocket(socketUrl('session', taskId));
      socket.onmessage = (event) => {
        try {
          const msg = JSON.parse(typeof event.data === 'string' ? event.data : '') as {
            type?: string;
            session?: TaskSessionResponse;
          };
          if (msg.type === 'session' && msg.session) {
            setLive(true);
            // A written frame also clears an earlier error, as a successful query fetch does.
            latest.current.setSession(msg.session);
            if (msg.session.status && msg.session.status !== 'running') latest.current.onSteeringSettled();
          } else if (msg.type === 'error') {
            // The relay could not read the session (e.g. sandbox not running): use the normal fetch path.
            setLive(false);
          }
        } catch {
          // ignore malformed frames
        }
      };
      socket.onclose = () => setLive(false);
      socket.onerror = () => setLive(false);
    } catch {
      // streaming is optional; polling covers it
    }
    return () => {
      setLive(false);
      socket?.close();
    };
  }, [taskId]);

  useEffect(() => {
    const isRunning = status === 'running' || isSteeringActive;
    if (!isRunning || streamLive) return;

    let pollCount = 0;
    const interval = setInterval(async () => {
      pollCount++;
      await latest.current.refresh();
      latest.current.onPolled();
      // Stop steering mode once the session is no longer running, or after the poll budget.
      const current = latest.current.status;
      if ((current && current !== 'running' && pollCount > 4) || pollCount >= MAX_POLLS) {
        latest.current.onSteeringSettled();
      }
    }, POLL_INTERVAL_MS);

    return () => clearInterval(interval);
  }, [status, isSteeringActive, taskId, streamLive]);

  useEffect(() => {
    if (!taskId) return;
    try {
      const socket = new WebSocket(socketUrl('terminal', taskId));
      socket.onmessage = (event) => {
        const frames = parseWsFrames(typeof event.data === 'string' ? event.data : '');
        for (const frame of frames) {
          if (frame.type !== 'agent_event') continue;
          // While the session stream is live it pushes every change, so the terminal event refreshes only without it.
          if (!streamLiveRef.current) void latest.current.refresh();
          const payload = frame.payload as { eventType?: string } | undefined;
          if (payload?.eventType === 'step_finish') latest.current.onSteeringSettled();
        }
      };
      return () => socket.close();
    } catch {
      // ignore
    }
  }, [taskId]);
}
