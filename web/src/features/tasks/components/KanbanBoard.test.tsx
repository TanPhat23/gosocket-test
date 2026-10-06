import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
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

  it('renders all 4 columns: Backlog, Queued & In Progress, Review, Done', () => {
    render(<KanbanBoard />);

    expect(screen.getByRole('heading', { name: /^Backlog$/i })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: /^Queued & In Progress$/i })).toBeInTheDocument();
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
});
