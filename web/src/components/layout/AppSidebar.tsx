import React, { useEffect, useState } from 'react';
import {
  useTeamStore,
  useTeamsList,
  useProjectsList,
  useActiveTeamId,
  useActiveProjectId,
} from '@/features/teams/store/useTeamStore';
import { useTaskStore } from '@/features/tasks/store/useTaskStore';
import { TeamSwitcher } from '@/features/teams/components/TeamSwitcher';
import { ProjectIntegrationModal } from '@/features/integrations/components/ProjectIntegrationModal';
import { FolderKanban, GitBranch, Layers, Plus, Trash2 } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { Input } from '@/components/ui/input';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from '@/components/ui/dialog';
import { integrationApi } from '@/features/integrations/api/integrationApi';
import type { ProjectDTO as Project } from '@/features/teams/schemas/teamSchemas';

export const AppSidebar: React.FC = () => {
  const teams = useTeamsList();
  const projects = useProjectsList();
  const activeTeamId = useActiveTeamId();
  const activeProjectId = useActiveProjectId();
  const setActiveProjectId = useTeamStore((state) => state.setActiveProjectId);
  const createProject = useTeamStore((state) => state.createProject);
  const deleteProject = useTeamStore((state) => state.deleteProject);
  const allTasks = useTaskStore((state) => state.tasks);

  const activeTeam = teams.find((t) => t.id === activeTeamId);
  const [activeConnectedReposCount, setActiveConnectedReposCount] = useState<number | null>(null);

  // New Project Dialog state
  const [isNewProjectOpen, setIsNewProjectOpen] = useState(false);
  const [newProjectName, setNewProjectName] = useState('');
  const [newProjectRepo, setNewProjectRepo] = useState('');
  const [isCreatingProject, setIsCreatingProject] = useState(false);
  const [createProjectError, setCreateProjectError] = useState<string | null>(null);

  // Delete Project Confirmation Dialog state
  const [projectToDelete, setProjectToDelete] = useState<Project | null>(null);
  const [isDeletingProject, setIsDeletingProject] = useState(false);

  const handleCreateProject = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!newProjectName.trim()) return;
    setIsCreatingProject(true);
    setCreateProjectError(null);
    try {
      await createProject({
        name: newProjectName.trim(),
        team_id: activeTeamId || (teams.length > 0 ? teams[0].id : 'team_1'),
        repo_path: newProjectRepo.trim() || newProjectName.trim(),
      });
      setNewProjectName('');
      setNewProjectRepo('');
      setIsNewProjectOpen(false);
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : 'Failed to create project';
      setCreateProjectError(msg);
    } finally {
      setIsCreatingProject(false);
    }
  };

  const handleDeleteProject = async () => {
    if (!projectToDelete) return;
    setIsDeletingProject(true);
    try {
      await deleteProject(projectToDelete.id);
      setProjectToDelete(null);
    } catch {
      // error handled
    } finally {
      setIsDeletingProject(false);
    }
  };

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
            <div className="flex items-center gap-1">
              {projects.length > 0 && (
                <span className="text-[10px] font-mono font-normal text-muted-foreground mr-1">
                  {projects.length}
                </span>
              )}
              <Button
                type="button"
                variant="ghost"
                size="icon"
                onClick={() => setIsNewProjectOpen(true)}
                className="size-5 text-muted-foreground hover:text-foreground"
                title="Create New Project"
                data-testid="add-new-project-btn"
              >
                <Plus className="size-3" />
              </Button>
            </div>
          </div>

          <nav className="space-y-1.5" aria-label="Projects navigation">
            {projects.length === 0 ? (
              <div className="px-2 py-3 text-xs text-muted-foreground text-center rounded-md border border-dashed border-border/60 bg-muted/20">
                No projects in {activeTeam ? activeTeam.name : 'workspace'}
              </div>
            ) : (
              <>
                {/* All Projects Option */}
                <div
                  onClick={() => setActiveProjectId(null)}
                  data-testid="sidebar-all-projects"
                  role="button"
                  tabIndex={0}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' || e.key === ' ') {
                      e.preventDefault();
                      setActiveProjectId(null);
                    }
                  }}
                  className={`w-full text-left px-2.5 py-1.5 rounded-md text-xs transition-colors flex items-center justify-between gap-1.5 cursor-pointer ${
                    activeProjectId === null
                      ? 'bg-accent text-accent-foreground font-medium shadow-xs'
                      : 'text-muted-foreground hover:bg-muted/60 hover:text-foreground'
                  }`}
                  aria-current={activeProjectId === null ? 'page' : undefined}
                >
                  <div className="flex items-center gap-1.5 truncate">
                    <Layers className="size-3 text-muted-foreground shrink-0" />
                    <span className="truncate">All Projects</span>
                  </div>
                  {activeProjectId === null && (
                    <Badge variant="default" className="text-[9px] px-1.5 py-0 h-4 font-mono font-medium">
                      Active
                    </Badge>
                  )}
                </div>

                {projects.map((project) => {
                const isActive = project.id === activeProjectId;
                const workingTasks = Object.values(allTasks).filter(
                  (t) => t.project_id === project.id && t.status === 'in_progress'
                );

                return (
                  <div key={project.id} className="space-y-1 group/item">
                    <div
                      onClick={() => setActiveProjectId(project.id)}
                      data-testid={`sidebar-project-${project.id}`}
                      role="button"
                      tabIndex={0}
                      onKeyDown={(e) => {
                        if (e.key === 'Enter' || e.key === ' ') {
                          e.preventDefault();
                          setActiveProjectId(project.id);
                        }
                      }}
                      className={`w-full text-left px-2.5 py-1.5 rounded-md text-xs transition-colors flex items-center justify-between gap-1.5 cursor-pointer group ${
                        isActive
                          ? 'bg-accent text-accent-foreground font-medium shadow-xs'
                          : 'text-muted-foreground hover:bg-muted/60 hover:text-foreground'
                      }`}
                      aria-current={isActive ? 'page' : undefined}
                    >
                      <span className="truncate flex-1">{project.name}</span>

                      <div className="flex items-center gap-1 shrink-0">
                        {isActive && (
                          <Badge variant="default" className="text-[9px] px-1.5 py-0 h-4 font-mono font-medium">
                            Active
                          </Badge>
                        )}
                        <Button
                          type="button"
                          variant="ghost"
                          size="icon"
                          onClick={(e) => {
                            e.stopPropagation();
                            setProjectToDelete(project);
                          }}
                          className="size-5 opacity-0 group-hover/item:opacity-100 text-muted-foreground hover:text-destructive hover:bg-destructive/10 transition-opacity"
                          title={`Delete ${project.name}`}
                          data-testid={`delete-project-${project.id}`}
                        >
                          <Trash2 className="size-3" />
                        </Button>
                      </div>
                    </div>

                    {/* Active working task indicator (if any task is in_progress) */}
                    {workingTasks.length === 1 && (
                      <div
                        className="mx-1 px-2 py-0.5 rounded bg-emerald-500/10 border border-emerald-500/20 text-[10px] text-emerald-600 dark:text-emerald-400 font-medium flex items-center gap-1.5 min-w-0"
                        title={`Currently working on: ${workingTasks[0].title}`}
                        data-testid={`project-active-task-${project.id}`}
                      >
                        <span className="size-1.5 rounded-full bg-emerald-500 animate-pulse shrink-0" />
                        <span className="truncate">{workingTasks[0].title}</span>
                      </div>
                    )}
                    {workingTasks.length > 1 && (
                      <div
                        className="mx-1 px-2 py-0.5 rounded bg-emerald-500/10 border border-emerald-500/20 text-[10px] text-emerald-600 dark:text-emerald-400 font-medium flex items-center gap-1.5 min-w-0"
                        title={`${workingTasks.length} tasks currently being worked`}
                        data-testid={`project-active-tasks-count-${project.id}`}
                      >
                        <span className="size-1.5 rounded-full bg-emerald-500 animate-pulse shrink-0" />
                        <span className="truncate">{workingTasks.length} tasks in progress</span>
                      </div>
                    )}

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
              </>
            )}
          </nav>
        </div>
      </div>

      {/* Create Project Modal */}
      <Dialog open={isNewProjectOpen} onOpenChange={setIsNewProjectOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle className="text-sm font-semibold flex items-center gap-2">
              <FolderKanban className="size-4 text-primary" />
              Create New Project
            </DialogTitle>
          </DialogHeader>
          <form onSubmit={handleCreateProject} className="space-y-3.5 pt-2">
            <div className="space-y-1.5">
              <label htmlFor="new-project-name" className="text-xs font-medium text-foreground">
                Project Name
              </label>
              <Input
                id="new-project-name"
                value={newProjectName}
                onChange={(e) => setNewProjectName(e.target.value)}
                placeholder="e.g. mobile-app or backend-service"
                required
                className="text-xs"
                data-testid="new-project-name-input"
              />
            </div>
            <div className="space-y-1.5">
              <label htmlFor="new-project-repo" className="text-xs font-medium text-foreground">
                Repository / Working Directory (Optional)
              </label>
              <Input
                id="new-project-repo"
                value={newProjectRepo}
                onChange={(e) => setNewProjectRepo(e.target.value)}
                placeholder="e.g. my-org/mobile-app (defaults to name)"
                className="text-xs font-mono"
                data-testid="new-project-repo-input"
              />
            </div>
            {createProjectError && (
              <p className="text-xs text-destructive font-mono">{createProjectError}</p>
            )}
            <DialogFooter className="pt-2">
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() => setIsNewProjectOpen(false)}
                className="text-xs"
              >
                Cancel
              </Button>
              <Button
                type="submit"
                size="sm"
                disabled={!newProjectName.trim() || isCreatingProject}
                className="text-xs font-medium"
                data-testid="submit-create-project-btn"
              >
                {isCreatingProject ? 'Creating...' : 'Create Project'}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      {/* Delete Project Confirmation Dialog */}
      <Dialog open={!!projectToDelete} onOpenChange={(open) => !open && setProjectToDelete(null)}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle className="text-sm font-semibold flex items-center gap-2 text-destructive">
              <Trash2 className="size-4" />
              Delete Project
            </DialogTitle>
          </DialogHeader>
          <div className="py-2 space-y-2 text-xs text-muted-foreground">
            <p>
              Are you sure you want to delete <strong className="text-foreground">{projectToDelete?.name}</strong>?
            </p>
            <p className="text-[11px] text-muted-foreground/80">
              This will remove the project from your workspace.
            </p>
          </div>
          <DialogFooter className="pt-2">
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => setProjectToDelete(null)}
              className="text-xs"
            >
              Cancel
            </Button>
            <Button
              type="button"
              variant="destructive"
              size="sm"
              disabled={isDeletingProject}
              onClick={handleDeleteProject}
              className="text-xs font-medium"
              data-testid="confirm-delete-project-btn"
            >
              {isDeletingProject ? 'Deleting...' : 'Delete Project'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </aside>
  );
};