import { queryOptions } from '@tanstack/react-query';
import { taskApi } from './api/taskApi';

export const tasksQueryOptions = () =>
  queryOptions({
    queryKey: ['tasks'],
    queryFn: () => taskApi.getTasks(),
    staleTime: 1000 * 30,
  });

export const taskDetailQueryOptions = (taskId: string) =>
  queryOptions({
    queryKey: ['tasks', taskId],
    queryFn: () => taskApi.getTask(taskId),
    staleTime: 1000 * 30,
  });

export const taskSessionQueryOptions = (taskId: string) =>
  queryOptions({
    queryKey: ['tasks', taskId, 'session'],
    queryFn: () => taskApi.getTaskSession(taskId),
    staleTime: 1000 * 5,
  });
