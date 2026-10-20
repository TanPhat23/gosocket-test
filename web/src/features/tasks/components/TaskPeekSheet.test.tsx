import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { TaskPeekSheet } from './TaskPeekSheet';
import { useTaskStore } from '../store/useTaskStore';
import { useWorkspaceSettingsStore } from '../store/useWorkspaceSettingsStore';
import type { TaskDTO } from '../schemas/taskSchemas';

describe('TaskPeekSheet', () => {
  const mockTask1: TaskDTO = {
    id: 'task-1111-uuid',
    project_id: 'proj-1',
    title: 'First preview task',
    prompt: 'Implement preview feature with detailed prompt content',
    assignee_id: 'agent-1',
    assignee_type: 'agent',
    status: 'in_progress',
    priority: 85,
    model: 'anthropic/claude-3-5-sonnet',
    integration_id: 'repo-1',
    pull_request_url: 'https://github.com/example/repo/pull/42',
    external_id: '123',
    external_url: 'https://github.com/example/repo/issues/123',
    skills: ['testing', 'frontend'],
    mcp_servers: ['github'],
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
  };

  const mockTask2: TaskDTO = {
    id: 'task-2222-uuid',
    project_id: 'proj-1',
    title: 'Second preview task',
    prompt: 'Second task prompt description',
    assignee_id: 'agent-2',
    assignee_type: 'agent',
    status: 'in_progress',
    priority: 45,
    skills: [],
    mcp_servers: [],
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
  };

  beforeEach(() => {
    vi.restoreAllMocks();
    useTaskStore.setState({
      tasks: {
        [mockTask1.id]: mockTask1,
        [mockTask2.id]: mockTask2,
      },
      taskIds: [mockTask1.id, mockTask2.id],
      selectedTaskId: null,
      isLoading: false,
      error: null,
    });
    useWorkspaceSettingsStore.setState({
      connectedRepos: [
        {
          id: 'repo-1',
          project_id: 'proj-1',
          credential_id: 'cred-1',
          provider: 'github',
          external_id: 'owner/repo-project',
          external_url: 'https://github.com/owner/repo-project',
          enabled: true,
          config: { base_branch: 'main' },
          created_at: new Date().toISOString(),
          updated_at: new Date().toISOString(),
        },
      ],
    });
  });

  it('renders task title, short id, status, priority, prompt, and repository/PR context', () => {
    render(
      <TaskPeekSheet
        taskId={mockTask1.id}
        taskIds={[mockTask1.id, mockTask2.id]}
        open={true}
        onOpenChange={vi.fn()}
        onTaskChange={vi.fn()}
        onOpenInspector={vi.fn()}
        finalFocus={() => null}
      />
    );

    expect(screen.getByRole('dialog')).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: /First preview task/i })).toBeInTheDocument();
    expect(screen.getByText('#task-111')).toBeInTheDocument();
    expect(screen.getByText('in_progress')).toBeInTheDocument();
    expect(screen.getByText(/Critical/i)).toBeInTheDocument();
    expect(screen.getByText(/Implement preview feature with detailed prompt content/i)).toBeInTheDocument();
    expect(screen.getByText('owner/repo-project')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /PR/i })).toHaveAttribute(
      'href',
      'https://github.com/example/repo/pull/42'
    );
  });

  it('disables previous control on first task and enables next control', () => {
    const handleTaskChange = vi.fn();
    render(
      <TaskPeekSheet
        taskId={mockTask1.id}
        taskIds={[mockTask1.id, mockTask2.id]}
        open={true}
        onOpenChange={vi.fn()}
        onTaskChange={handleTaskChange}
        onOpenInspector={vi.fn()}
        finalFocus={() => null}
      />
    );

    const prevButton = screen.getByRole('button', { name: /previous task/i });
    const nextButton = screen.getByRole('button', { name: /next task/i });

    expect(prevButton).toBeDisabled();
    expect(nextButton).not.toBeDisabled();

    fireEvent.click(nextButton);
    expect(handleTaskChange).toHaveBeenCalledWith(mockTask2.id);
  });

  it('disables next control on last task', () => {
    const handleTaskChange = vi.fn();
    render(
      <TaskPeekSheet
        taskId={mockTask2.id}
        taskIds={[mockTask1.id, mockTask2.id]}
        open={true}
        onOpenChange={vi.fn()}
        onTaskChange={handleTaskChange}
        onOpenInspector={vi.fn()}
        finalFocus={() => null}
      />
    );

    const prevButton = screen.getByRole('button', { name: /previous task/i });
    const nextButton = screen.getByRole('button', { name: /next task/i });

    expect(prevButton).not.toBeDisabled();
    expect(nextButton).toBeDisabled();

    fireEvent.click(prevButton);
    expect(handleTaskChange).toHaveBeenCalledWith(mockTask1.id);
  });

  it('disables both navigation controls when current task is not in taskIds', () => {
    render(
      <TaskPeekSheet
        taskId={mockTask1.id}
        taskIds={[mockTask2.id]}
        open={true}
        onOpenChange={vi.fn()}
        onTaskChange={vi.fn()}
        onOpenInspector={vi.fn()}
        finalFocus={() => null}
      />
    );

    expect(screen.getByRole('button', { name: /previous task/i })).toBeDisabled();
    expect(screen.getByRole('button', { name: /next task/i })).toBeDisabled();
  });

  it('closes the sheet if task does not exist in store', () => {
    const handleOpenChange = vi.fn();
    render(
      <TaskPeekSheet
        taskId="non-existent-task"
        taskIds={[mockTask1.id]}
        open={true}
        onOpenChange={handleOpenChange}
        onTaskChange={vi.fn()}
        onOpenInspector={vi.fn()}
        finalFocus={() => null}
      />
    );

    expect(handleOpenChange).toHaveBeenCalledWith(false);
  });

  it('calls onOpenInspector when clicking the inspector button', () => {
    const handleOpenInspector = vi.fn();
    render(
      <TaskPeekSheet
        taskId={mockTask1.id}
        taskIds={[mockTask1.id, mockTask2.id]}
        open={true}
        onOpenChange={vi.fn()}
        onTaskChange={vi.fn()}
        onOpenInspector={handleOpenInspector}
        finalFocus={() => null}
      />
    );

    const inspectBtn = screen.getByRole('button', { name: /open full inspector/i });
    fireEvent.click(inspectBtn);
    expect(handleOpenInspector).toHaveBeenCalledWith(mockTask1.id);
  });

  it('supports j and k keyboard navigation scoped to open peek', () => {
    const handleTaskChange = vi.fn();
    const { rerender } = render(
      <TaskPeekSheet
        taskId={mockTask1.id}
        taskIds={[mockTask1.id, mockTask2.id]}
        open={true}
        onOpenChange={vi.fn()}
        onTaskChange={handleTaskChange}
        onOpenInspector={vi.fn()}
        finalFocus={() => null}
      />
    );

    // Press j to go to next
    fireEvent.keyDown(window, { key: 'j' });
    expect(handleTaskChange).toHaveBeenCalledWith(mockTask2.id);

    // Now switch to task2
    rerender(
      <TaskPeekSheet
        taskId={mockTask2.id}
        taskIds={[mockTask1.id, mockTask2.id]}
        open={true}
        onOpenChange={vi.fn()}
        onTaskChange={handleTaskChange}
        onOpenInspector={vi.fn()}
        finalFocus={() => null}
      />
    );

    // Press k to go to previous
    fireEvent.keyDown(window, { key: 'k' });
    expect(handleTaskChange).toHaveBeenCalledWith(mockTask1.id);
  });

  it('ignores j and k when typing in editable targets or when modifiers are pressed', () => {
    const handleTaskChange = vi.fn();
    render(
      <div>
        <input data-testid="test-input" />
        <TaskPeekSheet
          taskId={mockTask1.id}
          taskIds={[mockTask1.id, mockTask2.id]}
          open={true}
          onOpenChange={vi.fn()}
          onTaskChange={handleTaskChange}
          onOpenInspector={vi.fn()}
          finalFocus={() => null}
        />
      </div>
    );

    const input = screen.getByTestId('test-input');

    // Event targeting the input
    fireEvent.keyDown(input, { key: 'j' });
    expect(handleTaskChange).not.toHaveBeenCalled();

    // Event with modifier key
    fireEvent.keyDown(window, { key: 'j', metaKey: true });
    fireEvent.keyDown(window, { key: 'j', ctrlKey: true });
    fireEvent.keyDown(window, { key: 'j', altKey: true });
    expect(handleTaskChange).not.toHaveBeenCalled();
  });

  it('includes a polite live announcement for screen-reader users', () => {
    render(
      <TaskPeekSheet
        taskId={mockTask1.id}
        taskIds={[mockTask1.id, mockTask2.id]}
        open={true}
        onOpenChange={vi.fn()}
        onTaskChange={vi.fn()}
        onOpenInspector={vi.fn()}
        finalFocus={() => null}
      />
    );

    const liveRegion = screen.getByRole('status');
    expect(liveRegion).toHaveAttribute('aria-live', 'polite');
    expect(liveRegion).toHaveTextContent(/First preview task/i);
  });
});
