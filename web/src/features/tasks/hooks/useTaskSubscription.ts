import { useEffect, useRef } from 'react';
import { useTaskStore } from '../store/useTaskStore';
import type { TaskStatus } from '../schemas/taskSchemas';

export function useTaskSubscription() {
  const fetchTasks = useTaskStore((state) => state.fetchTasks);
  const updateTaskStatus = useTaskStore((state) => state.updateTaskStatus);
  const wsRef = useRef<WebSocket | null>(null);

  useEffect(() => {
    let reconnectTimeout: number | undefined;
    let isSubscribed = true;

    const connect = () => {
      if (!isSubscribed) return;

      try {
        const isHttps = typeof window !== 'undefined' && window.location.protocol === 'https:';
        const wsProtocol = isHttps ? 'wss:' : 'ws:';
        const host = typeof window !== 'undefined' ? window.location.host : 'localhost:8080';
        const wsUrl = `${wsProtocol}//${host}/api/v1/ws/events`;

        const ws = new WebSocket(wsUrl);
        wsRef.current = ws;

        ws.onopen = () => {
          // Sync any tasks that changed during reconnection
          fetchTasks();
        };

        ws.onmessage = (event) => {
          try {
            const data = JSON.parse(event.data);
            if (data && data.task_id && data.type === 'status' && data.payload) {
              updateTaskStatus(data.task_id, data.payload as TaskStatus);
            }
          } catch {
            // Ignore non-json frames
          }
        };

        ws.onclose = () => {
          if (isSubscribed) {
            reconnectTimeout = window.setTimeout(connect, 2500);
          }
        };

        ws.onerror = () => {
          if (ws) ws.close();
        };
      } catch {
        if (isSubscribed) {
          reconnectTimeout = window.setTimeout(connect, 3000);
        }
      }
    };

    connect();

    // Adaptive polling: when tasks are queued or in_progress, refresh every 3s as backup
    const pollInterval = window.setInterval(() => {
      const tasks = Object.values(useTaskStore.getState().tasks);
      const hasActive = tasks.some(
        (t) => t.status === 'queued' || t.status === 'in_progress'
      );
      if (hasActive) {
        useTaskStore.getState().fetchTasks();
      }
    }, 3000);

    return () => {
      isSubscribed = false;
      if (reconnectTimeout) clearTimeout(reconnectTimeout);
      clearInterval(pollInterval);
      if (wsRef.current) {
        wsRef.current.close();
        wsRef.current = null;
      }
    };
  }, [fetchTasks, updateTaskStatus]);
}
