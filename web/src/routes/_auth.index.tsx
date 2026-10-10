import { createFileRoute } from '@tanstack/react-router';
import { tasksQueryOptions } from '@/features/tasks/queries';
import { teamsQueryOptions } from '@/features/teams/queries';
import { WorkspaceView } from '@/App';

export const Route = createFileRoute('/_auth/')({
  loader: async ({ context }) => {
    await Promise.all([
      context.queryClient.ensureQueryData(teamsQueryOptions()),
      context.queryClient.ensureQueryData(tasksQueryOptions()),
    ]);
  },
  component: () => <WorkspaceView />,
});
