import { queryOptions } from '@tanstack/react-query';
import { integrationApi } from './api/integrationApi';
import { credentialApi } from '@/features/teams/api/credentialApi';

export const projectIntegrationsQueryOptions = (projectId: string) =>
  queryOptions({
    queryKey: ['integrations', projectId],
    queryFn: () => integrationApi.listProjectIntegrations(projectId),
    staleTime: 1000 * 30,
  });

export const credentialsQueryOptions = (teamId: string) =>
  queryOptions({
    queryKey: ['credentials', teamId],
    queryFn: () => credentialApi.listCredentials(teamId),
    staleTime: 1000 * 30,
  });

export const gitHubReposQueryOptions = (teamId: string, credentialId: string) =>
  queryOptions({
    queryKey: ['github-repos', teamId, credentialId],
    queryFn: () => credentialApi.listGitHubRepositories(teamId, credentialId),
    staleTime: 1000 * 60,
  });

export const gitHubBranchesQueryOptions = (
  teamId: string,
  credentialId: string,
  repo: string
) =>
  queryOptions({
    queryKey: ['github-branches', teamId, credentialId, repo],
    queryFn: () => credentialApi.listGitHubBranches(teamId, credentialId, repo),
    staleTime: 1000 * 60,
  });
