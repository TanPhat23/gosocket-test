import React, { useEffect } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { ArrowLeft } from 'lucide-react';
import { AppShell } from '@/components/layout/AppShell';
import { teamsQueryOptions } from '@/features/teams/queries';
import { useProjectsList, useTeamStore, useTeamsList } from '@/features/teams/store/useTeamStore';
import { RuntimeControls } from './RuntimeControls';
import { RuntimeLogViewer } from './RuntimeLogViewer';
import { RuntimeStatusPanel } from './RuntimeStatusPanel';

const ADMIN_ROLE = 30;

// Manage the container that runs a project: its state, start/stop/restart, and its logs.
export const RuntimePage: React.FC<{ projectId: string }> = ({ projectId }) => {
  const { data: cachedTeams } = useQuery(teamsQueryOptions());
  const setTeamsData = useTeamStore((state) => state.setTeamsData);
  const teams = useTeamsList();
  const projects = useProjectsList();

  // The sidebar and header read the team store, which the workspace page normally fills.
  useEffect(() => {
    if (cachedTeams && cachedTeams.length > 0) setTeamsData(cachedTeams);
  }, [cachedTeams, setTeamsData]);

  const project = projects.find((p) => p.id === projectId);
  const role = teams.find((t) => t.id === project?.team_id)?.role ?? 0;

  return (
    <AppShell>
        <div className="flex-1 w-full max-w-4xl px-6 py-6 flex flex-col gap-6">
          <div className="space-y-1">
            <Link to="/" className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground">
              <ArrowLeft className="size-3" /> Board
            </Link>
            <h1 className="text-lg font-semibold tracking-tight" data-testid="runtime-title">
              {project ? `${project.name} container` : 'Project container'}
            </h1>
            <p className="text-sm text-muted-foreground">
              Every task of this project runs inside this container.
            </p>
          </div>
          <RuntimeStatusPanel projectId={projectId} />
          <RuntimeControls projectId={projectId} canManage={role >= ADMIN_ROLE} />
          <RuntimeLogViewer projectId={projectId} />
        </div>
    </AppShell>
  );
};
