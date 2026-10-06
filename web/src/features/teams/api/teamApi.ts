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
};
