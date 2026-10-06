import { axiosClient } from '@/lib/axios';
import {
  TaskSchema,
  TaskDTO,
  CreateTaskInput,
} from '../schemas/taskSchemas';

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

  async getModels(): Promise<{ id: string; name: string; provider: string; active?: boolean }[]> {
    const res = await axiosClient.get<{ id: string; name: string; provider: string; active?: boolean }[]>('/models');
    return res.data;
  },
};
