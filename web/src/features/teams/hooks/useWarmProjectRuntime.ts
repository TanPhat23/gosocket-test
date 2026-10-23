import { useEffect } from 'react';
import { teamApi } from '../api/teamApi';

// Opening a project starts its container ahead of the first task. The request is best effort: if it fails, the
// task run still starts the container itself.
export function useWarmProjectRuntime(projectId: string | null): void {
  useEffect(() => {
    if (!projectId) return;
    teamApi.startProjectRuntime(projectId).catch((err: unknown) => {
      console.warn('Could not start the project runtime:', err);
    });
  }, [projectId]);
}
