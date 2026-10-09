import React, { useEffect } from 'react';
import { useAuthStore } from '@/features/auth/store/useAuthStore';
import { LoginForm } from '@/features/auth/components/LoginForm';
import { useTaskStore } from '@/features/tasks/store/useTaskStore';
import { useTeamStore, useActiveProjectId } from '@/features/teams/store/useTeamStore';
import { useTaskSubscription } from '@/features/tasks/hooks/useTaskSubscription';
import { AppSidebar } from '@/components/layout/AppSidebar';
import { Header } from '@/components/layout/Header';
import { Footer } from '@/components/layout/Footer';
import { WorkspaceControlBar } from '@/features/tasks/components/WorkspaceControlBar';
import { KanbanBoard } from '@/features/tasks/components/KanbanBoard';
import { TaskInspectorModal } from '@/features/tasks/components/TaskInspectorModal';
import { Loader2 } from 'lucide-react';

export const WorkspaceView: React.FC = () => {
  const fetchTasks = useTaskStore((state) => state.fetchTasks);
  const fetchTeams = useTeamStore((state) => state.fetchTeams);
  const activeProjectId = useActiveProjectId();

  // Maintain real-time task status updates across the Kanban board
  useTaskSubscription();

  useEffect(() => {
    fetchTeams();
  }, [fetchTeams]);

  useEffect(() => {
    fetchTasks(activeProjectId || undefined);
  }, [fetchTasks, activeProjectId]);

  return (
    <div className="h-screen bg-background text-foreground flex overflow-hidden font-sans">
      {/* Left Sidebar */}
      <AppSidebar />

      {/* Main Area */}
      <div className="flex-1 flex flex-col h-full overflow-y-auto">
        <Header />

        {/* Main Workspace */}
        <main className="flex-1 w-full px-6 py-6 flex flex-col gap-6">
          {/* Workspace Control Panel with Project/Model Selectors, Live Filters, and New Task */}
          <WorkspaceControlBar />

          {/* 4-Lane Kanban Board */}
          <section className="flex-1 flex flex-col gap-3">
            <KanbanBoard />
          </section>

          {/* Task Inspector Modal (opens when selectedTaskId is set) */}
          <TaskInspectorModal />
        </main>

        <Footer />
      </div>
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

  // Session check resolved without user; show in-app direct login form
  if (!user) {
    return <LoginForm />;
  }

  // Authenticated workspace
  return <WorkspaceView />;
};

export default App;
