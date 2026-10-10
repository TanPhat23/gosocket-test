import { queryOptions } from '@tanstack/react-query';
import { taskApi } from './api/taskApi';

export const modelsQueryOptions = () =>
  queryOptions({
    queryKey: ['models'],
    queryFn: () => taskApi.getModels(),
    staleTime: 1000 * 60 * 5,
  });

export const tasksQueryOptions = (projectId?: string) =>
  queryOptions({
    queryKey: projectId ? ['tasks', projectId] : ['tasks'],
    queryFn: () => taskApi.getTasks(projectId),
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
