import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { TaskCard } from './TaskCard';
import { TaskInspectorModal } from './TaskInspectorModal';
import { useTaskStore } from '../store/useTaskStore';
import { taskApi } from '../api/taskApi';
import type { TaskDTO } from '../schemas/taskSchemas';

describe('GitHub enhancements in TaskCard and TaskInspectorModal', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    useTaskStore.setState({
      tasks: {},
      taskIds: [],
      selectedTaskId: null,
    });
  });

  it('renders GH issue badge and PR badge on TaskCard when fields exist', () => {
    const taskWithGithub: TaskDTO = {
      id: 'task-gh-1',
      project_id: 'proj-1',
      title: 'Fix issue from GitHub',
      prompt: 'Resolve bug reported in issue #42',
      assignee_id: 'agent-1',
      assignee_type: 'agent',
      status: 'review',
      priority: 50,
      skills: [],
      mcp_servers: [],
      external_id: '42',
      external_url: 'https://github.com/my-org/my-repo/issues/42',
      pull_request_url: 'https://github.com/my-org/my-repo/pull/99',
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
    };

    useTaskStore.setState({
      tasks: { [taskWithGithub.id]: taskWithGithub },
      taskIds: [taskWithGithub.id],
    });

    render(<TaskCard id={taskWithGithub.id} />);

    // GH Issue badge
    const ghBadge = screen.getByTestId(`task-gh-issue-${taskWithGithub.id}`);
    expect(ghBadge).toBeInTheDocument();
    expect(ghBadge).toHaveTextContent('GH #42');
    expect(ghBadge).toHaveAttribute('href', 'https://github.com/my-org/my-repo/issues/42');

    // PR badge
    const prBadge = screen.getByTestId(`task-pr-badge-${taskWithGithub.id}`);
    expect(prBadge).toBeInTheDocument();
    expect(prBadge).toHaveTextContent('PR');
    expect(prBadge).toHaveAttribute('href', 'https://github.com/my-org/my-repo/pull/99');
  });

  it('renders Create Pull Request button in TaskInspectorModal when completed and triggers PR creation', async () => {
    const completedTask: TaskDTO = {
      id: 'task-done-1',
      project_id: 'proj-1',
      title: 'Completed Autonomous Feature',
      prompt: 'Implement cool feature',
      assignee_id: 'agent-1',
      assignee_type: 'agent',
      status: 'done',
      priority: 80,
      skills: [],
      mcp_servers: [],
      external_id: '101',
      external_url: 'https://github.com/my-org/my-repo/issues/101',
      pull_request_url: null,
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
    };

    useTaskStore.setState({
      tasks: { [completedTask.id]: completedTask },
      taskIds: [completedTask.id],
      selectedTaskId: completedTask.id,
    });

    const prApiSpy = vi.spyOn(taskApi, 'createTaskPR').mockResolvedValue({
      task_id: completedTask.id,
      pull_request_url: 'https://github.com/my-org/my-repo/pull/777',
    });

    render(<TaskInspectorModal />);

    // Inspect links in header
    expect(screen.getByTestId('inspector-gh-issue-link')).toBeInTheDocument();
    expect(screen.queryByTestId('inspector-pr-link')).not.toBeInTheDocument();

    // Find and click "Create Pull Request" button
    const createPrBtn = screen.getByTestId('create-pr-btn');
    expect(createPrBtn).toBeInTheDocument();
    fireEvent.click(createPrBtn);

    await waitFor(() => {
      expect(prApiSpy).toHaveBeenCalledWith(completedTask.id);
      expect(screen.getByTestId('pr-success-toast')).toBeInTheDocument();
      expect(screen.getByText(/Pull request created successfully!/i)).toBeInTheDocument();
      expect(screen.getByTestId('view-pr-btn')).toBeInTheDocument();
    });

    // Check that store has been updated
    const updatedTask = useTaskStore.getState().tasks[completedTask.id];
    expect(updatedTask.pull_request_url).toBe('https://github.com/my-org/my-repo/pull/777');
  });

  it('renders clean PR Merged state and suppresses review approve/reject buttons when task is done and PR is merged', async () => {
    const mergedTask: TaskDTO = {
      id: 'task-merged-1',
      project_id: 'proj-1',
      title: 'Merged Autonomous Feature',
      prompt: 'Implement cool feature',
      assignee_id: 'agent-1',
      assignee_type: 'agent',
      status: 'done',
      priority: 50,
      skills: [],
      mcp_servers: [],
      external_id: '102',
      external_url: 'https://github.com/my-org/my-repo/issues/102',
      pull_request_url: 'https://github.com/my-org/my-repo/pull/1',
      pr_url: 'https://github.com/my-org/my-repo/pull/1',
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
    };

    useTaskStore.setState({
      tasks: { [mergedTask.id]: mergedTask },
      taskIds: [mergedTask.id],
      selectedTaskId: mergedTask.id,
    });

    vi.spyOn(taskApi, 'getTaskDiff').mockResolvedValue('diff --git a/README.md b/README.md\n+hello');

    render(<TaskInspectorModal />);

    // Header has issue link and status, but not duplicate green PR link
    expect(screen.getByTestId('inspector-gh-issue-link')).toBeInTheDocument();
    expect(screen.queryByTestId('inspector-pr-link')).not.toBeInTheDocument();

    // Workflow bar shows PR Merged
    const prMergedBtn = screen.getByTestId('view-pr-btn');
    expect(prMergedBtn).toBeInTheDocument();
    expect(prMergedBtn).toHaveTextContent(/PR Merged/i);

    // Switch to Git Diff Review tab
    const diffTab = screen.getByRole('tab', { name: /git diff review/i });
    fireEvent.click(diffTab);

    // Approve & Create PR / Reject buttons must NOT be present
    expect(screen.queryByRole('button', { name: /approve/i })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /reject/i })).not.toBeInTheDocument();

    // In diff review, PR Merged badge is displayed
    await waitFor(() => {
      expect(screen.getAllByText(/PR Merged/i).length).toBeGreaterThanOrEqual(1);
    });
  });
});
