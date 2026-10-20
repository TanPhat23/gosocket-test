import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { KanbanBoard } from './KanbanBoard';
import { useTaskStore } from '../store/useTaskStore';

describe('KanbanBoard', () => {
  beforeEach(() => {
    useTaskStore.setState({
      tasks: {},
      taskIds: [],
      selectedTaskId: null,
      isLoading: false,
      error: null,
    });
  });

  it('renders all 5 columns: Backlog, Queued, In Progress, Review, Done', () => {
    render(<KanbanBoard />);

    expect(screen.getByRole('heading', { name: /^Backlog$/i })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: /^Queued$/i })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: /^In Progress$/i })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: /^Review$/i })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: /^Done$/i })).toBeInTheDocument();
  });

  it('renders tasks correctly inside their respective columns', () => {
    useTaskStore.setState({
      tasks: {
        'task-1': {
          id: 'task-1',
          project_id: 'proj-1',
          title: 'Backlog item 1',
          prompt: 'Execute step 1',
          assignee_id: 'agent-1',
          assignee_type: 'agent',
          status: 'backlog',
          priority: 50,
          created_at: new Date().toISOString(),
          updated_at: new Date().toISOString(),
          skills: [],
          mcp_servers: [],
        },
        'task-2': {
          id: 'task-2',
          project_id: 'proj-1',
          title: 'Active item 2',
          prompt: 'Execute step 2',
          assignee_id: 'agent-1',
          assignee_type: 'agent',
          status: 'in_progress',
          priority: 85,
          created_at: new Date().toISOString(),
          updated_at: new Date().toISOString(),
          skills: ['frontend-ui-engineering'],
          mcp_servers: ['github'],
        },
      },
      taskIds: ['task-1', 'task-2'],
    });

    render(<KanbanBoard />);

    expect(screen.getByText('Backlog item 1')).toBeInTheDocument();
    expect(screen.getByText('Active item 2')).toBeInTheDocument();
  });

  it('preserves card click opening full inspector and preview button opening side peek', async () => {
    useTaskStore.setState({
      tasks: {
        'task-1': {
          id: 'task-1',
          project_id: 'proj-1',
          title: 'Backlog item 1',
          prompt: 'Execute step 1',
          assignee_id: 'agent-1',
          assignee_type: 'agent',
          status: 'backlog',
          priority: 50,
          created_at: new Date().toISOString(),
          updated_at: new Date().toISOString(),
          skills: [],
          mcp_servers: [],
        },
      },
      taskIds: ['task-1'],
    });

    render(<KanbanBoard />);

    const card = screen.getByTestId('task-card-task-1');
    const previewBtn = screen.getByRole('button', { name: /preview task #task-1/i });

    // Preview button click opens peek without selecting task in store
    previewBtn.click();
    expect(useTaskStore.getState().selectedTaskId).toBeNull();
    expect(await screen.findByRole('dialog')).toBeInTheDocument();

    // Card click selects task in store
    card.click();
    expect(useTaskStore.getState().selectedTaskId).toBe('task-1');
  });

  it('passes filtered lane task order to the peek navigation', async () => {
    useTaskStore.setState({
      tasks: {
        'task-1': {
          id: 'task-1',
          project_id: 'proj-1',
          title: 'Backlog item 1',
          prompt: 'Execute step 1',
          assignee_id: 'agent-1',
          assignee_type: 'agent',
          status: 'backlog',
          priority: 50,
          created_at: new Date().toISOString(),
          updated_at: new Date().toISOString(),
          skills: [],
          mcp_servers: [],
        },
        'task-2': {
          id: 'task-2',
          project_id: 'proj-1',
          title: 'Backlog item 2',
          prompt: 'Execute step 2',
          assignee_id: 'agent-1',
          assignee_type: 'agent',
          status: 'backlog',
          priority: 50,
          created_at: new Date().toISOString(),
          updated_at: new Date().toISOString(),
          skills: [],
          mcp_servers: [],
        },
        'task-3': {
          id: 'task-3',
          project_id: 'proj-1',
          title: 'In progress item 3',
          prompt: 'Execute step 3',
          assignee_id: 'agent-1',
          assignee_type: 'agent',
          status: 'in_progress',
          priority: 50,
          created_at: new Date().toISOString(),
          updated_at: new Date().toISOString(),
          skills: [],
          mcp_servers: [],
        },
      },
      taskIds: ['task-1', 'task-2', 'task-3'],
    });

    render(<KanbanBoard />);

    const previewBtn1 = screen.getByRole('button', { name: /preview task #task-1/i });
    fireEvent.click(previewBtn1);

    expect(await screen.findByRole('dialog')).toBeInTheDocument();
    expect(screen.getByText('1 of 2')).toBeInTheDocument();

    const nextBtn = screen.getByRole('button', { name: /next task/i });
    fireEvent.click(nextBtn);

    expect(await screen.findByText('2 of 2')).toBeInTheDocument();
  });
});
