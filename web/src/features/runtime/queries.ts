import { queryOptions } from '@tanstack/react-query';
import { runtimeApi } from './api/runtimeApi';

export const LOG_TAIL_LINES = 500;

// The container changes state in the background, so both queries poll while the page is open.
export const runtimeQueryOptions = (projectId: string) =>
  queryOptions({
    queryKey: ['projects', projectId, 'runtime'],
    queryFn: () => runtimeApi.getRuntime(projectId),
    refetchInterval: 5000,
  });

export const runtimeLogsQueryOptions = (projectId: string) =>
  queryOptions({
    queryKey: ['projects', projectId, 'runtime', 'logs'],
    queryFn: () => runtimeApi.getLogs(projectId, LOG_TAIL_LINES),
    refetchInterval: 3000,
  });
