import { createFileRoute } from '@tanstack/react-router';
import { tasksQueryOptions } from '@/features/tasks/queries';
import { WorkspaceView } from '@/App';

export const Route = createFileRoute('/_auth/')({
  loader: async ({ context }) => {
    await context.queryClient.ensureQueryData(tasksQueryOptions());
  },
  component: () => <WorkspaceView />,
});
