import React from 'react';
import { useQuery } from '@tanstack/react-query';
import { taskDiffQueryOptions } from '../../queries';
import { DiffViewer } from '../DiffViewer';
import { useInspectedTask, usePullRequest } from './TaskInspectorContext';

// DiffTab loads the task's diff and shows it with the review actions. It reads the task and the pull request actions
// from context, so the inspector does not pass them down, and it re-renders only when the task or the PR state change.
export const DiffTab: React.FC = () => {
  const task = useInspectedTask();
  const pullRequest = usePullRequest();
  const diffQuery = useQuery({ ...taskDiffQueryOptions(task.id), enabled: !!task.id });
  const prLink = task.pull_request_url || task.pr_url || null;
  const canReview = task.status === 'review' && !prLink;

  return (
    <DiffViewer
      diff={diffQuery.data ?? ''}
      loading={diffQuery.isPending}
      prUrl={prLink}
      isMerged={task.status === 'done'}
      onApprove={canReview ? pullRequest.handleCreatePR : undefined}
      onReject={
        canReview
          ? () => {
              alert(`Rejected changes for task ${task.id}. Prompting agent for fixes.`);
            }
          : undefined
      }
    />
  );
};
