import { describe, it, expect, vi, beforeEach } from 'vitest';
import { credentialApi } from './credentialApi';
import { axiosClient } from '@/lib/axios';

describe('credentialApi', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  describe('listCredentials', () => {
    it('unpacks { credentials: [...] } payload from backend', async () => {
      vi.spyOn(axiosClient, 'get').mockResolvedValueOnce({
        data: {
          credentials: [
            {
              id: 'a1b2c3d4-e5f6-7a8b-9c0d-1e2f3a4b5c6d',
              team_id: 'b2c3d4e5-f6a7-8b9c-0d1e-2f3a4b5c6d7e',
              provider: 'github',
              label: 'Production PAT',
              created_at: '2026-10-07T00:00:00Z',
              updated_at: '2026-10-07T00:00:00Z',
            },
          ],
        },
      });

      const result = await credentialApi.listCredentials('b2c3d4e5-f6a7-8b9c-0d1e-2f3a4b5c6d7e');
      expect(result).toHaveLength(1);
      expect(result[0].label).toBe('Production PAT');
      expect(result[0].provider).toBe('github');
    });

    it('handles direct array payload defensively', async () => {
      vi.spyOn(axiosClient, 'get').mockResolvedValueOnce({
        data: [
          {
            id: 'a1b2c3d4-e5f6-7a8b-9c0d-1e2f3a4b5c6d',
            team_id: 'b2c3d4e5-f6a7-8b9c-0d1e-2f3a4b5c6d7e',
            provider: 'github',
            label: 'Direct Array PAT',
            created_at: '2026-10-07T00:00:00Z',
            updated_at: '2026-10-07T00:00:00Z',
          },
        ],
      });

      const result = await credentialApi.listCredentials('b2c3d4e5-f6a7-8b9c-0d1e-2f3a4b5c6d7e');
      expect(result).toHaveLength(1);
      expect(result[0].label).toBe('Direct Array PAT');
    });

    it('returns empty array when credentials property is empty or missing', async () => {
      vi.spyOn(axiosClient, 'get').mockResolvedValueOnce({
        data: { credentials: [] },
      });

      const result = await credentialApi.listCredentials('team-1');
      expect(result).toEqual([]);
    });
  });

  describe('createCredential', () => {
    it('unpacks { credential: {...} } payload from backend', async () => {
      vi.spyOn(axiosClient, 'post').mockResolvedValueOnce({
        data: {
          credential: {
            id: 'a1b2c3d4-e5f6-7a8b-9c0d-1e2f3a4b5c6d',
            team_id: 'b2c3d4e5-f6a7-8b9c-0d1e-2f3a4b5c6d7e',
            provider: 'github',
            label: 'New PAT',
            created_at: '2026-10-07T00:00:00Z',
            updated_at: '2026-10-07T00:00:00Z',
          },
        },
      });

      const result = await credentialApi.createCredential('b2c3d4e5-f6a7-8b9c-0d1e-2f3a4b5c6d7e', {
        provider: 'github',
        label: 'New PAT',
        secret: 'ghp_secret',
      });
      expect(result.id).toBe('a1b2c3d4-e5f6-7a8b-9c0d-1e2f3a4b5c6d');
      expect(result.label).toBe('New PAT');
    });
  });

  describe('updateCredential', () => {
    it('unpacks updated credential payload from backend', async () => {
      vi.spyOn(axiosClient, 'put').mockResolvedValueOnce({
        data: {
          credential: {
            id: 'a1b2c3d4-e5f6-7a8b-9c0d-1e2f3a4b5c6d',
            team_id: 'b2c3d4e5-f6a7-8b9c-0d1e-2f3a4b5c6d7e',
            provider: 'github',
            label: 'Updated PAT',
            created_at: '2026-10-07T00:00:00Z',
            updated_at: '2026-10-07T01:00:00Z',
          },
        },
      });

      const result = await credentialApi.updateCredential(
        'b2c3d4e5-f6a7-8b9c-0d1e-2f3a4b5c6d7e',
        'a1b2c3d4-e5f6-7a8b-9c0d-1e2f3a4b5c6d',
        {
          label: 'Updated PAT',
          secret: 'ghp_updated_secret',
        }
      );
      expect(result.id).toBe('a1b2c3d4-e5f6-7a8b-9c0d-1e2f3a4b5c6d');
      expect(result.label).toBe('Updated PAT');
    });
  });

  describe('listGitHubRepositories', () => {
    it('unpacks { repositories: [...] } payload from backend', async () => {
      vi.spyOn(axiosClient, 'get').mockResolvedValueOnce({
        data: {
          repositories: [
            {
              id: 123456,
              name: 'agent-task-manager',
              full_name: 'test-org/agent-task-manager',
              private: true,
              description: 'Autonomous control plane',
              html_url: 'https://github.com/test-org/agent-task-manager',
              default_branch: 'main',
            },
          ],
        },
      });

      const repos = await credentialApi.listGitHubRepositories('team-1', 'cred-1');
      expect(repos).toHaveLength(1);
      expect(repos[0].name).toBe('agent-task-manager');
      expect(repos[0].full_name).toBe('test-org/agent-task-manager');
      expect(repos[0].private).toBe(true);
      expect(repos[0].default_branch).toBe('main');
    });

    it('handles direct array payload defensively', async () => {
      vi.spyOn(axiosClient, 'get').mockResolvedValueOnce({
        data: [
          {
            id: 999,
            name: 'direct-repo',
            full_name: 'test-org/direct-repo',
            private: false,
            description: null,
            html_url: 'https://github.com/test-org/direct-repo',
            default_branch: 'master',
          },
        ],
      });

      const repos = await credentialApi.listGitHubRepositories('team-1', 'cred-1');
      expect(repos).toHaveLength(1);
      expect(repos[0].name).toBe('direct-repo');
      expect(repos[0].description).toBeNull();
      expect(repos[0].default_branch).toBe('master');
    });

    it('returns empty array when repositories field is missing or empty', async () => {
      vi.spyOn(axiosClient, 'get').mockResolvedValueOnce({
        data: { repositories: [] },
      });

      const repos = await credentialApi.listGitHubRepositories('team-1', 'cred-1');
      expect(repos).toEqual([]);
    });
  });

  describe('listGitHubBranches', () => {
    it('unpacks { branches: [...] } payload from backend', async () => {
      vi.spyOn(axiosClient, 'get').mockResolvedValueOnce({
        data: {
          branches: ['main', 'develop', 'release/v1.0'],
        },
      });

      const branches = await credentialApi.listGitHubBranches('team-1', 'cred-1', 'test-org/repo');
      expect(branches).toEqual(['main', 'develop', 'release/v1.0']);
    });

    it('handles direct array of branches defensively', async () => {
      vi.spyOn(axiosClient, 'get').mockResolvedValueOnce({
        data: ['main', 'master'],
      });

      const branches = await credentialApi.listGitHubBranches('team-1', 'cred-1', 'test-org/repo');
      expect(branches).toEqual(['main', 'master']);
    });
  });
});
