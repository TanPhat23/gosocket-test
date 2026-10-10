import { queryOptions } from '@tanstack/react-query';
import { teamApi } from './api/teamApi';

export const teamsQueryOptions = () =>
  queryOptions({
    queryKey: ['teams'],
    queryFn: () => teamApi.getTeams(),
    staleTime: 1000 * 30,
  });

export const projectsQueryOptions = (teamId?: string) =>
  queryOptions({
    queryKey: ['projects', teamId],
    queryFn: () => teamApi.getProjects(teamId),
    staleTime: 1000 * 30,
  });
