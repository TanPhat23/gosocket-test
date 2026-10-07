import React from 'react';
import { ShieldCheck, Bot, KeyRound, ArrowRight, UserCheck, Code } from 'lucide-react';
import { buttonVariants } from '@/components/ui/button';
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from '@/components/ui/card';
import { cn } from '@/lib/utils';

export const LoginScreen: React.FC = () => {
  return (
    <div className="min-h-screen bg-background text-foreground flex flex-col justify-center items-center px-4 py-12 relative overflow-hidden">
      {/* Background radial glow accents for dark-tech aesthetic */}
      <div
        className="pointer-events-none absolute -top-40 -left-40 size-96 rounded-full bg-primary/5 blur-3xl"
        aria-hidden="true"
      />
      <div
        className="pointer-events-none absolute -bottom-40 -right-40 size-96 rounded-full bg-primary/5 blur-3xl"
        aria-hidden="true"
      />

      <div className="w-full max-w-md mx-auto flex flex-col gap-6 relative z-10">
        {/* ATM Branding Header */}
        <div className="flex flex-col items-center text-center space-y-3">
          <div className="relative flex items-center justify-center">
            <div
              className="absolute -inset-1 rounded-2xl bg-primary/20 blur-md animate-pulse"
              aria-hidden="true"
            />
            <div className="relative size-16 rounded-2xl bg-card border border-border shadow-xl flex items-center justify-center text-primary">
              <Bot className="size-8" />
            </div>
            <span
              className="absolute -top-1 -right-1 flex h-3.5 w-3.5"
              aria-label="Agent subsystem online"
            >
              <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75" />
              <span className="relative inline-flex rounded-full h-3.5 w-3.5 bg-emerald-500" />
            </span>
          </div>

          <div className="space-y-1">
            <div className="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-[11px] font-mono font-medium tracking-wide border border-border bg-muted/60 text-muted-foreground uppercase">
              <ShieldCheck className="size-3 text-emerald-500" />
              <span>ATM Core Security</span>
            </div>
            <h1 className="text-2xl font-bold tracking-tight text-foreground sm:text-3xl">
              Autonomous Coding Control Plane
            </h1>
            <p className="text-sm text-muted-foreground leading-relaxed max-w-sm mx-auto">
              Orchestrate agents across Git worktrees, OpenCode sessions, and automated GitHub pull requests.
            </p>
          </div>
        </div>

        {/* Primary Auth Action */}
        <Card className="border border-border/80 shadow-lg bg-card/80 backdrop-blur">
          <CardHeader className="text-center pb-2">
            <CardTitle className="text-base font-semibold">Authentication Required</CardTitle>
            <CardDescription className="text-xs">
              Single Sign-On via enterprise OpenID Connect provider
            </CardDescription>
          </CardHeader>
          <CardContent className="pt-2 flex flex-col gap-4">
            <a
              href="/api/v1/auth/login"
              className={cn(
                buttonVariants({ variant: 'default' }),
                'w-full h-11 text-sm font-medium shadow-md transition-all cursor-pointer flex items-center justify-center gap-2'
              )}
            >
              <span>Sign in with SSO</span>
              <ArrowRight className="size-4" />
            </a>
          </CardContent>
        </Card>

        {/* Local Development Credentials Hint Card */}
        <Card size="sm" className="border-dashed border-border/70 bg-muted/20">
          <CardHeader className="pb-1 pt-3 px-4">
            <div className="flex items-center gap-2 text-xs font-semibold text-muted-foreground">
              <KeyRound className="size-3.5" />
              <span>Local Development Credentials</span>
            </div>
            <CardDescription className="text-[11px] text-muted-foreground/80">
              For local testing with mock authentication:
            </CardDescription>
          </CardHeader>
          <CardContent className="pt-1 pb-3 px-4 text-xs font-mono">
            <div className="space-y-2">
              <div className="flex items-center justify-between p-2 rounded-md bg-background/60 border border-border/50">
                <div className="flex items-center gap-2">
                  <UserCheck className="size-3.5 text-primary" />
                  <span className="font-semibold text-foreground">Admin:</span>
                  <span className="text-muted-foreground select-all">admin@example.com</span>
                </div>
                <span className="text-[11px] text-muted-foreground/70 bg-muted px-1.5 py-0.5 rounded">password123</span>
              </div>
              <div className="flex items-center justify-between p-2 rounded-md bg-background/60 border border-border/50">
                <div className="flex items-center gap-2">
                  <Code className="size-3.5 text-primary" />
                  <span className="font-semibold text-foreground">Developer:</span>
                  <span className="text-muted-foreground select-all">developer@example.com</span>
                </div>
                <span className="text-[11px] text-muted-foreground/70 bg-muted px-1.5 py-0.5 rounded">password123</span>
              </div>
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  );
};
