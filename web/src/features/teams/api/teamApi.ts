import { axiosClient } from '@/lib/axios';
import { TeamSchema, TeamDTO, ProjectSchema, ProjectDTO } from '../schemas/teamSchemas';

export const teamApi = {
  async getTeams(): Promise<TeamDTO[]> {
    const res = await axiosClient.get<unknown[]>('/teams');
    return res.data.map((item) => TeamSchema.parse(item));
  },

  async getProjects(teamId?: string): Promise<ProjectDTO[]> {
    const res = await axiosClient.get<unknown[]>('/projects', {
      params: teamId ? { team_id: teamId } : undefined,
    });
    return res.data.map((item) => ProjectSchema.parse(item));
  },

  async createProject(req: { name: string; team_id: string; repo_path?: string }): Promise<ProjectDTO> {
    const res = await axiosClient.post<unknown>('/projects', req);
    return ProjectSchema.parse(res.data);
  },

  async deleteProject(projectId: string): Promise<void> {
    await axiosClient.delete(`/projects/${encodeURIComponent(projectId)}`);
  },
};
