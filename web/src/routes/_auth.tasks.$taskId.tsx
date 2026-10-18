import { lazy, Suspense } from 'react';
import { createFileRoute, useNavigate } from '@tanstack/react-router';
import { tasksQueryOptions, taskDetailQueryOptions, taskSessionQueryOptions } from '@/features/tasks/queries';
import { WorkspaceView } from '@/App';

// Split out of the workspace bundle, like the inspector in App.tsx.
const TaskInspectorModal = lazy(() =>
  import('@/features/tasks/components/TaskInspectorModal').then((m) => ({ default: m.TaskInspectorModal }))
);

export const Route = createFileRoute('/_auth/tasks/$taskId')({
  loader: async ({ params, context }) => {
    await Promise.all([
      context.queryClient.ensureQueryData(tasksQueryOptions()),
      context.queryClient.ensureQueryData(taskDetailQueryOptions(params.taskId)),
      context.queryClient.ensureQueryData(taskSessionQueryOptions(params.taskId)),
    ]);
  },
  component: TaskDetailRouteComponent,
});

function TaskDetailRouteComponent() {
  const { taskId } = Route.useParams();
  const navigate = useNavigate();

  const handleClose = () => {
    navigate({ to: '/' });
  };

  return (
    <>
      <WorkspaceView />
      <Suspense fallback={null}>
        <TaskInspectorModal taskId={taskId} onClose={handleClose} />
      </Suspense>
    </>
  );
}
