import React, { useState } from 'react';
import { useAuthStore } from '../store/useAuthStore';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { ArrowRight, AlertCircle } from 'lucide-react';

export const LoginForm: React.FC = () => {
  const [email, setEmail] = useState('admin@example.com');
  const [password, setPassword] = useState('password123');
  const login = useAuthStore((state) => state.login);
  const isLoading = useAuthStore((state) => state.isLoading);
  const error = useAuthStore((state) => state.error);

  const handleSubmit = async (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    if (!email || !password) return;
    await login({ email, password });
  };

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

        <form onSubmit={handleSubmit} className="flex flex-col gap-4">
          <div className="flex flex-col gap-1.5">
            <label
              htmlFor="email"
              className="text-xs font-medium text-foreground tracking-tight"
            >
              Email
            </label>
            <Input
              id="email"
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              required
              disabled={isLoading}
              autoComplete="email"
            />
          </div>

          <div className="flex flex-col gap-1.5">
            <label
              htmlFor="password"
              className="text-xs font-medium text-foreground tracking-tight"
            >
              Password
            </label>
            <Input
              id="password"
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
              disabled={isLoading}
              autoComplete="current-password"
            />
          </div>

          <Button
            type="submit"
            className="w-full mt-1"
            disabled={isLoading}
          >
            {isLoading ? 'Signing in...' : 'Sign In'}
          </Button>
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

        <p className="mt-6 text-center text-[11px] text-muted-foreground">
          Local demo: <span className="font-mono text-foreground/80">admin@example.com</span> ·{' '}
          <span className="font-mono text-foreground/80">password123</span>
        </p>
      </div>
    </div>
  );
};
