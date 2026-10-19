import { useCallback, useMemo } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { taskSessionQueryOptions } from '../queries';
import type { TaskSessionResponse } from '../schemas/taskSchemas';
import { sessionErrorMessage } from '../utils/sessionFormat';

// The task session from the shared query, so the inspector's metrics and the transcript use one request. Stream
// frames and optimistic messages write into the same cache entry.
export function useSessionQuery(taskId: string) {
  const queryClient = useQueryClient();
  const queryKey = useMemo(() => taskSessionQueryOptions(taskId).queryKey, [taskId]);
  // retry: false keeps the immediate error banner. refetchOnMount: false lets the tab reuse the inspector's fetch.
  const query = useQuery({ ...taskSessionQueryOptions(taskId), retry: false, refetchOnMount: false });
  const session = query.data ?? null;
  const failure = query.error as { status?: number; message?: string } | null;

  // Overlapping refreshes (polling, the terminal stream, prompts) share one request.
  const refresh = useCallback(
    () => queryClient.fetchQuery({ ...taskSessionQueryOptions(taskId), staleTime: 0, retry: false }).catch(() => undefined),
    [queryClient, taskId],
  );

  const updateSession = useCallback(
    (update: (prev: TaskSessionResponse) => TaskSessionResponse) =>
      queryClient.setQueryData<TaskSessionResponse>(queryKey, (prev) => (prev ? update(prev) : prev)),
    [queryClient, queryKey],
  );

  const setSession = useCallback(
    (next: TaskSessionResponse) => queryClient.setQueryData(queryKey, next),
    [queryClient, queryKey],
  );

  return {
    session,
    error: failure ? sessionErrorMessage(failure) : null,
    isUnavailable: failure?.status === 503,
    isSimulated: session?.simulated === true,
    loading: query.isFetching && !session,
    refresh,
    updateSession,
    setSession,
  };
}
