import React from 'react';
import { UserNav } from '@/features/auth/components/UserNav';
import { ThemeSettingsDialog } from '@/components/theme/ThemeSettingsDialog';
import {
  useTeamsList,
  useProjectsList,
  useActiveTeamId,
  useActiveProjectId,
} from '@/features/teams/store/useTeamStore';
import { ChevronRight } from 'lucide-react';

export const Header: React.FC = () => {
  const teams = useTeamsList();
  const projects = useProjectsList();
  const activeTeamId = useActiveTeamId();
  const activeProjectId = useActiveProjectId();

  const activeTeam = teams.find((t) => t.id === activeTeamId);
  const activeProject = projects.find((p) => p.id === activeProjectId);

  return (
    <header className="border-b border-border bg-card/50 backdrop-blur px-6 py-3 flex items-center justify-between">
      {/* Breadcrumb: <Workspace Name> / <Active Project Name> */}
      <div className="flex items-center gap-2 text-xs" data-testid="header-breadcrumbs">
        <span className="font-medium text-foreground">
          {activeTeam ? activeTeam.name : 'Workspace'}
        </span>
        <ChevronRight className="size-3.5 text-muted-foreground/60 shrink-0" />
        <span className="text-muted-foreground font-normal">
          {activeProject ? activeProject.name : 'All Projects'}
        </span>
      </div>

      <div className="flex items-center gap-3">
        <ThemeSettingsDialog />
        <UserNav />
      </div>
    </header>
  );
};
