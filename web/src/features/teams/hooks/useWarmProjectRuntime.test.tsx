import { describe, it, expect, vi, afterEach } from 'vitest';
import { renderHook } from '@testing-library/react';
import { teamApi } from '../api/teamApi';
import { useWarmProjectRuntime } from './useWarmProjectRuntime';

describe('useWarmProjectRuntime', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('starts the runtime of the project when it becomes active', () => {
    const start = vi.spyOn(teamApi, 'startProjectRuntime').mockResolvedValue(undefined);

    renderHook(({ id }) => useWarmProjectRuntime(id), { initialProps: { id: 'proj-1' as string | null } });

    expect(start).toHaveBeenCalledWith('proj-1');
  });

  it('does nothing without an active project', () => {
    const start = vi.spyOn(teamApi, 'startProjectRuntime').mockResolvedValue(undefined);

    renderHook(() => useWarmProjectRuntime(null));

    expect(start).not.toHaveBeenCalled();
  });

  it('starts again when the active project changes', () => {
    const start = vi.spyOn(teamApi, 'startProjectRuntime').mockResolvedValue(undefined);

    const { rerender } = renderHook(({ id }) => useWarmProjectRuntime(id), {
      initialProps: { id: 'proj-1' as string | null },
    });
    rerender({ id: 'proj-2' });

    expect(start).toHaveBeenNthCalledWith(1, 'proj-1');
    expect(start).toHaveBeenNthCalledWith(2, 'proj-2');
  });
});
