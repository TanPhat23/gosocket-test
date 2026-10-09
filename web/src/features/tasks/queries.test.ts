import { describe, it, expect } from 'vitest';
import { tasksQueryOptions, taskDetailQueryOptions, taskSessionQueryOptions } from './queries';

describe('Task Query Options', () => {
  it('creates query options with correct query keys and stale times', () => {
    const tasksOpts = tasksQueryOptions();
    expect(tasksOpts.queryKey).toEqual(['tasks']);
    expect(tasksOpts.staleTime).toBeGreaterThan(0);

    const detailOpts = taskDetailQueryOptions('task-123');
    expect(detailOpts.queryKey).toEqual(['tasks', 'task-123']);

    const sessionOpts = taskSessionQueryOptions('task-123');
    expect(sessionOpts.queryKey).toEqual(['tasks', 'task-123', 'session']);
  });
});
