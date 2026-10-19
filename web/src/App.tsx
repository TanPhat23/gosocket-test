import React, { lazy, Suspense, useEffect, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useAuthStore } from '@/features/auth/store/useAuthStore';
import { LoginForm } from '@/features/auth/components/LoginForm';
import { useSelectedTaskId, useTaskStore } from '@/features/tasks/store/useTaskStore';
import { useTeamStore, useActiveProjectId } from '@/features/teams/store/useTeamStore';
import { useTaskSubscription } from '@/features/tasks/hooks/useTaskSubscription';
import { tasksQueryOptions } from '@/features/tasks/queries';
import { teamsQueryOptions } from '@/features/teams/queries';
import { AppShell } from '@/components/layout/AppShell';
import { Footer } from '@/components/layout/Footer';
import { WorkspaceControlBar } from '@/features/tasks/components/WorkspaceControlBar';
import { KanbanBoard } from '@/features/tasks/components/KanbanBoard';
import { Loader2 } from 'lucide-react';

// The inspector loads the terminal, markdown and session views, so its code is split out of the workspace bundle.
const TaskInspectorModal = lazy(() =>
  import('@/features/tasks/components/TaskInspectorModal').then((m) => ({ default: m.TaskInspectorModal }))
);

// LazyTaskInspector loads the inspector the first time a task is selected. It then stays mounted, as it did before,
// so the inspector keeps its open tab between selections.
const LazyTaskInspector: React.FC = () => {
  const selectedTaskId = useSelectedTaskId();
  const [requested, setRequested] = useState(selectedTaskId !== null);
  if (selectedTaskId !== null && !requested) {
    setRequested(true);
  }
  if (!requested) return null;
  return (
    <Suspense fallback={null}>
      <TaskInspectorModal />
    </Suspense>
  );
};

export const WorkspaceView: React.FC = () => {
  const setTasksData = useTaskStore((state) => state.setTasksData);
  const setTeamsData = useTeamStore((state) => state.setTeamsData);
  const activeProjectId = useActiveProjectId();

  // Consume TanStack Query cache driven by queries
  const { data: cachedTeams } = useQuery(teamsQueryOptions());
  const { data: cachedTasks, dataUpdatedAt: tasksUpdatedAt } = useQuery(
    tasksQueryOptions(activeProjectId || undefined)
  );

  // Sync loader/query data into Zustand store on load or updates
  useEffect(() => {
    if (cachedTeams && cachedTeams.length > 0) {
      setTeamsData(cachedTeams);
    }
  }, [cachedTeams, setTeamsData]);

  // Also keyed on dataUpdatedAt: a refetch that returns the same list still re-syncs the store.
  useEffect(() => {
    if (cachedTasks) {
      setTasksData(cachedTasks);
    }
  }, [cachedTasks, tasksUpdatedAt, setTasksData]);

  // Maintain real-time task status updates across the Kanban board
  useTaskSubscription();

  return (
    <AppShell>
      {/* Main Workspace */}
      <div className="flex-1 w-full px-6 py-6 flex flex-col gap-6">
        {/* Workspace Control Panel with Project/Model Selectors, Live Filters, and New Task */}
        <WorkspaceControlBar />

        {/* 4-Lane Kanban Board */}
        <section className="flex-1 flex flex-col gap-3">
          <KanbanBoard />
        </section>

        {/* Task Inspector Modal (opens when selectedTaskId is set) */}
        <LazyTaskInspector />
      </div>

      <Footer />
    </AppShell>
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
