import { z } from 'zod';

export const UserSchema = z.object({
  id: z.string().uuid().or(z.string().min(1)),
  issuer: z.string().min(1).optional(),
  subject: z.string().min(1).optional(),
  email: z.string().email().optional(),
  display_name: z.string().optional(),
});

export type UserDTO = z.infer<typeof UserSchema>;

export const LoginResponseSchema = z.object({
  status: z.string(),
  user: UserSchema,
});

export type LoginResponseDTO = z.infer<typeof LoginResponseSchema>;
