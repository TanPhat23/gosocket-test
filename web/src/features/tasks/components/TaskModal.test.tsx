import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { TaskModal } from './TaskModal';
import { useTaskStore } from '../store/useTaskStore';

describe('TaskModal', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('renders trigger button and opens dialog upon click', async () => {
    render(<TaskModal />);

    const triggerBtn = screen.getByRole('button', { name: /new task/i });
    expect(triggerBtn).toBeInTheDocument();

    fireEvent.click(triggerBtn);

    expect(screen.getByText(/Create Autonomous Agent Task/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/Task Title/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/Prompt & Execution Specification/i)).toBeInTheDocument();
  });

  it('validates client input with zod and displays errors on empty submission', async () => {
    render(<TaskModal />);

    const triggerBtn = screen.getByRole('button', { name: /new task/i });
    fireEvent.click(triggerBtn);

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

    const titleInput = screen.getByLabelText(/Task Title/i);
    const promptInput = screen.getByLabelText(/Prompt & Execution Specification/i);

    fireEvent.change(titleInput, { target: { value: 'Valid task title' } });
    fireEvent.change(promptInput, { target: { value: 'Valid prompt details' } });

    const submitBtn = screen.getByRole('button', { name: /dispatch task/i });
    fireEvent.click(submitBtn);

    await waitFor(() => {
      expect(createTaskSpy).toHaveBeenCalledTimes(1);
    });
  });
});
