import { axiosClient } from '@/lib/axios';
import { RuntimeInfoSchema, RuntimeLogsSchema, type RuntimeInfo } from '../schemas/runtimeSchemas';

const base = (projectId: string) => `/projects/${encodeURIComponent(projectId)}/runtime`;

export const runtimeApi = {
  async getRuntime(projectId: string): Promise<RuntimeInfo> {
    const res = await axiosClient.get<unknown>(base(projectId));
    return RuntimeInfoSchema.parse(res.data);
  },

  async getLogs(projectId: string, tail: number): Promise<string> {
    const res = await axiosClient.get<unknown>(`${base(projectId)}/logs`, { params: { tail } });
    return RuntimeLogsSchema.parse(res.data).logs;
  },

  // Start, stop and restart answer 202 at once; the container changes state in the background.
  async start(projectId: string): Promise<void> {
    await axiosClient.post(base(projectId));
  },

  // Stop and restart answer 409 while tasks are still running, unless force is set.
  async stop(projectId: string, force: boolean): Promise<void> {
    await axiosClient.post(`${base(projectId)}/stop`, null, { params: force ? { force: true } : undefined });
  },

  async restart(projectId: string, force: boolean): Promise<void> {
    await axiosClient.post(`${base(projectId)}/restart`, null, { params: force ? { force: true } : undefined });
  },
};
