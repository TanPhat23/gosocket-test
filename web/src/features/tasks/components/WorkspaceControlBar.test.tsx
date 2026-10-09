import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { WorkspaceControlBar } from './WorkspaceControlBar';
import { useWorkspaceSettingsStore } from '../store/useWorkspaceSettingsStore';
import { useTeamStore } from '@/features/teams/store/useTeamStore';
import { useTaskStore } from '../store/useTaskStore';

describe('WorkspaceControlBar', () => {
  beforeEach(() => {
    vi.clearAllMocks();

    useWorkspaceSettingsStore.setState({
      defaultModel: 'google/gemini-3.8-flash',
      defaultPriority: 50,
      searchQuery: '',
      priorityFilter: 'all',
      availableModels: [
        { id: 'google/gemini-3.8-flash', name: 'Gemini 3.8 Flash', provider: 'google' },
        { id: 'anthropic/claude-sonnet-4-5', name: 'Claude Sonnet 4.5', provider: 'anthropic' },
      ],
    });

    useTeamStore.setState({
      projects: [
        { id: 'proj-1', name: 'Project Alpha', team_id: 'team-1', created_at: '', updated_at: '' },
        { id: 'proj-2', name: 'Project Beta', team_id: 'team-1', created_at: '', updated_at: '' },
      ],
      activeProjectId: null,
      teams: [],
      activeTeamId: null,
      isLoading: false,
      error: null,
      setActiveProjectId: (id: string | null) => useTeamStore.setState({ activeProjectId: id }),
    });

    useTaskStore.setState({
      tasks: {
        'task-1': {
          id: 'task-1',
          project_id: 'proj-1',
          title: 'Implement WebSockets',
          prompt: 'Add real-time notifications',
          assignee_id: 'agent-1',
          assignee_type: 'agent',
          status: 'in_progress',
          priority: 80,
          created_at: '',
          updated_at: '',
          skills: [],
          mcp_servers: [],
        },
        'task-2': {
          id: 'task-2',
          project_id: 'proj-2',
          title: 'Fix database migration',
          prompt: 'Upgrade schema to v2',
          assignee_id: 'agent-1',
          assignee_type: 'agent',
          status: 'queued',
          priority: 50,
          created_at: '',
          updated_at: '',
          skills: [],
          mcp_servers: [],
        },
      },
      taskIds: ['task-1', 'task-2'],
    });
  });

  it('renders control bar with heading, project filter, model selector, search, and priority', () => {
    render(<WorkspaceControlBar />);

    expect(screen.getByRole('heading', { name: /Agent Control Plane/i })).toBeInTheDocument();
    expect(screen.getByLabelText(/filter by project/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/default ai model/i)).toBeInTheDocument();
    expect(screen.getByPlaceholderText(/filter tasks by title/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/filter by priority/i)).toBeInTheDocument();
    expect(screen.getByText('2 tasks')).toBeInTheDocument();
  });

  it('updates active project when selecting a project from the toolbar', async () => {
    render(<WorkspaceControlBar />);

    const projectTrigger = screen.getByLabelText(/filter by project/i);
    expect(projectTrigger).toBeInTheDocument();
    fireEvent.click(projectTrigger);

    const option = await screen.findByText('Project Alpha');
    fireEvent.pointerDown(option);
    fireEvent.pointerUp(option);
    fireEvent.click(option);

    expect(useTeamStore.getState().activeProjectId).toBe('proj-1');
  });

  it('updates default AI model when selecting a model from the toolbar', async () => {
    render(<WorkspaceControlBar />);

    const modelTrigger = screen.getByLabelText(/default ai model/i);
    expect(modelTrigger).toBeInTheDocument();
    fireEvent.click(modelTrigger);

    const option = await screen.findByText('Claude Sonnet 4.5');
    fireEvent.pointerDown(option);
    fireEvent.pointerUp(option);
    fireEvent.click(option);

    expect(useWorkspaceSettingsStore.getState().defaultModel).toBe('anthropic/claude-sonnet-4-5');
  });

  it('filters task count and displays active filter badge when typing search query', () => {
    render(<WorkspaceControlBar />);

    const searchInput = screen.getByPlaceholderText(/filter tasks by title/i);
    fireEvent.change(searchInput, { target: { value: 'WebSockets' } });

    expect(useWorkspaceSettingsStore.getState().searchQuery).toBe('WebSockets');
    expect(screen.getByText('1 task')).toBeInTheDocument();
    expect(screen.getByText(/Query: "WebSockets"/i)).toBeInTheDocument();

    const resetBtn = screen.getByRole('button', { name: /reset filters/i });
    fireEvent.click(resetBtn);

    expect(useWorkspaceSettingsStore.getState().searchQuery).toBe('');
  });
});
