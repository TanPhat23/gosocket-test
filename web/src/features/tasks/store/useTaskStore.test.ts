import { describe, it, expect, beforeEach } from 'vitest';
import { useTaskStore } from './useTaskStore';
import type { TaskDTO as Task } from '../schemas/taskSchemas';

describe('useTaskStore real-time updates', () => {
  beforeEach(() => {
    useTaskStore.setState({
      tasks: {},
      taskIds: [],
      selectedTaskId: null,
      isLoading: false,
      error: null,
    });
  });

  it('updates task status in real time and updates timestamp', () => {
    const mockTask: Task = {
      id: 'task-100',
      project_id: 'proj-1',
      title: 'Real-time test task',
      prompt: 'Execute step',
      assignee_id: 'opencode',
      assignee_type: 'agent',
      status: 'queued',
      priority: 50,
      skills: [],
      mcp_servers: [],
      created_at: '2026-10-06T14:00:00Z',
      updated_at: '2026-10-06T14:00:00Z',
    };

    useTaskStore.setState({
      tasks: { 'task-100': mockTask },
      taskIds: ['task-100'],
    });

    expect(useTaskStore.getState().tasks['task-100'].status).toBe('queued');

    // Simulate real-time status update from WebSocket event
    useTaskStore.getState().updateTaskStatus('task-100', 'in_progress');

    expect(useTaskStore.getState().tasks['task-100'].status).toBe('in_progress');
    expect(useTaskStore.getState().tasks['task-100'].updated_at).not.toBe('2026-10-06T14:00:00Z');

    // Transition to review
    useTaskStore.getState().updateTaskStatus('task-100', 'review');
    expect(useTaskStore.getState().tasks['task-100'].status).toBe('review');
  });
});
