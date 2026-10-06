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

  fetchTeams: () => Promise<void>;
  selectTeam: (teamId: string) => void;
  fetchProjects: (teamId?: string) => Promise<void>;
  selectProject: (projectId: string | null) => void;
}

export const useTeamStore = create<TeamState>((set, get) => ({
  teams: [],
  activeTeamId: null,
  projects: [],
  activeProjectId: null,
  isLoading: false,
  error: null,

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
      const activeProjectId = projects.length > 0 ? projects[0].id : null;
      set({ projects, activeProjectId });
    } catch (err) {
      set({ error: err instanceof Error ? err.message : 'Failed to fetch projects' });
    }
  },

  selectProject: (projectId: string | null) => {
    set({ activeProjectId: projectId });
  },
}));

export const useActiveTeamId = () => useTeamStore((state) => state.activeTeamId);
export const useActiveProjectId = () => useTeamStore((state) => state.activeProjectId);
export const useTeamsList = () => useTeamStore((state) => state.teams);
export const useProjectsList = () => useTeamStore((state) => state.projects);
