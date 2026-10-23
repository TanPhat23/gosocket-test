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
      data: {
        id: 'usr-123',
        issuer: 'https://auth.example.com',
        subject: 'sub-456',
        email: 'dev@example.com',
        display_name: 'Developer',
      },
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
      images: [{ name: 'diag.png', mime: 'image/png', data: 'aGVsbG8=' }],
    });
    expect(created.id).toBe('task-1');
    expect(postSpy).toHaveBeenCalledWith('/tasks', expect.objectContaining({
      title: 'Fix issue',
      images: [{ name: 'diag.png', mime: 'image/png', data: 'aGVsbG8=' }],
    }));

    const fetched = await apiClient.getTask('task-1');
    expect(fetched.title).toBe('Fix issue');
    expect(getSpy).toHaveBeenCalledWith('/tasks/task-1');
  });

  it('creates task pull request via createTaskPR', async () => {
    const postSpy = vi.spyOn(apiClient.client, 'post').mockResolvedValue({
      status: 200,
      statusText: 'OK',
      headers: {},
      config: {} as any,
      data: {
        task_id: 'task-123',
        pull_request_url: 'https://github.com/org/repo/pull/42',
      },
    });

    const res = await apiClient.createTaskPR('task-123');
    expect(res.task_id).toBe('task-123');
    expect(res.pull_request_url).toBe('https://github.com/org/repo/pull/42');
    expect(postSpy).toHaveBeenCalledWith('/tasks/task-123/pr');
  });

  it('manages project integrations (list, create, update, delete)', async () => {
    const getSpy = vi.spyOn(apiClient.client, 'get').mockResolvedValue({
      status: 200,
      statusText: 'OK',
      headers: {},
      config: {} as any,
      data: [
        {
          id: 'integ-1',
          project_id: 'proj-1',
          credential_id: 'cred-1',
          provider: 'github',
          external_id: 'org/repo',
          external_url: 'https://github.com/org/repo',
          enabled: true,
          config: {},
          created_at: new Date().toISOString(),
          updated_at: new Date().toISOString(),
        },
      ],
    });

    const list = await apiClient.listProjectIntegrations('proj-1');
    expect(list).toHaveLength(1);
    expect(list[0].external_id).toBe('org/repo');
    expect(getSpy).toHaveBeenCalledWith('/projects/proj-1/integrations');

    const postSpy = vi.spyOn(apiClient.client, 'post').mockResolvedValue({
      status: 201,
      statusText: 'Created',
      headers: {},
      config: {} as any,
      data: {
        id: 'integ-2',
        project_id: 'proj-1',
        credential_id: 'cred-1',
        provider: 'github',
        external_id: 'org/repo-2',
        external_url: 'https://github.com/org/repo-2',
        enabled: true,
        config: {},
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
      },
    });

    const created = await apiClient.createProjectIntegration('proj-1', {
      credential_id: 'cred-1',
      provider: 'github',
      repo: 'org/repo-2',
      base_branch: 'main',
      auto_pr: true,
    });
    expect(created.id).toBe('integ-2');
    expect(postSpy).toHaveBeenCalledWith('/projects/proj-1/integrations', {
      credential_id: 'cred-1',
      provider: 'github',
      repo: 'org/repo-2',
      base_branch: 'main',
      auto_pr: true,
    });

    const deleteSpy = vi.spyOn(apiClient.client, 'delete').mockResolvedValue({
      status: 200,
      statusText: 'OK',
      headers: {},
      config: {} as any,
      data: {},
    });

    await apiClient.deleteProjectIntegration('proj-1', 'integ-1');
    expect(deleteSpy).toHaveBeenCalledWith('/projects/proj-1/integrations/integ-1');
  });

  it('interacts with agent task sessions (get, prompt, compact)', async () => {
    const mockSession = {
      task_id: 'task-123',
      session_id: 'ses_atm_task-123',
      status: 'succeeded',
      model: 'gemini-3.8-flash',
      tokens: {
        input: 1000,
        output: 200,
        reasoning: 0,
        cache_read: 0,
        cache_write: 0,
      },
      cost: 0,
      messages: [
        {
          id: 'msg-1',
          type: 'user',
          role: 'user',
          text: 'Inspect repository',
        },
      ],
    };

    const getSpy = vi.spyOn(apiClient.client, 'get').mockResolvedValue({
      status: 200,
      statusText: 'OK',
      headers: {},
      config: {} as any,
      data: mockSession,
    });

    const session = await apiClient.getTaskSession('task-123');
    expect(session.session_id).toBe('ses_atm_task-123');
    expect(session.messages).toHaveLength(1);
    expect(getSpy).toHaveBeenCalledWith('/tasks/task-123/session');

    const postSpy = vi.spyOn(apiClient.client, 'post').mockResolvedValue({
      status: 200,
      statusText: 'OK',
      headers: {},
      config: {} as any,
      data: { status: 'ok' },
    });

    await apiClient.promptTaskSession('task-123', 'Focus on unit tests');
    expect(postSpy).toHaveBeenCalledWith('/tasks/task-123/session/prompt', {
      prompt: 'Focus on unit tests',
      images: [],
    });

    await apiClient.compactTaskSession('task-123');
    expect(postSpy).toHaveBeenCalledWith('/tasks/task-123/session/compact', {});
  });
});
