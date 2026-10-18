import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { TaskModal } from './TaskModal';
import { useTaskStore } from '../store/useTaskStore';
import { useTeamStore } from '@/features/teams/store/useTeamStore';
import { integrationApi } from '@/features/integrations/api/integrationApi';
import { credentialApi } from '@/features/teams/api/credentialApi';

describe('TaskModal', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('renders trigger button and opens dialog upon click', async () => {
    render(<TaskModal />);

    const triggerBtn = screen.getByRole('button', { name: /new task/i });
    expect(triggerBtn).toBeInTheDocument();

    fireEvent.click(triggerBtn);
    await screen.findByLabelText(/^Title$/i);

    expect(screen.getByText(/Create task/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/^Title$/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/^Prompt$/i)).toBeInTheDocument();
  });

  it('validates client input with zod and displays errors on empty submission', async () => {
    render(<TaskModal />);

    const triggerBtn = screen.getByRole('button', { name: /new task/i });
    fireEvent.click(triggerBtn);
    await screen.findByLabelText(/^Title$/i);

    const submitBtn = screen.getByRole('button', { name: /dispatch task/i });
    fireEvent.click(submitBtn);

    await waitFor(() => {
      expect(screen.getByText(/Title is required/i)).toBeInTheDocument();
      expect(screen.getByText(/Prompt is required/i)).toBeInTheDocument();
    });
  });

  it('submits valid form data to createTask store action', async () => {
    const createTaskSpy = vi.fn().mockResolvedValue({
      id: 'new-task-1',
      project_id: 'default-project',
      title: 'Valid task title',
      prompt: 'Valid prompt details',
      assignee_id: 'agent-sandbox-1',
      assignee_type: 'agent',
      status: 'backlog',
      priority: 50,
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
      skills: [],
      mcp_servers: [],
    });

    useTaskStore.setState({ createTask: createTaskSpy });

    render(<TaskModal />);

    const triggerBtn = screen.getByRole('button', { name: /new task/i });
    fireEvent.click(triggerBtn);
    await screen.findByLabelText(/^Title$/i);

    const titleInput = screen.getByLabelText(/^Title$/i);
    const promptInput = screen.getByLabelText(/^Prompt$/i);

    fireEvent.change(titleInput, { target: { value: 'Valid task title' } });
    fireEvent.change(promptInput, { target: { value: 'Valid prompt details' } });

    const submitBtn = screen.getByRole('button', { name: /dispatch task/i });
    fireEvent.click(submitBtn);

    await waitFor(() => {
      expect(createTaskSpy).toHaveBeenCalledTimes(1);
    });
  });

  it('allows selecting target repository when project has connected repositories', async () => {
    const createTaskSpy = vi.fn().mockResolvedValue({
      id: 'task-with-repo',
      project_id: 'default-project',
      integration_id: 'integ-repo-2',
      title: 'Task on second repo',
      prompt: 'Do work on repo 2',
      assignee_id: 'agent-sandbox-1',
      assignee_type: 'agent',
      status: 'backlog',
      priority: 50,
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
      skills: [],
      mcp_servers: [],
    });

    useTaskStore.setState({ createTask: createTaskSpy });

    render(<TaskModal />);

    const triggerBtn = screen.getByRole('button', { name: /new task/i });
    fireEvent.click(triggerBtn);
    await screen.findByLabelText(/^Title$/i);

    const titleInput = screen.getByLabelText(/^Title$/i);
    const promptInput = screen.getByLabelText(/^Prompt$/i);

    fireEvent.change(titleInput, { target: { value: 'Task on second repo' } });
    fireEvent.change(promptInput, { target: { value: 'Do work on repo 2' } });

    const submitBtn = screen.getByRole('button', { name: /dispatch task/i });
    fireEvent.click(submitBtn);

    await waitFor(() => {
      expect(createTaskSpy).toHaveBeenCalled();
    });
  });

  it('fetches branches for connected repository and allows selecting branch', async () => {
    useTeamStore.setState({
      activeProjectId: 'proj-1',
      activeTeamId: 'team-1',
      projects: [{ id: 'proj-1', team_id: 'team-1', name: 'Project 1' }],
    });

    vi.spyOn(integrationApi, 'listProjectIntegrations').mockResolvedValue([
      {
        id: 'integ-1',
        project_id: 'proj-1',
        credential_id: 'cred-1',
        provider: 'github',
        external_id: 'org/repo-demo',
        external_url: 'https://github.com/org/repo-demo',
        enabled: true,
        config: { base_branch: 'main' },
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
      },
    ]);

    const branchSpy = vi.spyOn(credentialApi, 'listGitHubBranches').mockResolvedValue([
      'main',
      'feature/branch-x',
      'develop',
    ]);

    render(<TaskModal />);

    fireEvent.click(screen.getByRole('button', { name: /new task/i }));
    await screen.findByLabelText(/^Title$/i);

    await waitFor(() => {
      expect(screen.getByText('org/repo-demo')).toBeInTheDocument();
      expect(branchSpy).toHaveBeenCalledWith('team-1', 'cred-1', 'org/repo-demo');
    });

    expect(screen.getByTestId('target-branch-select')).toBeInTheDocument();
  });
});
