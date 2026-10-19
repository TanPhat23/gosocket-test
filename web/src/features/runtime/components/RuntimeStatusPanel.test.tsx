import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { runtimeApi } from '../api/runtimeApi';
import { RuntimeStatusPanel } from './RuntimeStatusPanel';

function renderPanel() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <RuntimeStatusPanel projectId="p1" />
    </QueryClientProvider>
  );
}

describe('RuntimeStatusPanel', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('shows the uptime and resource use of a running container', async () => {
    vi.spyOn(runtimeApi, 'getRuntime').mockResolvedValue({
      state: 'running',
      container_id: '0123456789ab',
      image: 'atm-runtime:abc',
      started_at: new Date(Date.now() - 125_000).toISOString(),
      cpu_percent: 12.34,
      memory_bytes: 512 * 1024 * 1024,
      memory_limit_bytes: 2 * 1024 ** 3,
    });
    renderPanel();

    expect(await screen.findByTestId('runtime-state')).toHaveTextContent('Running');
    expect(screen.getByTestId('runtime-uptime')).toHaveTextContent('2m 5s');
    expect(screen.getByTestId('runtime-cpu')).toHaveTextContent('12.3%');
    expect(screen.getByTestId('runtime-memory')).toHaveTextContent('512 MB / 2.0 GB');
    expect(screen.getByTestId('runtime-container-id')).toHaveTextContent('0123456789ab');
  });

  it('leaves the numbers out when nothing is running', async () => {
    vi.spyOn(runtimeApi, 'getRuntime').mockResolvedValue({ state: 'absent' });
    renderPanel();

    expect(await screen.findByTestId('runtime-state')).toHaveTextContent('Not created');
    expect(screen.getByTestId('runtime-uptime')).toHaveTextContent('—');
    expect(screen.getByTestId('runtime-cpu')).toHaveTextContent('—');
  });

  it('reports a container state it could not read', async () => {
    vi.spyOn(runtimeApi, 'getRuntime').mockRejectedValue(new Error('daemon down'));
    renderPanel();

    expect(await screen.findByTestId('runtime-status-error')).toHaveTextContent('daemon down');
  });
});
