import React, { createContext, useContext } from 'react';
import type { TaskDTO } from '../../schemas/taskSchemas';
import type { PullRequestController } from './usePullRequestController';

// The inspector shares its task and pull request actions through context instead of passing them down each tab.
const TaskContext = createContext<TaskDTO | null>(null);
const PullRequestContext = createContext<PullRequestController | null>(null);

export function TaskInspectorProvider({
  task,
  pullRequest,
  children,
}: Readonly<{
  task: TaskDTO;
  pullRequest: PullRequestController;
  children: React.ReactNode;
}>) {
  return (
    <TaskContext.Provider value={task}>
      <PullRequestContext.Provider value={pullRequest}>{children}</PullRequestContext.Provider>
    </TaskContext.Provider>
  );
}

export function useInspectedTask(): TaskDTO {
  const task = useContext(TaskContext);
  if (!task) throw new Error('useInspectedTask must be used inside TaskInspectorProvider');
  return task;
}

export function usePullRequest(): PullRequestController {
  const pullRequest = useContext(PullRequestContext);
  if (!pullRequest) throw new Error('usePullRequest must be used inside TaskInspectorProvider');
  return pullRequest;
}
