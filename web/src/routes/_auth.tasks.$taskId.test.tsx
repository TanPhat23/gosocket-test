import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { Route } from './_auth.tasks.$taskId';
import type { QueryClient } from '@tanstack/react-query';

const mockNavigate = vi.fn();

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>();
  return {
    ...actual,
    useNavigate: () => mockNavigate,
  };
});

vi.mock('@/App', () => ({
  WorkspaceView: () => <div data-testid="workspace-view">Mocked WorkspaceView</div>,
}));

vi.mock('@/features/tasks/components/TaskInspectorModal', () => ({
  TaskInspectorModal: ({ taskId, onClose }: { taskId?: string | null; onClose?: () => void }) => (
    <div data-testid="task-inspector-modal">
      <span data-testid="modal-task-id">{taskId}</span>
      <button data-testid="modal-close-button" onClick={onClose}>
        Close Modal
      </button>
    </div>
  ),
}));

describe('/_auth.tasks.$taskId route', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  describe('loader', () => {
    it('calls ensureQueryData for tasks, taskDetail, and taskSession', async () => {
      const mockQueryClient = {
        ensureQueryData: vi.fn().mockResolvedValue({}),
      } as unknown as QueryClient;

      const loader = Route.options.loader;
      expect(loader).toBeDefined();

      await loader!({
        params: { taskId: 'task-test-456' },
        context: { queryClient: mockQueryClient },
      } as any);

      expect(mockQueryClient.ensureQueryData).toHaveBeenCalledTimes(3);

      const calls = (mockQueryClient.ensureQueryData as any).mock.calls;
      const queryKeys = calls.map((call: any[]) => call[0].queryKey);

      expect(queryKeys).toContainEqual(['tasks']);
      expect(queryKeys).toContainEqual(['tasks', 'task-test-456']);
      expect(queryKeys).toContainEqual(['tasks', 'task-test-456', 'session']);
    });
  });

  describe('component', () => {
    it('renders WorkspaceView and TaskInspectorModal with taskId, and closing navigates back to /', () => {
      // Stub Route.useParams
      vi.spyOn(Route, 'useParams').mockReturnValue({ taskId: 'task-789' } as any);

      const Component = Route.options.component!;
      render(<Component />);

      expect(screen.getByTestId('workspace-view')).toBeInTheDocument();
      expect(screen.getByTestId('task-inspector-modal')).toBeInTheDocument();
      expect(screen.getByTestId('modal-task-id')).toHaveTextContent('task-789');

      fireEvent.click(screen.getByTestId('modal-close-button'));
      expect(mockNavigate).toHaveBeenCalledTimes(1);
      expect(mockNavigate).toHaveBeenCalledWith({ to: '/' });
    });
  });
});
