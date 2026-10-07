import { z } from 'zod';

export const UserSchema = z.object({
  id: z.string().min(1),
  issuer: z.string().min(1).optional().nullable(),
  subject: z.string().min(1).optional().nullable(),
  email: z.string().optional().nullable(),
  display_name: z.string().optional().nullable(),
});

export type UserDTO = z.infer<typeof UserSchema>;

export const AuthMeResponseSchema = z.union([
  z.object({ user: UserSchema }).transform((data) => data.user),
  UserSchema,
]);

export const LoginResponseSchema = z.object({
  status: z.string(),
  user: UserSchema,
});

export type LoginResponseDTO = z.infer<typeof LoginResponseSchema>;
