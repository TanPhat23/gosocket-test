import { createFileRoute, redirect } from '@tanstack/react-router';
import { authQueryOptions } from '@/features/auth/queries';
import { LoginForm } from '@/features/auth/components/LoginForm';

export const Route = createFileRoute('/login')({
  beforeLoad: async ({ context }) => {
    const user = await context.queryClient.ensureQueryData(authQueryOptions());
    if (user) {
      throw redirect({ to: '/' as string });
    }
  },
  component: () => (
    <div className="min-h-screen flex items-center justify-center bg-background p-4">
      <LoginForm />
    </div>
  ),
});
