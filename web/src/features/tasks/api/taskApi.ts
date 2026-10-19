import { axiosClient } from '@/lib/axios';
import {
  TaskSchema,
  TaskDTO,
  CreateTaskInput,
} from '../schemas/taskSchemas';
import type { SteerImage } from '../utils/steerImages';

// TaskFilesResponse lists a task workspace. Source is "workspace" for the live worktree or "committed" for the branch.
export interface TaskFilesResponse {
  task_id: string;
  source: string;
  files: string[];
}

// TaskFileResponse is one file of a task workspace. Content is empty for binary files.
export interface TaskFileResponse {
  task_id: string;
  path: string;
  source: string;
  size: number;
  content: string;
  binary: boolean;
  truncated: boolean;
}

export const taskApi = {
  async getTasks(projectId?: string): Promise<TaskDTO[]> {
    const res = await axiosClient.get<unknown[]>('/tasks', {
      params: projectId ? { project_id: projectId } : undefined,
    });
    return res.data.map((item) => TaskSchema.parse(item));
  },

  async getTask(taskId: string): Promise<TaskDTO> {
    const res = await axiosClient.get<unknown>(`/tasks/${encodeURIComponent(taskId)}`);
    return TaskSchema.parse(res.data);
  },

  async createTask(req: CreateTaskInput): Promise<TaskDTO> {
    const res = await axiosClient.post<unknown>('/tasks', req);
    return TaskSchema.parse(res.data);
  },

  async cancelTask(taskId: string): Promise<{ status: string }> {
    const res = await axiosClient.post<{ status: string }>(`/tasks/${encodeURIComponent(taskId)}/cancel`);
    return res.data;
  },

  async createTaskPR(taskId: string): Promise<{ task_id: string; pull_request_url: string }> {
    const res = await axiosClient.post<{ task_id: string; pull_request_url: string }>(
      `/tasks/${encodeURIComponent(taskId)}/pr`
    );
    return res.data;
  },

  async getModels(): Promise<{ id: string; name: string; provider: string; active?: boolean }[]> {
    const res = await axiosClient.get<{ id: string; name: string; provider: string; active?: boolean }[]>('/models');
    return res.data;
  },

  async getTaskSession(taskId: string): Promise<import('../schemas/taskSchemas').TaskSessionResponse> {
    const res = await axiosClient.get<import('../schemas/taskSchemas').TaskSessionResponse>(
      `/tasks/${encodeURIComponent(taskId)}/session`
    );
    return res.data;
  },

  async promptTaskSession(taskId: string, prompt: string, images: SteerImage[] = []): Promise<void> {
    await axiosClient.post(`/tasks/${encodeURIComponent(taskId)}/session/prompt`, {
      prompt,
      images: images.map(({ name, mime, data }) => ({ name, mime, data })),
    });
  },

  async compactTaskSession(taskId: string): Promise<void> {
    await axiosClient.post(`/tasks/${encodeURIComponent(taskId)}/session/compact`, {});
  },

  async interruptTaskSession(taskId: string): Promise<void> {
    await axiosClient.post(`/tasks/${encodeURIComponent(taskId)}/session/interrupt`, {});
  },

  async getTaskDiff(taskId: string): Promise<string> {
    const res = await axiosClient.get<{ diff?: string }>(`/tasks/${encodeURIComponent(taskId)}/diff`);
    return res.data?.diff ?? '';
  },

  async getTaskFiles(taskId: string): Promise<TaskFilesResponse> {
    const res = await axiosClient.get<TaskFilesResponse>(`/tasks/${encodeURIComponent(taskId)}/files`);
    return res.data;
  },

  async getTaskFile(taskId: string, path: string): Promise<TaskFileResponse> {
    const res = await axiosClient.get<TaskFileResponse>(
      `/tasks/${encodeURIComponent(taskId)}/files/content`,
      { params: { path } }
    );
    return res.data;
  },

  async syncTaskPR(taskId: string): Promise<TaskDTO> {
    const res = await axiosClient.post<TaskDTO>(`/tasks/${encodeURIComponent(taskId)}/sync-pr`);
    return res.data;
  },
};
