import { createFileRoute } from '@tanstack/react-router';
import { teamsQueryOptions } from '@/features/teams/queries';
import { runtimeQueryOptions } from '@/features/runtime/queries';
import { RuntimePage } from '@/features/runtime/components/RuntimePage';

export const Route = createFileRoute('/_auth/projects/$projectId/runtime')({
  loader: async ({ params, context }) => {
    await Promise.all([
      context.queryClient.ensureQueryData(teamsQueryOptions()),
      // Prefetch only: the page shows an unreadable container itself rather than failing the route.
      context.queryClient.prefetchQuery(runtimeQueryOptions(params.projectId)),
    ]);
  },
  component: RuntimeRouteComponent,
});

function RuntimeRouteComponent() {
  const { projectId } = Route.useParams();
  return <RuntimePage projectId={projectId} />;
}
