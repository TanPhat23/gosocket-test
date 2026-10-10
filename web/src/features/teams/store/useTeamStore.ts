import { create } from 'zustand';
import { teamApi } from '../api/teamApi';
import type { TeamDTO as Team, ProjectDTO as Project } from '../schemas/teamSchemas';

interface TeamState {
  teams: Team[];
  activeTeamId: string | null;
  projects: Project[];
  activeProjectId: string | null;
  isLoading: boolean;
  error: string | null;

  setTeamsData: (teams: Team[]) => void;
  fetchTeams: () => Promise<void>;
  selectTeam: (teamId: string) => void;
  fetchProjects: (teamId?: string) => Promise<void>;
  selectProject: (projectId: string | null) => void;
  setActiveProjectId: (projectId: string | null) => void;
  createProject: (req: { name: string; team_id: string; repo_path?: string }) => Promise<Project>;
  deleteProject: (projectId: string) => Promise<void>;
}

export const useTeamStore = create<TeamState>((set, get) => ({
  teams: [],
  activeTeamId: null,
  projects: [],
  activeProjectId: null,
  isLoading: false,
  error: null,

  setTeamsData: (teams: Team[]) => {
    const currentActiveTeamId = get().activeTeamId;
    const activeTeamId = currentActiveTeamId && teams.some((t) => t.id === currentActiveTeamId)
      ? currentActiveTeamId
      : (teams.length > 0 ? teams[0].id : null);
    set({ teams, activeTeamId, isLoading: false, error: null });
    if (activeTeamId && get().projects.length === 0) {
      get().fetchProjects(activeTeamId);
    }
  },

  fetchTeams: async () => {
    set({ isLoading: true, error: null });
    try {
      const teams = await teamApi.getTeams();
      const activeTeamId = get().activeTeamId || (teams.length > 0 ? teams[0].id : null);
      set({ teams, activeTeamId, isLoading: false });
      if (activeTeamId) {
        get().fetchProjects(activeTeamId);
      }
    } catch (err) {
      set({
        isLoading: false,
        error: err instanceof Error ? err.message : 'Failed to fetch teams',
      });
    }
  },

  selectTeam: (teamId: string) => {
    set({ activeTeamId: teamId, activeProjectId: null });
    get().fetchProjects(teamId);
  },

  fetchProjects: async (teamId) => {
    try {
      const projects = await teamApi.getProjects(teamId);
      const currentActive = get().activeProjectId;
      let nextActive = currentActive;
      if (!currentActive || !projects.some((p) => p.id === currentActive)) {
        nextActive = projects.length > 0 ? projects[0].id : null;
      }
      set({ projects, activeProjectId: nextActive });
    } catch (err) {
      set({ error: err instanceof Error ? err.message : 'Failed to fetch projects' });
    }
  },

  selectProject: (projectId: string | null) => {
    set({ activeProjectId: projectId });
  },

  setActiveProjectId: (projectId: string | null) => {
    set({ activeProjectId: projectId });
  },

  createProject: async (req) => {
    const newProject = await teamApi.createProject(req);
    set((state) => ({
      projects: [newProject, ...state.projects],
      activeProjectId: newProject.id,
    }));
    return newProject;
  },

  deleteProject: async (projectId: string) => {
    await teamApi.deleteProject(projectId);
    set((state) => {
      const remaining = state.projects.filter((p) => p.id !== projectId);
      const nextActive = state.activeProjectId === projectId
        ? (remaining.length > 0 ? remaining[0].id : null)
        : state.activeProjectId;
      return {
        projects: remaining,
        activeProjectId: nextActive,
      };
    });
  },
}));

export const useActiveTeamId = () => useTeamStore((state) => state.activeTeamId);
export const useActiveProjectId = () => useTeamStore((state) => state.activeProjectId);
export const useTeamsList = () => useTeamStore((state) => state.teams);
export const useProjectsList = () => useTeamStore((state) => state.projects);
