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

// The diff and the file explorer re-read the workspace, which changes while an agent works. They are cached briefly
// so switching tabs or revisiting a file does not hit the server again.
export const taskDiffQueryOptions = (taskId: string) =>
  queryOptions({
    queryKey: ['tasks', taskId, 'diff'],
    queryFn: () => taskApi.getTaskDiff(taskId),
    staleTime: 1000 * 15,
  });

export const taskFilesQueryOptions = (taskId: string) =>
  queryOptions({
    queryKey: ['tasks', taskId, 'files'],
    queryFn: () => taskApi.getTaskFiles(taskId),
    staleTime: 1000 * 15,
  });

export const taskFileQueryOptions = (taskId: string, path: string) =>
  queryOptions({
    queryKey: ['tasks', taskId, 'file', path],
    queryFn: () => taskApi.getTaskFile(taskId, path),
    staleTime: 1000 * 15,
  });
