import { useEffect, useRef } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { useTaskStore } from '../store/useTaskStore';
import type { TaskDTO, TaskStatus } from '../schemas/taskSchemas';

export function useTaskSubscription() {
  const queryClient = useQueryClient();
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
          // Sync any tasks that changed during reconnection. The active task query refetches and WorkspaceView copies
          // the result into the store, so this is one request rather than one for the store and one for the query.
          queryClient.invalidateQueries({ queryKey: ['tasks'] });
        };

        ws.onmessage = (event) => {
          try {
            const data = JSON.parse(event.data);
            const taskId = data?.task_id || data?.taskId;

            // Handle task status updates
            if (data && taskId && (data.type === 'status' || data.type === 'task_status_changed')) {
              const newStatus = (data.payload || data.status) as TaskStatus;
              if (newStatus) {
                updateTaskStatus(taskId, newStatus);
                queryClient.setQueryData<TaskDTO[]>(['tasks'], (oldTasks) => {
                  if (!oldTasks) return oldTasks;
                  return oldTasks.map((t) =>
                    t.id === taskId ? { ...t, status: newStatus } : t
                  );
                });
                queryClient.setQueryData<TaskDTO>(['tasks', taskId], (oldTask) => {
                  if (!oldTask) return oldTask;
                  return { ...oldTask, status: newStatus };
                });
              }
            }

            // Handle agent events
            if (data && (data.type === 'agent_event' || data.type === 'tool_use' || data.type === 'agent_event_emitted')) {
              if (taskId) {
                queryClient.invalidateQueries({ queryKey: ['tasks', taskId, 'session'] });
              }
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
  }, [updateTaskStatus]);
}
