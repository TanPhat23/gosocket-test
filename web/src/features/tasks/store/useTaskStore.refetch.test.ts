import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { useTaskStore } from './useTaskStore';
import { taskApi } from '../api/taskApi';
import type { TaskDTO } from '../schemas/taskSchemas';

function makeTask(id: string, status = 'queued'): TaskDTO {
  return {
    id,
    title: `Task ${id}`,
    prompt: 'p',
    status,
    priority: 50,
    project_id: 'project-1',
    created_at: '2026-10-01T00:00:00Z',
    updated_at: '2026-10-01T00:00:00Z',
  } as unknown as TaskDTO;
}

// The poll refetches the whole list. Unchanged tasks must keep their object identity so subscribed cards do not
// re-render on every poll.
describe('useTaskStore refetch identity', () => {
  beforeEach(() => {
    useTaskStore.setState({ tasks: {}, taskIds: [] });
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('keeps the same record and task objects when a refetch returns identical data', async () => {
    vi.spyOn(taskApi, 'getTasks').mockResolvedValue([makeTask('a'), makeTask('b')]);
    await useTaskStore.getState().fetchTasks();
    const recordBefore = useTaskStore.getState().tasks;
    const aBefore = recordBefore.a;

    vi.spyOn(taskApi, 'getTasks').mockResolvedValue([makeTask('a'), makeTask('b')]);
    await useTaskStore.getState().fetchTasks();

    expect(useTaskStore.getState().tasks).toBe(recordBefore);
    expect(useTaskStore.getState().tasks.a).toBe(aBefore);
    expect(useTaskStore.getState().isLoading).toBe(false);
  });

  it('replaces only the task that changed', async () => {
    vi.spyOn(taskApi, 'getTasks').mockResolvedValue([makeTask('a'), makeTask('b')]);
    await useTaskStore.getState().fetchTasks();
    const { tasks: before } = useTaskStore.getState();

    vi.spyOn(taskApi, 'getTasks').mockResolvedValue([makeTask('a'), makeTask('b', 'in_progress')]);
    await useTaskStore.getState().fetchTasks();
    const after = useTaskStore.getState();

    expect(after.tasks).not.toBe(before);
    expect(after.tasks.a).toBe(before.a);
    expect(after.tasks.b).not.toBe(before.b);
    expect(after.tasks.b.status).toBe('in_progress');
    expect(after.taskIds).toEqual(['a', 'b']);
  });

  it('reports a change when a task is added or removed', async () => {
    vi.spyOn(taskApi, 'getTasks').mockResolvedValue([makeTask('a')]);
    await useTaskStore.getState().fetchTasks();
    const before = useTaskStore.getState().tasks;

    vi.spyOn(taskApi, 'getTasks').mockResolvedValue([makeTask('a'), makeTask('c')]);
    await useTaskStore.getState().fetchTasks();

    expect(useTaskStore.getState().taskIds).toEqual(['a', 'c']);
    expect(useTaskStore.getState().tasks).not.toBe(before);
    expect(Object.keys(useTaskStore.getState().tasks)).toEqual(['a', 'c']);
  });
});
