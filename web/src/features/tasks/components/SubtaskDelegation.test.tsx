import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { TaskCard } from './TaskCard';
import { TaskInspectorModal } from './TaskInspectorModal';
import { useTaskStore } from '../store/useTaskStore';
import type { Task } from '../schemas/taskSchemas';

describe('Subagent task delegation UI', () => {
  const parentTask: Task = {
    id: 'parent-12345678',
    project_id: 'proj-1',
    title: 'Parent Orchestration Task',
    prompt: 'Orchestrate child subagent tasks',
    assignee_id: 'coordinator-agent',
    assignee_type: 'agent',
    status: 'in_progress',
    priority: 80,
    skills: ['dispatching-parallel-agents'],
    mcp_servers: ['github'],
    created_at: '2026-10-07T10:00:00Z',
    updated_at: '2026-10-07T10:05:00Z',
  };

  const childTask: Task = {
    id: 'child-87654321',
    project_id: 'proj-1',
    parent_task_id: 'parent-12345678',
    title: 'Delegated Subagent Task 1',
    prompt: 'Implement UI components in frontend',
    assignee_id: 'frontend-subagent',
    assignee_type: 'agent',
    status: 'review',
    priority: 50,
    skills: ['frontend-ui-engineering'],
    mcp_servers: [],
    created_at: '2026-10-07T10:01:00Z',
    updated_at: '2026-10-07T10:04:00Z',
  };

  beforeEach(() => {
    useTaskStore.setState({
      tasks: {
        [parentTask.id]: parentTask,
        [childTask.id]: childTask,
      },
      taskIds: [parentTask.id, childTask.id],
      selectedTaskId: null,
      isLoading: false,
      error: null,
    });
  });

  describe('TaskCard subtask badge', () => {
    it('displays Subtask badge when parent_task_id is present', () => {
      render(<TaskCard id={childTask.id} />);
      expect(screen.getByText('Subtask')).toBeInTheDocument();
      expect(screen.getByText('#child-87')).toBeInTheDocument();
    });

    it('does not display Subtask badge for top-level tasks without parent_task_id', () => {
      render(<TaskCard id={parentTask.id} />);
      expect(screen.queryByText('Subtask')).not.toBeInTheDocument();
      expect(screen.getByText('#parent-1')).toBeInTheDocument();
    });
  });

  describe('TaskInspectorModal subtasks and parent display', () => {
    it('displays Delegated Subtasks section and allows selecting child task', () => {
      useTaskStore.setState({ selectedTaskId: parentTask.id });
      render(<TaskInspectorModal />);

      expect(screen.getByText('Delegated Subtasks')).toBeInTheDocument();
      expect(screen.getByText('1 task')).toBeInTheDocument();
      expect(screen.getByText('Delegated Subagent Task 1')).toBeInTheDocument();

      // Click the delegated subtask to inspect it
      fireEvent.click(screen.getByText('Delegated Subagent Task 1'));
      expect(useTaskStore.getState().selectedTaskId).toBe(childTask.id);
    });

    it('displays Parent Task ID in execution runtime metadata when inspecting child task', () => {
      useTaskStore.setState({ selectedTaskId: childTask.id });
      render(<TaskInspectorModal />);

      expect(screen.getByText('Parent Task:')).toBeInTheDocument();
      expect(screen.getByRole('button', { name: '#parent-1' })).toBeInTheDocument();

      // Clicking parent task link switches inspected task to parent
      fireEvent.click(screen.getByRole('button', { name: '#parent-1' }));
      expect(useTaskStore.getState().selectedTaskId).toBe(parentTask.id);
    });

    it('displays OpenCode Session when session_id is present and allows copying', async () => {
      const taskWithSession: Task = {
        ...parentTask,
        session_id: 'ses_123456abcdef',
      };
      useTaskStore.setState({
        tasks: { [taskWithSession.id]: taskWithSession },
        selectedTaskId: taskWithSession.id,
      });

      render(<TaskInspectorModal />);

      expect(screen.getByText('OpenCode Session:')).toBeInTheDocument();
      expect(screen.getByTestId('opencode-session-id')).toHaveTextContent('ses_123456abcdef');
      expect(screen.getByRole('button', { name: 'Copy OpenCode Session ID' })).toBeInTheDocument();
    });
  });
});
