import React, { useEffect, useState } from 'react';
import {
  useTeamStore,
  useTeamsList,
  useProjectsList,
  useActiveTeamId,
  useActiveProjectId,
} from '@/features/teams/store/useTeamStore';
import { TeamSwitcher } from '@/features/teams/components/TeamSwitcher';
import { ProjectIntegrationModal } from '@/features/integrations/components/ProjectIntegrationModal';
import { FolderKanban, GitBranch, Layers } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { integrationApi } from '@/features/integrations/api/integrationApi';

export const AppSidebar: React.FC = () => {
  const teams = useTeamsList();
  const projects = useProjectsList();
  const activeTeamId = useActiveTeamId();
  const activeProjectId = useActiveProjectId();
  const setActiveProjectId = useTeamStore((state) => state.setActiveProjectId);

  const activeTeam = teams.find((t) => t.id === activeTeamId);
  const [activeConnectedReposCount, setActiveConnectedReposCount] = useState<number | null>(null);

  // Fetch count of connected integrations for active project
  useEffect(() => {
    let isMounted = true;
    if (!activeProjectId) {
      setActiveConnectedReposCount(null);
      return;
    }

    integrationApi
      .listProjectIntegrations(activeProjectId)
      .then((integrations) => {
        if (isMounted) {
          setActiveConnectedReposCount(integrations.length);
        }
      })
      .catch(() => {
        if (isMounted) {
          setActiveConnectedReposCount(null);
        }
      });

    return () => {
      isMounted = false;
    };
  }, [activeProjectId]);

  return (
    <aside
      className="w-64 border-r border-border bg-card/40 flex flex-col h-full shrink-0 select-none"
      data-testid="app-sidebar"
      aria-label="Workspace Sidebar"
    >
      {/* Top: Logo & Title + Workspace Switcher */}
      <div className="p-4 border-b border-border space-y-4">
        <div className="flex items-center gap-2.5">
          <div className="size-8 rounded-lg bg-primary text-primary-foreground flex items-center justify-center font-bold text-xs shadow-xs">
            ATM
          </div>
          <div className="min-w-0 flex-1">
            <h1 className="font-semibold text-sm leading-tight tracking-tight text-foreground truncate">
              Agent Task Manager
            </h1>
            <p className="text-[11px] text-muted-foreground truncate">Autonomous Control Plane</p>
          </div>
        </div>

        <div className="space-y-1.5">
          <div className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider flex items-center gap-1.5">
            <Layers className="size-3" />
            <span>Workspace</span>
          </div>
          <div className="w-full [&>div]:w-full [&_button]:w-full">
            <TeamSwitcher />
          </div>
        </div>
      </div>

      {/* Middle: Projects under Active Team */}
      <div className="flex-1 overflow-y-auto p-3 space-y-4">
        <div className="space-y-1">
          <div className="px-2 py-1 flex items-center justify-between text-xs font-semibold text-muted-foreground tracking-wider uppercase">
            <div className="flex items-center gap-1.5">
              <FolderKanban className="size-3.5 text-muted-foreground" />
              <span>Projects</span>
            </div>
            {projects.length > 0 && (
              <span className="text-[10px] font-mono font-normal text-muted-foreground">
                {projects.length}
              </span>
            )}
          </div>

          {projects.length === 0 ? (
            <div className="px-2 py-3 text-xs text-muted-foreground text-center rounded-md border border-dashed border-border/60 bg-muted/20">
              No projects in {activeTeam ? activeTeam.name : 'workspace'}
            </div>
          ) : (
            <nav className="space-y-1" aria-label="Projects navigation">
              {projects.map((project) => {
                const isActive = project.id === activeProjectId;
                return (
                  <div key={project.id} className="space-y-1">
                    <button
                      type="button"
                      onClick={() => setActiveProjectId(project.id)}
                      data-testid={`sidebar-project-${project.id}`}
                      className={`w-full text-left px-2.5 py-1.5 rounded-md text-xs transition-colors flex items-center justify-between gap-2 group ${
                        isActive
                          ? 'bg-accent text-accent-foreground font-medium shadow-xs'
                          : 'text-muted-foreground hover:bg-muted/60 hover:text-foreground'
                      }`}
                      aria-current={isActive ? 'page' : undefined}
                    >
                      <span className="truncate">{project.name}</span>
                      {isActive ? (
                        <Badge variant="default" className="text-[9px] px-1.5 py-0 h-4 font-mono font-medium">
                          Active
                        </Badge>
                      ) : (
                        <span className="size-1.5 rounded-full shrink-0 bg-transparent group-hover:bg-muted-foreground/40" />
                      )}
                    </button>

                    {/* Integrations button under the active project */}
                    {isActive && (
                      <div className="pl-2 pr-1 py-0.5">
                        <ProjectIntegrationModal
                          trigger={
                            <Button
                              variant="ghost"
                              size="xs"
                              className="w-full justify-between text-[11px] h-7 text-muted-foreground hover:text-foreground px-2"
                              data-testid="sidebar-github-integrations-btn"
                            >
                              <div className="flex items-center gap-1.5 truncate">
                                <GitBranch className="size-3 text-primary shrink-0" />
                                <span className="truncate">GitHub Repos</span>
                              </div>
                              {activeConnectedReposCount !== null && (
                                <Badge
                                  variant="secondary"
                                  className="text-[9px] px-1 py-0 h-3.5 font-mono shrink-0"
                                  data-testid="sidebar-repos-count"
                                >
                                  {activeConnectedReposCount}
                                </Badge>
                              )}
                            </Button>
                          }
                        />
                      </div>
                    )}
                  </div>
                );
              })}
            </nav>
          )}
        </div>
      </div>
    </aside>
  );
};