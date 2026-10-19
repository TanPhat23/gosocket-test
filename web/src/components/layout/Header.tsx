import React from 'react';
import { UserNav } from '@/features/auth/components/UserNav';
import { ProviderKeyDialog } from '@/features/auth/components/ProviderKeyDialog';
import { ThemeSettingsDialog } from '@/components/theme/ThemeSettingsDialog';
import {
  useTeamsList,
  useProjectsList,
  useActiveTeamId,
  useActiveProjectId,
} from '@/features/teams/store/useTeamStore';
import { ChevronRight } from 'lucide-react';
import { SidebarTrigger } from '@/components/ui/sidebar';
import { Separator } from '@/components/ui/separator';

export const Header: React.FC = () => {
  const teams = useTeamsList();
  const projects = useProjectsList();
  const activeTeamId = useActiveTeamId();
  const activeProjectId = useActiveProjectId();

  const activeTeam = teams.find((t) => t.id === activeTeamId);
  const activeProject = projects.find((p) => p.id === activeProjectId);

  return (
    <header className="sticky top-0 z-10 border-b border-border bg-card/50 backdrop-blur px-4 md:px-6 py-3 flex items-center justify-between gap-3">
      <SidebarTrigger className="-ml-1 shrink-0" data-testid="sidebar-toggle" />
      <Separator orientation="vertical" className="hidden h-4 sm:block" />
      {/* Breadcrumb: <Workspace Name> / <Active Project Name> */}
      <div className="flex min-w-0 flex-1 items-center gap-2 text-xs" data-testid="header-breadcrumbs">
        <span className="truncate font-medium text-foreground">
          {activeTeam ? activeTeam.name : 'Workspace'}
        </span>
        <ChevronRight className="size-3.5 text-muted-foreground/60 shrink-0" />
        <span className="truncate text-muted-foreground font-normal">
          {activeProject ? activeProject.name : 'All Projects'}
        </span>
      </div>

      <div className="flex shrink-0 items-center gap-1 sm:gap-3">
        <ProviderKeyDialog />
        <ThemeSettingsDialog />
        <UserNav />
      </div>
    </header>
  );
};
