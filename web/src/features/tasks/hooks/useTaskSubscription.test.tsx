import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import type { ReactNode } from 'react';
import { renderHook, act } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { useTaskSubscription } from './useTaskSubscription';
import { taskApi } from '../api/taskApi';

class FakeSocket {
  static instances: FakeSocket[] = [];
  onopen: (() => void) | null = null;
  onmessage: ((e: { data: string }) => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;
  close = vi.fn();
  constructor(public url: string) {
    FakeSocket.instances.push(this);
  }
}

describe('useTaskSubscription', () => {
  beforeEach(() => {
    FakeSocket.instances = [];
    vi.stubGlobal('WebSocket', FakeSocket);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('on socket open invalidates the task query and does not also fetch the store directly', async () => {
    const getTasks = vi.spyOn(taskApi, 'getTasks').mockResolvedValue([]);
    const queryClient = new QueryClient();
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries');
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    );

    const { unmount } = renderHook(() => useTaskSubscription(), { wrapper });
    const socket = FakeSocket.instances.find((s) => s.url.endsWith('/api/v1/ws/events'));
    expect(socket).toBeDefined();

    act(() => {
      socket!.onopen?.();
    });

    expect(invalidate).toHaveBeenCalledWith({ queryKey: ['tasks'] });
    // No active task query is mounted here, so the store fetch was the only path that could hit the API.
    expect(getTasks).not.toHaveBeenCalled();

    unmount();
    expect(socket!.close).toHaveBeenCalled();
  });
});
