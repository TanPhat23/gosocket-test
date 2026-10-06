import { describe, it, expect, vi, beforeEach } from 'vitest';
import { apiClient, ApiError } from './api';

describe('apiClient (axios)', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('attaches credentials and parses json response for getTeams', async () => {
    const spy = vi.spyOn(apiClient.client, 'get').mockResolvedValue({
      status: 200,
      statusText: 'OK',
      headers: {},
      config: {} as any,
      data: [{ id: 'team-1', name: 'Core Team' }],
    });

    const teams = await apiClient.getTeams();
    expect(teams).toHaveLength(1);
    expect(teams[0].name).toBe('Core Team');
    expect(spy).toHaveBeenCalledWith('/teams');
    expect(apiClient.client.defaults.withCredentials).toBe(true);
  });

  it('fetches current user with getMe', async () => {
    vi.spyOn(apiClient.client, 'get').mockResolvedValue({
      status: 200,
      statusText: 'OK',
      headers: {},
      config: {} as any,
      data: { id: 'usr-123', email: 'dev@example.com', display_name: 'Developer' },
    });

    const user = await apiClient.getMe();
    expect(user.id).toBe('usr-123');
    expect(user.email).toBe('dev@example.com');
  });

  it('throws ApiError with status and error message on non-ok response', async () => {
    vi.spyOn(apiClient.client, 'get').mockRejectedValue(
      new ApiError(401, 'unauthorized session', { error: 'unauthorized session' })
    );

    await expect(apiClient.getMe()).rejects.toThrow(ApiError);
    await expect(apiClient.getMe()).rejects.toThrow('unauthorized session');
  });

  it('creates and lists credentials for a team', async () => {
    const postSpy = vi.spyOn(apiClient.client, 'post').mockResolvedValue({
      status: 201,
      statusText: 'Created',
      headers: {},
      config: {} as any,
      data: {
        id: 'cred-1',
        team_id: 'team-1',
        provider: 'github',
        label: 'GitHub PAT',
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
      },
    });

    const getSpy = vi.spyOn(apiClient.client, 'get').mockResolvedValue({
      status: 200,
      statusText: 'OK',
      headers: {},
      config: {} as any,
      data: [
        {
          id: 'cred-1',
          team_id: 'team-1',
          provider: 'github',
          label: 'GitHub PAT',
          created_at: new Date().toISOString(),
          updated_at: new Date().toISOString(),
        },
      ],
    });

    const created = await apiClient.createCredential('team-1', {
      provider: 'github',
      label: 'GitHub PAT',
      secret: 'ghp_secret123',
    });
    expect(created.id).toBe('cred-1');
    expect(postSpy).toHaveBeenCalledWith('/teams/team-1/credentials', {
      provider: 'github',
      label: 'GitHub PAT',
      secret: 'ghp_secret123',
    });

    const list = await apiClient.listCredentials('team-1');
    expect(list).toHaveLength(1);
    expect(list[0].label).toBe('GitHub PAT');
    expect(getSpy).toHaveBeenCalledWith('/teams/team-1/credentials');
  });

  it('creates and retrieves tasks', async () => {
    const postSpy = vi.spyOn(apiClient.client, 'post').mockResolvedValue({
      status: 201,
      statusText: 'Created',
      headers: {},
      config: {} as any,
      data: {
        id: 'task-1',
        project_id: 'proj-1',
        title: 'Fix issue',
        prompt: 'Refactor module',
        assignee_id: 'agent-1',
        assignee_type: 'agent',
        status: 'queued',
        priority: 50,
        skills: [],
        mcp_servers: [],
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
      },
    });

    const getSpy = vi.spyOn(apiClient.client, 'get').mockResolvedValue({
      status: 200,
      statusText: 'OK',
      headers: {},
      config: {} as any,
      data: {
        id: 'task-1',
        project_id: 'proj-1',
        title: 'Fix issue',
        prompt: 'Refactor module',
        assignee_id: 'agent-1',
        assignee_type: 'agent',
        status: 'queued',
        priority: 50,
        skills: [],
        mcp_servers: [],
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
      },
    });

    const created = await apiClient.createTask({
      project_id: 'proj-1',
      title: 'Fix issue',
      prompt: 'Refactor module',
      assignee_id: 'agent-1',
    });
    expect(created.id).toBe('task-1');
    expect(postSpy).toHaveBeenCalledWith('/tasks', expect.objectContaining({
      title: 'Fix issue',
    }));

    const fetched = await apiClient.getTask('task-1');
    expect(fetched.title).toBe('Fix issue');
    expect(getSpy).toHaveBeenCalledWith('/tasks/task-1');
  });
});
