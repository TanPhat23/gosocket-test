import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { useState } from 'react';
import type { TaskDTO } from '../../schemas/taskSchemas';
import { TaskInspectorProvider } from './TaskInspectorContext';
import type { PullRequestController } from './usePullRequestController';
import { DiffTab } from './DiffTab';

// The viewer counts its own renders, so the test can show whether the tab re-rendered it.
vi.mock('../DiffViewer', () => {
  const counter = { renders: 0 };
  (globalThis as { diffViewerCounter?: typeof counter }).diffViewerCounter = counter;
  return {
    DiffViewer: () => {
      counter.renders += 1;
      return null;
    },
  };
});

vi.mock('../../api/taskApi', () => ({
  taskApi: {
    getTaskDiff: vi.fn().mockResolvedValue('diff --git a/a.ts b/a.ts\n+x\n'),
  },
}));

const counter = () => (globalThis as unknown as { diffViewerCounter: { renders: number } }).diffViewerCounter;

const task = {
  id: 'task-1',
  title: 'Diff',
  status: 'in_progress',
  priority: 50,
} as unknown as TaskDTO;

// Stable across renders, as the modal provides it.
const pullRequest: PullRequestController = {
  isCreatingPR: false,
  isSyncingPR: false,
  prError: null,
  prToast: null,
  handleCreatePR: async () => {},
  handleSyncPR: async () => {},
};

function Harness() {
  const [bumps, setBumps] = useState(0);
  return (
    <>
      <button onClick={() => setBumps((n) => n + 1)}>bump {bumps}</button>
      <TaskInspectorProvider task={task} pullRequest={pullRequest}>
        <DiffTab />
      </TaskInspectorProvider>
    </>
  );
}

describe('DiffTab', () => {
  it('does not re-render the diff viewer when unrelated state above it changes', async () => {
    render(<Harness />);
    await waitFor(() => expect(counter().renders).toBeGreaterThan(0));
    const before = counter().renders;

    fireEvent.click(screen.getByRole('button', { name: /bump/ }));
    fireEvent.click(screen.getByRole('button', { name: /bump/ }));

    expect(screen.getByRole('button', { name: 'bump 2' })).toBeInTheDocument();
    expect(counter().renders).toBe(before);
  });
});
