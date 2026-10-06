import React, { useEffect, useState } from 'react';
import { apiClient, User } from './lib/api';

export const App: React.FC = () => {
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    apiClient
      .getMe()
      .then((data) => {
        setUser(data);
        setError(null);
      })
      .catch((err) => {
        // Unauthenticated or network error
        setError(err instanceof Error ? err.message : 'Not authenticated');
      })
      .finally(() => setLoading(false));
  }, []);

  const handleLogout = async () => {
    try {
      await apiClient.logout();
      setUser(null);
    } catch (err) {
      console.error('Logout failed:', err);
    }
  };

  return (
    <div className="min-h-screen bg-slate-950 text-slate-100 flex flex-col font-sans">
      {/* Navigation header */}
      <header className="border-b border-slate-800 bg-slate-900/60 backdrop-blur px-6 py-4 flex items-center justify-between">
        <div className="flex items-center space-x-3">
          <div className="h-8 w-8 rounded-lg bg-indigo-600 flex items-center justify-center font-bold text-white shadow-md shadow-indigo-500/20">
            ATM
          </div>
          <div>
            <h1 className="font-semibold text-lg leading-tight">Agent Task Manager</h1>
            <p className="text-xs text-slate-400">Control Plane Dashboard</p>
          </div>
        </div>

        <div className="flex items-center space-x-4">
          {loading ? (
            <span className="text-sm text-slate-400 animate-pulse">Loading session...</span>
          ) : user ? (
            <div className="flex items-center space-x-3">
              <div className="text-right">
                <div className="text-sm font-medium text-slate-200">
                  {user.display_name || user.email || 'Authenticated User'}
                </div>
                <div className="text-xs text-emerald-400">● Session Active</div>
              </div>
              <button
                onClick={handleLogout}
                className="px-3 py-1.5 text-xs font-medium bg-slate-800 hover:bg-slate-700 text-slate-300 rounded border border-slate-700 transition"
              >
                Log Out
              </button>
            </div>
          ) : (
            <a
              href="/api/v1/auth/login"
              className="px-4 py-2 text-xs font-semibold bg-indigo-600 hover:bg-indigo-500 text-white rounded-md shadow transition"
            >
              Sign In via OIDC
            </a>
          )}
        </div>
      </header>

      {/* Main Workspace */}
      <main className="flex-1 max-w-7xl w-full mx-auto p-6">
        <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
          <div className="p-5 rounded-xl border border-slate-800 bg-slate-900/50">
            <h2 className="text-sm font-medium text-slate-400 uppercase tracking-wider mb-2">
              System Status
            </h2>
            <div className="flex items-center space-x-2 text-emerald-400 text-sm font-semibold">
              <span className="h-2 w-2 rounded-full bg-emerald-400 animate-ping"></span>
              <span>API Gateway Connected</span>
            </div>
            <p className="text-xs text-slate-500 mt-2">
              Fiber v3 REST & WebSocket streams ready.
            </p>
          </div>

          <div className="p-5 rounded-xl border border-slate-800 bg-slate-900/50">
            <h2 className="text-sm font-medium text-slate-400 uppercase tracking-wider mb-2">
              Active Session
            </h2>
            <p className="text-sm text-slate-300">
              {user ? (user.email || user.id) : (error ? 'No active session' : 'Anonymous')}
            </p>
            <p className="text-xs text-slate-500 mt-2">
              HMAC-SHA256 session cookie + PAT Dual Auth.
            </p>
          </div>

          <div className="p-5 rounded-xl border border-slate-800 bg-slate-900/50">
            <h2 className="text-sm font-medium text-slate-400 uppercase tracking-wider mb-2">
              Queue & Workers
            </h2>
            <p className="text-sm text-slate-300">Asynq Redis Tier: Ready</p>
            <p className="text-xs text-slate-500 mt-2">
              Critical (6) / Default (3) / Low (1) priority scheduling.
            </p>
          </div>
        </div>

        {/* Board placeholder for Task 16 */}
        <section className="mt-8 rounded-xl border border-slate-800 bg-slate-900/40 p-8 text-center">
          <h3 className="text-base font-semibold text-slate-200">Agent Task Kanban & Terminal View</h3>
          <p className="text-sm text-slate-400 mt-1 max-w-md mx-auto">
            Task execution pipelines, WebSocket live terminal logs, and review diff viewers will appear here.
          </p>
        </section>
      </main>

      <footer className="border-t border-slate-800 py-4 px-6 text-center text-xs text-slate-500">
        Agent Task Manager v0.1.0 &bull; Self-Hostable Autonomous Control Plane
      </footer>
    </div>
  );
};

export default App;
