import React, { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
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
import { useWarmProjectRuntime } from '@/features/teams/hooks/useWarmProjectRuntime';
import { Container, FolderKanban, GitBranch, Layers, Plus, Trash2 } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from '@/components/ui/dialog';
import {
  Sidebar,
  SidebarContent,
  SidebarGroup,
  SidebarGroupAction,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuAction,
  SidebarMenuBadge,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarMenuSub,
  SidebarMenuSubButton,
  SidebarMenuSubItem,
  SidebarRail,
  useSidebar,
} from '@/components/ui/sidebar';
import { projectIntegrationsQueryOptions } from '@/features/integrations/queries';
import type { ProjectDTO as Project } from '@/features/teams/schemas/teamSchemas';

export const AppSidebar: React.FC = () => {
  const teams = useTeamsList();
  const projects = useProjectsList();
  const activeTeamId = useActiveTeamId();
  const activeProjectId = useActiveProjectId();
  useWarmProjectRuntime(activeProjectId);
  const setActiveProjectId = useTeamStore((state) => state.setActiveProjectId);
  const createProject = useTeamStore((state) => state.createProject);
  const deleteProject = useTeamStore((state) => state.deleteProject);
  const allTasks = useTaskStore((state) => state.tasks);

  // On a phone the sidebar is a sheet that should get out of the way once a project is picked.
  const { isMobile, setOpenMobile } = useSidebar();
  const closeOnMobile = () => {
    if (isMobile) setOpenMobile(false);
  };

  const selectProject = (id: string | null) => {
    setActiveProjectId(id);
    closeOnMobile();
  };

  const activeTeam = teams.find((t) => t.id === activeTeamId);

  // Use TanStack Query for project integrations
  const { data: projectIntegrations } = useQuery({
    ...projectIntegrationsQueryOptions(activeProjectId || ''),
    enabled: Boolean(activeProjectId),
  });
  const activeConnectedReposCount = activeProjectId && projectIntegrations ? projectIntegrations.length : null;

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

  return (
    <>
      <Sidebar data-testid="app-sidebar" aria-label="Workspace Sidebar" className="select-none">
        <SidebarHeader className="gap-4 border-b border-sidebar-border p-4">
          <div className="flex items-center gap-2.5">
            <div className="size-8 rounded-lg bg-primary text-primary-foreground flex items-center justify-center font-bold text-xs shadow-xs">
              ATM
            </div>
            <div className="min-w-0 flex-1">
              <h1 className="font-semibold text-sm leading-tight tracking-tight truncate">Agent Task Manager</h1>
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
        </SidebarHeader>

        <SidebarContent>
          <SidebarGroup>
            <SidebarGroupLabel className="gap-1.5 uppercase tracking-wider">
              <FolderKanban className="size-3.5" />
              <span>Projects</span>
              {projects.length > 0 && (
                <span className="ml-1 font-mono text-[10px] font-normal">{projects.length}</span>
              )}
            </SidebarGroupLabel>
            <SidebarGroupAction
              onClick={() => setIsNewProjectOpen(true)}
              title="Create New Project"
              data-testid="add-new-project-btn"
            >
              <Plus />
              <span className="sr-only">Create New Project</span>
            </SidebarGroupAction>

            <SidebarGroupContent>
              {projects.length === 0 ? (
                <div className="px-2 py-3 text-xs text-muted-foreground text-center rounded-md border border-dashed border-border/60 bg-muted/20">
                  No projects in {activeTeam ? activeTeam.name : 'workspace'}
                </div>
              ) : (
                <SidebarMenu aria-label="Projects navigation">
                  <SidebarMenuItem>
                    <SidebarMenuButton
                      isActive={activeProjectId === null}
                      onClick={() => selectProject(null)}
                      data-testid="sidebar-all-projects"
                      aria-current={activeProjectId === null ? 'page' : undefined}
                    >
                      <Layers />
                      <span>All Projects</span>
                    </SidebarMenuButton>
                  </SidebarMenuItem>

                  {projects.map((project) => {
                    const isActive = project.id === activeProjectId;
                    const workingTasks = Object.values(allTasks).filter(
                      (t) => t.project_id === project.id && t.status === 'in_progress'
                    );

                    return (
                      <SidebarMenuItem key={project.id}>
                        <SidebarMenuButton
                          isActive={isActive}
                          onClick={() => selectProject(project.id)}
                          data-testid={`sidebar-project-${project.id}`}
                          aria-current={isActive ? 'page' : undefined}
                          className="pr-8"
                        >
                          <span>{project.name}</span>
                        </SidebarMenuButton>
                        <SidebarMenuAction
                          showOnHover
                          onClick={() => setProjectToDelete(project)}
                          className="hover:text-destructive"
                          title={`Delete ${project.name}`}
                          data-testid={`delete-project-${project.id}`}
                        >
                          <Trash2 />
                          <span className="sr-only">Delete {project.name}</span>
                        </SidebarMenuAction>

                        {/* Active working task indicator (if any task is in_progress) */}
                        {workingTasks.length > 0 && (
                          <div
                            className="mx-2 mt-1 px-2 py-0.5 rounded bg-emerald-500/10 border border-emerald-500/20 text-[10px] text-emerald-600 dark:text-emerald-400 font-medium flex items-center gap-1.5 min-w-0"
                            title={
                              workingTasks.length === 1
                                ? `Currently working on: ${workingTasks[0].title}`
                                : `${workingTasks.length} tasks currently being worked`
                            }
                            data-testid={
                              workingTasks.length === 1
                                ? `project-active-task-${project.id}`
                                : `project-active-tasks-count-${project.id}`
                            }
                          >
                            <span className="size-1.5 rounded-full bg-emerald-500 animate-pulse shrink-0" />
                            <span className="truncate">
                              {workingTasks.length === 1 ? workingTasks[0].title : `${workingTasks.length} tasks in progress`}
                            </span>
                          </div>
                        )}

                        {isActive && (
                          <SidebarMenuSub>
                            <SidebarMenuSubItem>
                              <SidebarMenuSubButton
                                size="sm"
                                render={<Link to="/projects/$projectId/runtime" params={{ projectId: project.id }} />}
                                onClick={closeOnMobile}
                                data-testid="sidebar-runtime-link"
                              >
                                <Container />
                                <span>Container</span>
                              </SidebarMenuSubButton>
                            </SidebarMenuSubItem>
                            <SidebarMenuSubItem>
                              <ProjectIntegrationModal
                                trigger={
                                  <SidebarMenuSubButton
                                    size="sm"
                                    render={<button type="button" />}
                                    className="w-full"
                                    data-testid="sidebar-github-integrations-btn"
                                  >
                                    <GitBranch />
                                    <span>GitHub Repos</span>
                                  </SidebarMenuSubButton>
                                }
                              />
                              {activeConnectedReposCount !== null && (
                                <SidebarMenuBadge data-testid="sidebar-repos-count" className="top-1">
                                  {activeConnectedReposCount}
                                </SidebarMenuBadge>
                              )}
                            </SidebarMenuSubItem>
                          </SidebarMenuSub>
                        )}
                      </SidebarMenuItem>
                    );
                  })}
                </SidebarMenu>
              )}
            </SidebarGroupContent>
          </SidebarGroup>
        </SidebarContent>
        <SidebarRail />
      </Sidebar>

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
    </>
  );
};
