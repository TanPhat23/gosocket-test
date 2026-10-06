import React, { useEffect } from 'react';
import { useAuthStore } from '@/features/auth/store/useAuthStore';
import { useTaskStore } from '@/features/tasks/store/useTaskStore';
import { useTeamStore } from '@/features/teams/store/useTeamStore';
import { Header } from '@/components/layout/Header';
import { Footer } from '@/components/layout/Footer';
import { TaskMetricsGrid } from '@/features/tasks/components/TaskMetricsGrid';
import { KanbanBoard } from '@/features/tasks/components/KanbanBoard';
import { TaskModal } from '@/features/tasks/components/TaskModal';
import { TaskInspectorModal } from '@/features/tasks/components/TaskInspectorModal';

export const App: React.FC = () => {
  const fetchUser = useAuthStore((state) => state.fetchUser);
  const fetchTasks = useTaskStore((state) => state.fetchTasks);
  const fetchTeams = useTeamStore((state) => state.fetchTeams);

  useEffect(() => {
    fetchUser();
    fetchTasks();
    fetchTeams();
  }, [fetchUser, fetchTasks, fetchTeams]);

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

export default App;
