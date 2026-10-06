import React, { useEffect } from 'react';
import { useAuthStore } from '@/features/auth/store/useAuthStore';
import { Header } from '@/components/layout/Header';
import { Footer } from '@/components/layout/Footer';
import { TaskMetricsGrid } from '@/features/tasks/components/TaskMetricsGrid';
import { TaskBoardPlaceholder } from '@/features/tasks/components/TaskBoardPlaceholder';

export const App: React.FC = () => {
  const fetchUser = useAuthStore((state) => state.fetchUser);

  useEffect(() => {
    fetchUser();
  }, [fetchUser]);

  return (
    <div className="min-h-screen bg-background text-foreground flex flex-col font-sans">
      <Header />

      {/* Main Workspace */}
      <main className="flex-1 max-w-7xl w-full mx-auto p-6 flex flex-col gap-6">
        <TaskMetricsGrid />
        <TaskBoardPlaceholder />
      </main>

      <Footer />
    </div>
  );
};

export default App;
