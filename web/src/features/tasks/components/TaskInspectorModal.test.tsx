import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { TaskInspectorModal } from './TaskInspectorModal';
import { useTaskStore } from '../store/useTaskStore';
import { taskApi } from '../api/taskApi';
import type { TaskDTO, TaskSessionResponse } from '../schemas/taskSchemas';

describe('TaskInspectorModal Agent Session Tab and Steering', () => {
  const mockTask: TaskDTO = {
    id: 'task-sess-123',
    project_id: 'proj-1',
    title: 'Autonomous Feature Implementation',
    prompt: 'Implement live agent inspector modal',
    assignee_id: 'agent-1',
    assignee_type: 'agent',
    status: 'in_progress',
    priority: 50,
    skills: ['testing'],
    mcp_servers: ['context7'],
    session_id: 'ses_atm_task-sess-123',
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
  };

  const mockSessionData: TaskSessionResponse = {
    task_id: 'task-sess-123',
    session_id: 'ses_atm_task-sess-123',
    status: 'succeeded',
    model: 'gemini-3.8-flash',
    tokens: {
      input: 177897,
      output: 634,
      reasoning: 0,
      cache_read: 4200,
      cache_write: 0,
    },
    cost: 0.0152,
    messages: [
      {
        id: 'msg_user_1',
        type: 'user',
        role: 'user',
        text: 'Initial prompt or follow-up prompt',
        created_at: 1791357502838,
      },
      {
        id: 'msg_asst_1',
        type: 'assistant',
        role: 'assistant',
        agent: 'coordinator',
        model: 'gemini-3.8-flash',
        text: 'Assistant textual reply',
        tool_calls: [
          {
            name: 'shell',
            input: '{"command":"go test ./..."}',
            output: 'PASS\nok agent-task-manager 1.2s',
            status: 'completed',
            exit_code: 0,
          },
        ],
        created_at: 1791357538433,
      },
    ],
  };

  beforeEach(() => {
    vi.restoreAllMocks();
    useTaskStore.setState({
      tasks: { [mockTask.id]: mockTask },
      taskIds: [mockTask.id],
      selectedTaskId: mockTask.id,
    });
  });

  it('renders Agent Session tab trigger and displays session header, tokens, and messages', async () => {
    const getSessionSpy = vi.spyOn(taskApi, 'getTaskSession').mockResolvedValue(mockSessionData);

    render(<TaskInspectorModal />);

    // Check tab presence
    const sessionTabTrigger = screen.getByTestId('tab-agent-session');
    expect(sessionTabTrigger).toBeInTheDocument();
    expect(sessionTabTrigger).toHaveTextContent('Agent Session');

    // Switch to session tab
    fireEvent.click(sessionTabTrigger);

    expect(getSessionSpy).toHaveBeenCalledWith('task-sess-123');
    // The transcript renders once the shared query resolves.
    await screen.findByText('Initial prompt or follow-up prompt');

    // Session ID display
    expect(screen.getByTestId('session-id-display')).toHaveTextContent('ses_atm_task-sess-123');

    // Token metrics
    const tokenRow = screen.getByTestId('token-metrics-row');
    expect(tokenRow).toBeInTheDocument();
    expect(tokenRow).toHaveTextContent('In:177,897');
    expect(tokenRow).toHaveTextContent('Out:634');
    expect(tokenRow).toHaveTextContent('Cache: 4,200');

    // Messages
    expect(screen.getByText('Initial prompt or follow-up prompt')).toBeInTheDocument();
    expect(screen.getByText('Assistant textual reply')).toBeInTheDocument();
    expect(screen.getByText(/Agent \(coordinator\)/i)).toBeInTheDocument();

    // Tool call card
    expect(screen.getByTestId('tool-call-card')).toBeInTheDocument();
    expect(screen.getByText('shell')).toBeInTheDocument();
    expect(screen.getByText('exit 0')).toBeInTheDocument();

    // Expand tool call
    const toolToggleBtn = screen.getByTestId('toggle-tool-shell');
    fireEvent.click(toolToggleBtn);
    expect(screen.getByText('PASS ok agent-task-manager 1.2s')).toBeInTheDocument();
  });

  it('fetches the task session once per open, shared by the metrics card and the Agent Session tab', async () => {
    const getSessionSpy = vi.spyOn(taskApi, 'getTaskSession').mockResolvedValue(mockSessionData);

    render(<TaskInspectorModal />);
    await screen.findByText(/Session Token Metrics/);

    fireEvent.click(screen.getByTestId('tab-agent-session'));
    await waitFor(() => {
      expect(screen.getByTestId('session-transcript-container')).toBeInTheDocument();
    });

    expect(getSessionSpy).toHaveBeenCalledTimes(1);
  });

  it('allows submitting an interactive steer prompt and refreshes transcript', async () => {
    vi.spyOn(taskApi, 'getTaskSession').mockResolvedValue(mockSessionData);
    const promptSpy = vi.spyOn(taskApi, 'promptTaskSession').mockResolvedValue(undefined);

    render(<TaskInspectorModal />);

    // Switch to session tab
    fireEvent.click(screen.getByTestId('tab-agent-session'));

    await waitFor(() => {
      expect(screen.getByTestId('steer-prompt-textarea')).toBeInTheDocument();
    });

    const textarea = screen.getByTestId('steer-prompt-textarea');
    const sendBtn = screen.getByTestId('send-steer-btn');

    // Initially disabled when empty
    expect(sendBtn).toBeDisabled();

    // Type steer prompt
    fireEvent.change(textarea, { target: { value: 'Fix failing test in task_handlers.go' } });
    expect(sendBtn).not.toBeDisabled();

    // Click steer
    fireEvent.click(sendBtn);

    await waitFor(() => {
      expect(promptSpy).toHaveBeenCalledWith('task-sess-123', 'Fix failing test in task_handlers.go');
    });

    // Input should be cleared
    expect(textarea).toHaveValue('');
  });

  it('supports compacting session context with compact button', async () => {
    vi.spyOn(taskApi, 'getTaskSession').mockResolvedValue(mockSessionData);
    const compactSpy = vi.spyOn(taskApi, 'compactTaskSession').mockResolvedValue(undefined);

    render(<TaskInspectorModal />);

    fireEvent.click(screen.getByTestId('tab-agent-session'));

    // The button is disabled until the shared session query has loaded.
    await waitFor(() => {
      expect(screen.getByTestId('compact-session-btn')).toBeEnabled();
    });

    const compactBtn = screen.getByTestId('compact-session-btn');
    fireEvent.click(compactBtn);

    await waitFor(() => {
      expect(compactSpy).toHaveBeenCalledWith('task-sess-123');
      expect(screen.getByText(/Session context successfully compacted/i)).toBeInTheDocument();
    });
  });

  it('renders graceful service unavailable banner when 503 is returned', async () => {
    const error503 = { status: 503, message: 'Agent execution service unavailable' };
    vi.spyOn(taskApi, 'getTaskSession').mockRejectedValue(error503);

    render(<TaskInspectorModal />);

    fireEvent.click(screen.getByTestId('tab-agent-session'));

    await waitFor(() => {
      expect(screen.getByText(/Agent Execution Service Unavailable/i)).toBeInTheDocument();
      expect(screen.getByTestId('retry-session-btn')).toBeInTheDocument();
    });
  });

  it('shows a simulated-run state and disables steering for dev runtime sessions', async () => {
    vi.spyOn(taskApi, 'getTaskSession').mockResolvedValue({
      task_id: 'task-1',
      session_id: '',
      simulated: true,
      messages: [],
    });

    render(<TaskInspectorModal />);

    fireEvent.click(screen.getByTestId('tab-agent-session'));

    await waitFor(() => {
      expect(screen.getByTestId('simulated-session')).toBeInTheDocument();
    });
    expect(screen.queryByTestId('retry-session-btn')).not.toBeInTheDocument();
  });

  it('supports controlled taskId and onClose callback props', () => {
    useTaskStore.setState({
      tasks: {
        'task-prop-1': { ...mockTask, id: 'task-prop-1', title: 'Controlled Prop Task' },
      },
      selectedTaskId: null,
    });

    const handleClose = vi.fn();
    const { rerender } = render(<TaskInspectorModal taskId="task-prop-1" onClose={handleClose} />);

    expect(screen.getByText('Controlled Prop Task')).toBeInTheDocument();

    // Trigger dialog close via close button or openChange
    const closeBtn = screen.getByRole('button', { name: /close/i });
    fireEvent.click(closeBtn);
    expect(handleClose).toHaveBeenCalledTimes(1);

    // If taskId is null, dialog shouldn't render
    rerender(<TaskInspectorModal taskId={null} onClose={handleClose} />);
    expect(screen.queryByText('Controlled Prop Task')).not.toBeInTheDocument();
  });

  it('renders task.prompt using Markdown in the Overview tab and only shows Task Workflow bar when appropriate', () => {
    // In progress task without PR or PR error should NOT show Task Workflow bar
    const { unmount } = render(<TaskInspectorModal />);

    // Check Markdown rendering in Overview tab
    expect(screen.getByText('Prompt & Specification')).toBeInTheDocument();
    expect(screen.getByText('Implement live agent inspector modal')).toBeInTheDocument();

    // Workflow bar should be absent because task is in_progress and has no PR
    expect(screen.queryByText('Task Workflow:')).not.toBeInTheDocument();

    unmount();

    // When task is completed, Task Workflow bar appears with Create PR button
    useTaskStore.setState({
      tasks: {
        [mockTask.id]: {
          ...mockTask,
          status: 'done',
        },
      },
    });

    render(<TaskInspectorModal />);
    expect(screen.getByText('Task Workflow:')).toBeInTheDocument();
    expect(screen.getByTestId('create-pr-btn')).toBeInTheDocument();
  });
});

