import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { ApiError } from '@/lib/axios';
import { runtimeApi } from '../api/runtimeApi';
import type { RuntimeInfo } from '../schemas/runtimeSchemas';
import { RuntimeControls } from './RuntimeControls';

function renderControls(info: RuntimeInfo, canManage = true) {
  vi.spyOn(runtimeApi, 'getRuntime').mockResolvedValue(info);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <RuntimeControls projectId="p1" canManage={canManage} />
    </QueryClientProvider>
  );
}

describe('RuntimeControls', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('offers start for a stopped container and nothing destructive', async () => {
    renderControls({ state: 'stopped' });

    await waitFor(() => expect(screen.getByTestId('runtime-start-btn')).toBeEnabled());
    expect(screen.getByTestId('runtime-stop-btn')).toBeDisabled();
    expect(screen.getByTestId('runtime-restart-btn')).toBeDisabled();
  });

  it('starts the container', async () => {
    const start = vi.spyOn(runtimeApi, 'start').mockResolvedValue(undefined);
    renderControls({ state: 'absent' });

    await waitFor(() => expect(screen.getByTestId('runtime-start-btn')).toBeEnabled());
    fireEvent.click(screen.getByTestId('runtime-start-btn'));

    await waitFor(() => expect(start).toHaveBeenCalledWith('p1'));
  });

  it('keeps stop and restart from members who are not admins', async () => {
    renderControls({ state: 'running' }, false);

    await waitFor(() => expect(screen.getByTestId('runtime-start-btn')).toBeDisabled());
    expect(screen.getByTestId('runtime-stop-btn')).toBeDisabled();
    expect(screen.getByTestId('runtime-restart-btn')).toBeDisabled();
  });

  it('asks before stopping a busy project, then forces it', async () => {
    const stop = vi
      .spyOn(runtimeApi, 'stop')
      .mockRejectedValueOnce(new ApiError(409, 'tasks are still running'))
      .mockResolvedValueOnce(undefined);
    renderControls({ state: 'running' });

    await waitFor(() => expect(screen.getByTestId('runtime-stop-btn')).toBeEnabled());
    fireEvent.click(screen.getByTestId('runtime-stop-btn'));

    expect(await screen.findByTestId('runtime-busy-warning')).toHaveTextContent('tasks are still running');
    expect(stop).toHaveBeenLastCalledWith('p1', false);

    fireEvent.click(screen.getByTestId('runtime-force-btn'));

    await waitFor(() => expect(stop).toHaveBeenLastCalledWith('p1', true));
    await waitFor(() => expect(screen.queryByTestId('runtime-busy-warning')).not.toBeInTheDocument());
  });

  it('shows any other failure as an error', async () => {
    vi.spyOn(runtimeApi, 'restart').mockRejectedValue(new ApiError(503, 'agent execution service unavailable'));
    renderControls({ state: 'running' });

    await waitFor(() => expect(screen.getByTestId('runtime-restart-btn')).toBeEnabled());
    fireEvent.click(screen.getByTestId('runtime-restart-btn'));

    expect(await screen.findByTestId('runtime-action-error')).toHaveTextContent('unavailable');
    expect(screen.queryByTestId('runtime-busy-warning')).not.toBeInTheDocument();
  });
});
