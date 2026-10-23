import { z } from 'zod';

export const RuntimeStateSchema = z.enum(['running', 'starting', 'stopped', 'absent']);

export const RuntimeInfoSchema = z.object({
  state: RuntimeStateSchema,
  container_id: z.string().optional(),
  image: z.string().optional(),
  started_at: z.string().optional(),
  cpu_percent: z.number().optional(),
  memory_bytes: z.number().optional(),
  memory_limit_bytes: z.number().optional(),
});

export type RuntimeState = z.infer<typeof RuntimeStateSchema>;
export type RuntimeInfo = z.infer<typeof RuntimeInfoSchema>;

export const RuntimeLogsSchema = z.object({ logs: z.string() });
