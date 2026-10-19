import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { ApiError } from '@/lib/axios';
import { runtimeApi } from '../api/runtimeApi';

export type RuntimeAction = 'start' | 'stop' | 'restart';

// The pending action that the server refused because tasks are still running, until the user forces or drops it.
export interface BusyRefusal {
  action: 'stop' | 'restart';
  message: string;
}

// Runs start, stop and restart for one project. A stop or restart that hits 409 is held as `busy` so the page can
// ask the user to confirm and call `force`.
export function useRuntimeActions(projectId: string) {
  const queryClient = useQueryClient();
  const [busy, setBusy] = useState<BusyRefusal | null>(null);
  const [error, setError] = useState<string | null>(null);

  const mutation = useMutation({
    mutationFn: ({ action, force }: { action: RuntimeAction; force: boolean }) => {
      if (action === 'start') return runtimeApi.start(projectId);
      return action === 'stop' ? runtimeApi.stop(projectId, force) : runtimeApi.restart(projectId, force);
    },
    onMutate: () => {
      setBusy(null);
      setError(null);
    },
    onError: (err, { action }) => {
      if (err instanceof ApiError && err.status === 409 && action !== 'start') {
        setBusy({ action, message: err.message });
        return;
      }
      setError(err instanceof Error ? err.message : 'The request failed');
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: ['projects', projectId, 'runtime'] }),
  });

  return {
    pending: mutation.isPending ? mutation.variables.action : null,
    busy,
    error,
    run: (action: RuntimeAction) => mutation.mutate({ action, force: false }),
    force: () => busy && mutation.mutate({ action: busy.action, force: true }),
    dismissBusy: () => setBusy(null),
  };
}
