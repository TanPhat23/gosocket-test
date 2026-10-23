import React, { useRef, useState } from 'react';
import { taskApi } from '../../api/taskApi';
import { useTaskStore } from '../../store/useTaskStore';
import type { TaskDTO } from '../../schemas/taskSchemas';

export interface PullRequestController {
  isCreatingPR: boolean;
  isSyncingPR: boolean;
  prError: string | null;
  prToast: { message: string; url?: string } | null;
  handleCreatePR: () => Promise<void>;
  handleSyncPR: () => Promise<void>;
}

// usePullRequestController owns the pull request actions of one task: creating the PR, syncing its status, and the
// one-time sync of a review task that already has a PR. It is called once per inspector and shared by context, so the
// header and the diff review bar show the same state.
export function usePullRequestController(task: TaskDTO | null): PullRequestController {
  const updateTask = useTaskStore((state) => state.updateTask);
  const [isCreatingPR, setIsCreatingPR] = useState(false);
  const [prError, setPrError] = useState<string | null>(null);
  const [prToast, setPrToast] = useState<{ message: string; url?: string } | null>(null);
  const [isSyncingPR, setIsSyncingPR] = useState(false);
  const syncedTasksRef = useRef<Set<string>>(new Set());

  const handleSyncPR = React.useCallback(async () => {
    if (!task) return;
    setIsSyncingPR(true);
    try {
      const updated = await taskApi.syncTaskPR(task.id);
      if (updated) {
        updateTask(updated);
        if (updated.status === 'done') {
          setPrToast({
            message: 'Pull request merged! Task marked as completed.',
            url: updated.pull_request_url || updated.pr_url || undefined,
          });
          setTimeout(() => setPrToast(null), 5000);
        }
      }
    } catch {
      // ignore silently if offline or unmerged
    } finally {
      setIsSyncingPR(false);
    }
  }, [task?.id, updateTask]);

  React.useEffect(() => {
    if (
      task &&
      task.status === 'review' &&
      (task.pull_request_url || task.pr_url) &&
      !syncedTasksRef.current.has(task.id)
    ) {
      syncedTasksRef.current.add(task.id);
      handleSyncPR();
    }
  }, [task?.id, task?.status, task?.pull_request_url, task?.pr_url, handleSyncPR]);

  const handleCreatePR = async () => {
    if (!task) return;
    setIsCreatingPR(true);
    setPrError(null);
    try {
      const res = await taskApi.createTaskPR(task.id);
      const updatedPrUrl = res.pull_request_url;
      const updated: typeof task = {
        ...task,
        pull_request_url: updatedPrUrl,
        pr_url: updatedPrUrl,
        updated_at: new Date().toISOString(),
      };
      updateTask(updated);
      setPrToast({
        message: 'Pull request created successfully!',
        url: updatedPrUrl,
      });
      setTimeout(() => {
        setPrToast(null);
      }, 5000);
    } catch (err) {
      setPrError(err instanceof Error ? err.message : 'Failed to create pull request');
    } finally {
      setIsCreatingPR(false);
    }
  };

  return { isCreatingPR, isSyncingPR, prError, prToast, handleCreatePR, handleSyncPR };
}
