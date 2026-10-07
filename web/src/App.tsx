import React, { useEffect } from 'react';
import { useAuthStore } from '@/features/auth/store/useAuthStore';
import { useTaskStore } from '@/features/tasks/store/useTaskStore';
import { useTeamStore } from '@/features/teams/store/useTeamStore';
import { useTaskSubscription } from '@/features/tasks/hooks/useTaskSubscription';
import { Header } from '@/components/layout/Header';
import { Footer } from '@/components/layout/Footer';
import { TaskMetricsGrid } from '@/features/tasks/components/TaskMetricsGrid';
import { KanbanBoard } from '@/features/tasks/components/KanbanBoard';
import { TaskModal } from '@/features/tasks/components/TaskModal';
import { TaskInspectorModal } from '@/features/tasks/components/TaskInspectorModal';
import { Loader2 } from 'lucide-react';

export const WorkspaceView: React.FC = () => {
  const fetchTasks = useTaskStore((state) => state.fetchTasks);
  const fetchTeams = useTeamStore((state) => state.fetchTeams);

  // Maintain real-time task status updates across the Kanban board
  useTaskSubscription();

  useEffect(() => {
    fetchTasks();
    fetchTeams();
  }, [fetchTasks, fetchTeams]);

  return (
    <div className="min-h-screen bg-background text-foreground flex flex-col font-sans">
      <Header />

      {/* Main Workspace */}
      <main className="flex-1 max-w-7xl w-full mx-auto p-6 flex flex-col gap-6">
        {/* Top Control Bar with Metrics and New Task */}
        <div className="flex flex-col gap-4">
          <div className="flex items-center justify-between">
            <div>
              <h2 className="text-lg font-bold tracking-tight">Agent Control Plane</h2>
              <p className="text-xs text-muted-foreground">
                Autonomous task execution, isolated worktrees, and streaming agent diagnostics.
              </p>
            </div>
            <TaskModal />
          </div>

          <TaskMetricsGrid />
        </div>

        {/* 4-Lane Kanban Board */}
        <section className="flex-1 flex flex-col gap-3">
          <KanbanBoard />
        </section>

        {/* Task Inspector Modal (opens when selectedTaskId is set) */}
        <TaskInspectorModal />
      </main>

      <Footer />
    </div>
  );
};

export const App: React.FC = () => {
  const user = useAuthStore((state) => state.user);
  const isLoading = useAuthStore((state) => state.isLoading);
  const isInitialized = useAuthStore((state) => state.isInitialized);
  const fetchUser = useAuthStore((state) => state.fetchUser);

  useEffect(() => {
    fetchUser();
  }, [fetchUser]);

  // Unauthenticated: auto-redirect to SSO gateway
  useEffect(() => {
    if (isInitialized && !user) {
      window.location.href = '/api/v1/auth/login';
    }
  }, [isInitialized, user]);

  // Initial session check in progress
  if (!isInitialized || (isLoading && !user)) {
    return (
      <div className="min-h-screen bg-background text-foreground flex flex-col items-center justify-center p-4">
        <div className="flex flex-col items-center gap-3 text-center" role="status" aria-live="polite">
          <Loader2 className="size-5 animate-spin text-muted-foreground" aria-hidden="true" />
          <p className="text-sm font-medium text-foreground tracking-tight">
            Verifying session...
          </p>
        </div>
      </div>
    );
  }

  // Session check resolved without user; redirecting to SSO
  if (!user) {
    return (
      <div className="min-h-screen bg-background text-foreground flex flex-col items-center justify-center p-4">
        <div className="flex flex-col items-center gap-3 text-center" role="status" aria-live="polite">
          <Loader2 className="size-5 animate-spin text-muted-foreground" aria-hidden="true" />
          <p className="text-sm font-medium text-foreground tracking-tight">
            Redirecting to sign-in...
          </p>
        </div>
      </div>
    );
  }

  // Authenticated workspace
  return <WorkspaceView />;
};

export default App;
