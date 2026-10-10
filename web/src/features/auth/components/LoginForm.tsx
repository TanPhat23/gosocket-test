import React from 'react';
import { useForm } from '@tanstack/react-form';
import { z } from 'zod';
import { useAuthStore } from '../store/useAuthStore';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { ArrowRight, AlertCircle } from 'lucide-react';

const loginSchema = z.object({
  email: z.string().min(1, 'Email is required').email('Invalid email address'),
  password: z.string().min(1, 'Password is required'),
});

export const LoginForm: React.FC = () => {
  const login = useAuthStore((state) => state.login);
  const authLoading = useAuthStore((state) => state.isLoading);
  const error = useAuthStore((state) => state.error);
  const [isSubmitting, setIsSubmitting] = React.useState(false);

  const isBusy = isSubmitting || authLoading;

  const form = useForm({
    defaultValues: {
      email: '',
      password: '',
    },
    validators: {
      onSubmit: loginSchema,
    },
    onSubmit: async ({ value }) => {
      setIsSubmitting(true);
      try {
        await login({ email: value.email, password: value.password });
        const user = useAuthStore.getState().user;
        if (user) {
          const params = typeof window !== 'undefined' ? new URLSearchParams(window.location.search) : null;
          const target = params?.get('redirect') || '/';
          window.location.href = target;
        }
      } finally {
        setIsSubmitting(false);
      }
    },
  });

  return (
    <div className="min-h-screen bg-background text-foreground flex flex-col items-center justify-center p-4">
      <div className="max-w-sm w-full mx-auto p-6 bg-card border border-border rounded-xl shadow-sm">
        <div className="flex flex-col gap-1 mb-6 text-center">
          <h1 className="tracking-tight font-semibold text-lg text-card-foreground">
            Agent Task Manager
          </h1>
          <p className="text-xs text-muted-foreground">
            Sign in to access your control plane
          </p>
        </div>

        {error && (
          <div
            role="alert"
            className="mb-4 flex items-start gap-2 p-3 text-xs text-destructive bg-destructive/10 border border-destructive/20 rounded-lg"
          >
            <AlertCircle className="size-4 shrink-0 mt-0.5" aria-hidden="true" />
            <span className="leading-snug">{error}</span>
          </div>
        )}

        <form
          onSubmit={(e) => {
            e.preventDefault();
            e.stopPropagation();
            form.handleSubmit();
          }}
          className="flex flex-col gap-4"
        >
          <form.Field
            name="email"
            children={(field) => {
              const firstErr = field.state.meta.errors[0];
              const errorMessage = firstErr
                ? typeof firstErr === 'string'
                  ? firstErr
                  : (firstErr as unknown as { message?: string })?.message
                : undefined;

              return (
                <div className="flex flex-col gap-1.5">
                  <label
                    htmlFor={field.name}
                    className="text-xs font-medium text-foreground tracking-tight"
                  >
                    Email
                  </label>
                  <Input
                    id={field.name}
                    name={field.name}
                    type="email"
                    placeholder="name@example.com"
                    value={field.state.value}
                    onBlur={field.handleBlur}
                    onChange={(e) => field.handleChange(e.target.value)}
                    required
                    disabled={isBusy}
                    autoComplete="email"
                  />
                  {errorMessage && (
                    <p className="text-[11px] text-destructive font-medium">{errorMessage}</p>
                  )}
                </div>
              );
            }}
          />

          <form.Field
            name="password"
            children={(field) => {
              const firstErr = field.state.meta.errors[0];
              const errorMessage = firstErr
                ? typeof firstErr === 'string'
                  ? firstErr
                  : (firstErr as unknown as { message?: string })?.message
                : undefined;

              return (
                <div className="flex flex-col gap-1.5">
                  <label
                    htmlFor={field.name}
                    className="text-xs font-medium text-foreground tracking-tight"
                  >
                    Password
                  </label>
                  <Input
                    id={field.name}
                    name={field.name}
                    type="password"
                    placeholder="••••••••"
                    value={field.state.value}
                    onBlur={field.handleBlur}
                    onChange={(e) => field.handleChange(e.target.value)}
                    required
                    disabled={isBusy}
                    autoComplete="current-password"
                  />
                  {errorMessage && (
                    <p className="text-[11px] text-destructive font-medium">{errorMessage}</p>
                  )}
                </div>
              );
            }}
          />

          <form.Subscribe
            selector={(state) => [state.isSubmitting]}
            children={([formSubmitting]) => {
              const activeLoading = formSubmitting || isBusy;
              return (
                <Button
                  type="submit"
                  className="w-full mt-1"
                  disabled={activeLoading}
                >
                  {activeLoading ? 'Signing in...' : 'Sign In'}
                </Button>
              );
            }}
          />
        </form>

        <div className="relative my-6 text-center text-xs">
          <div className="absolute inset-0 flex items-center" aria-hidden="true">
            <div className="w-full border-t border-border" />
          </div>
          <span className="relative bg-card px-2 text-muted-foreground uppercase tracking-widest text-[10px]">
            or
          </span>
        </div>

        <Button
          variant="outline"
          nativeButton={false}
          className="w-full justify-center gap-1.5 text-xs text-foreground"
          render={<a href="/api/v1/auth/login" />}
        >
          <span>Sign in with SSO</span>
          <ArrowRight className="size-3.5" aria-hidden="true" />
        </Button>
      </div>
    </div>
  );
};