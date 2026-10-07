import React, { useEffect } from 'react';
import { useAuthStore } from '@/features/auth/store/useAuthStore';
import { useTaskStore } from '@/features/tasks/store/useTaskStore';
import { useTeamStore } from '@/features/teams/store/useTeamStore';
import { useTaskSubscription } from '@/features/tasks/hooks/useTaskSubscription';
import { LoginScreen } from '@/features/auth/components/LoginScreen';
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

  // Loading state when initial session check is ongoing and no user cached
  if (!isInitialized || (isLoading && !user)) {
    return (
      <div className="min-h-screen bg-background text-foreground flex flex-col items-center justify-center gap-4">
        <div className="relative flex items-center justify-center">
          <div
            className="absolute -inset-2 rounded-full bg-primary/20 blur-lg animate-pulse"
            aria-hidden="true"
          />
          <div className="relative size-12 rounded-xl bg-card border border-border shadow-md flex items-center justify-center text-primary">
            <Loader2 className="size-6 animate-spin text-primary" />
          </div>
        </div>
        <div className="text-center space-y-1">
          <p className="text-sm font-medium tracking-tight text-foreground">
            Verifying session...
          </p>
          <p className="text-xs text-muted-foreground font-mono">
            Connecting to security gateway
          </p>
        </div>
      </div>
    );
  }

  // Unauthenticated: gate workspace with LoginScreen
  if (!user) {
    return <LoginScreen />;
  }

  // Authenticated workspace
  return <WorkspaceView />;
};

export default App;
