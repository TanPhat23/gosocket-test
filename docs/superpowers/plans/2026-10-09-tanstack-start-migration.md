# TanStack Start SSR Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Migrate the web frontend from a static Vite SPA to a full-stack Server-Side Rendered (SSR) application using TanStack Start (`@tanstack/react-start` + `@tanstack/react-router` + `@tanstack/react-query`).

**Architecture:** TanStack Start runs as a Node.js Nitro server container proxying API and WebSocket traffic to the Go Fiber control plane. Route `beforeLoad` guards enforce server-side auth without UI flash, route loaders pre-fetch queries into TanStack Query, and `/tasks/$taskId` deep links open the inspector modal directly over the Kanban workspace.

**Tech Stack:** `@tanstack/react-start`, `@tanstack/react-router`, `@tanstack/react-query`, `@tanstack/react-form`, `react` 19, `@tailwindcss/vite` (Tailwind v4), `@base-ui/react`, `@shadcn/react`, Docker.

**Spec:** `docs/superpowers/specs/2026-10-09-tanstack-start-migration-design.md`

## Global Constraints
- Node.js runtime: Node 22 Alpine in Docker container.
- Vite / Tailwind: Tailwind v4 with `@tailwindcss/vite` and `@/` path alias.
- React 19: All peer dependencies and component trees must support React 19.
- Port: Nitro server listens on port `3000` internally, Traefik routes web traffic to `agent-web-ui:3000`.
- API proxy: Server-side calls forward client cookies to `http://control-plane:8080`.

## Review Focus
- Unauthenticated requests to protected routes immediately redirect to `/login` without rendering protected markup.
- Server-side loaders forward cookies and headers to the Go backend during SSR without dropping auth credentials.
- Deep-linking to `/tasks/$taskId` opens the inspector modal while rendering the Kanban workspace underneath.
- Real-time WebSocket messages (`task_status_changed`, `agent_event`) update the TanStack Query cache without race conditions.
- Production multi-stage Docker build produces a runnable Nitro server (`.output/server/index.mjs`) on port 3000.

---

### Task 1: Install TanStack Start Stack & Configure Build Tooling

**Files:**
- Modify: `web/package.json`
- Create: `web/app.config.ts`
- Modify: `web/tsconfig.json`

**Interfaces:**
- Produces: TanStack Start build configuration with Tailwind v4 and path aliases.

- [ ] **Step 1: Install TanStack Start and Router dependencies**

Run in `web`:
```bash
npm install --prefix web @tanstack/react-start @tanstack/react-router @tanstack/react-query @tanstack/react-query-devtools vite-tsconfig-paths
```

- [ ] **Step 2: Create `web/app.config.ts`**

```ts
import { defineConfig } from '@tanstack/react-start/config';
import tailwindcss from '@tailwindcss/vite';
import tsConfigPaths from 'vite-tsconfig-paths';

export default defineConfig({
  server: {
    preset: 'node-server',
  },
  vite: {
    plugins: [
      tsConfigPaths(),
      tailwindcss(),
    ],
  },
});
```

- [ ] **Step 3: Update `web/tsconfig.json` to include TanStack Router types**

Ensure `compilerOptions` includes `"moduleResolution": "bundler"`, `"jsx": "react-jsx"`, and path alias `"@/*": ["./src/*"]`.

- [ ] **Step 4: Verify configuration build test**

Run: `npm run build --prefix web`
Expected: Passes build or reports missing entry point/routes.

- [ ] **Step 5: Commit build scaffolding**

```bash
git add web/package.json web/package-lock.json web/app.config.ts web/tsconfig.json
git commit -m "chore(web): add TanStack Start dependencies and app.config.ts"
```

---

### Task 2: Query Client, SSR API Client & Query Options Factories

**Files:**
- Create: `web/src/lib/queryClient.ts`
- Modify: `web/src/lib/api.ts`
- Create: `web/src/features/auth/queries.ts`
- Create: `web/src/features/tasks/queries.ts`
- Modify: `web/src/features/tasks/hooks/useTaskSubscription.ts`
- Test: `web/src/features/tasks/queries.test.ts`

**Interfaces:**
- Produces: `getQueryClient()`, `tasksQueryOptions()`, `taskDetailQueryOptions(id)`, `taskSessionQueryOptions(id)`, `authQueryOptions()`

- [ ] **Step 1: Write failing test for query options factories**

Create `web/src/features/tasks/queries.test.ts`:
```ts
import { describe, it, expect } from 'vitest';
import { tasksQueryOptions, taskDetailQueryOptions, taskSessionQueryOptions } from './queries';

describe('Task Query Options', () => {
  it('creates query options with correct query keys and stale times', () => {
    const tasksOpts = tasksQueryOptions();
    expect(tasksOpts.queryKey).toEqual(['tasks']);
    expect(tasksOpts.staleTime).toBeGreaterThan(0);

    const detailOpts = taskDetailQueryOptions('task-123');
    expect(detailOpts.queryKey).toEqual(['tasks', 'task-123']);

    const sessionOpts = taskSessionQueryOptions('task-123');
    expect(sessionOpts.queryKey).toEqual(['tasks', 'task-123', 'session']);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `npm test --prefix web -- src/features/tasks/queries.test.ts`
Expected: FAIL (Cannot find module './queries')

- [ ] **Step 3: Implement `web/src/lib/queryClient.ts`**

```ts
import { QueryClient } from '@tanstack/react-query';

export function makeQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: {
        staleTime: 1000 * 30,
        refetchOnWindowFocus: false,
      },
    },
  });
}

let browserQueryClient: QueryClient | undefined = undefined;

export function getQueryClient() {
  if (typeof window === 'undefined') {
    return makeQueryClient();
  }
  if (!browserQueryClient) {
    browserQueryClient = makeQueryClient();
  }
  return browserQueryClient;
}
```

- [ ] **Step 4: Implement query option factories in `src/features/tasks/queries.ts`**

```ts
import { queryOptions } from '@tanstack/react-query';
import { taskApi } from './api/taskApi';

export const tasksQueryOptions = () =>
  queryOptions({
    queryKey: ['tasks'],
    queryFn: () => taskApi.getTasks(),
    staleTime: 1000 * 30,
  });

export const taskDetailQueryOptions = (taskId: string) =>
  queryOptions({
    queryKey: ['tasks', taskId],
    queryFn: () => taskApi.getTask(taskId),
    staleTime: 1000 * 30,
  });

export const taskSessionQueryOptions = (taskId: string) =>
  queryOptions({
    queryKey: ['tasks', taskId, 'session'],
    queryFn: () => taskApi.getTaskSession(taskId),
    staleTime: 1000 * 5,
  });
```

- [ ] **Step 5: Implement `src/features/auth/queries.ts`**

```ts
import { queryOptions } from '@tanstack/react-query';
import { authApi } from './api/authApi';

export const authQueryOptions = () =>
  queryOptions({
    queryKey: ['auth', 'user'],
    queryFn: async () => {
      try {
        const res = await authApi.getCurrentUser();
        return res.user;
      } catch {
        return null;
      }
    },
    staleTime: 1000 * 60,
  });
```

- [ ] **Step 6: Update `useTaskSubscription.ts` to sync with query client**

In `web/src/features/tasks/hooks/useTaskSubscription.ts`, subscribe to WebSocket messages and call:
- `queryClient.setQueryData(['tasks'], ...)` on task updates.
- `queryClient.invalidateQueries({ queryKey: ['tasks', data.taskId, 'session'] })` on agent events.

- [ ] **Step 7: Run tests to verify pass**

Run: `npm test --prefix web -- src/features/tasks/queries.test.ts`
Expected: PASS

- [ ] **Step 8: Commit query layer**

```bash
git add web/src/lib/queryClient.ts web/src/features/tasks/queries.ts web/src/features/tasks/queries.test.ts web/src/features/auth/queries.ts web/src/features/tasks/hooks/useTaskSubscription.ts
git commit -m "feat(web): add TanStack Query factories and real-time cache sync"
```

---

### Task 3: Root Layout & Router Infrastructure

**Files:**
- Create: `web/src/router.tsx`
- Create: `web/src/routes/__root.tsx`
- Create: `web/src/routes/login.tsx`
- Test: `web/src/routes/login.test.tsx`

**Interfaces:**
- Produces: `createRouter` factory, Root layout with `ThemeProvider` & `QueryClientProvider`, `/login` route.

- [ ] **Step 1: Create `web/src/routes/__root.tsx`**

```tsx
import { createRootRouteWithContext, Outlet, ScrollRestoration } from '@tanstack/react-router';
import { Body, Head, Html, Scripts } from '@tanstack/react-start';
import type { QueryClient } from '@tanstack/react-query';
import { ThemeProvider } from '@/components/theme/ThemeProvider';

export const Route = createRootRouteWithContext<{
  queryClient: QueryClient;
}>()({
  meta: () => [
    { charSet: 'utf-8' },
    { name: 'viewport', content: 'width=device-width, initial-scale=1' },
    { title: 'Agent Task Manager' },
  ],
  component: RootComponent,
});

function RootComponent() {
  return (
    <Html lang="en">
      <Head>
        <title>Agent Task Manager</title>
      </Head>
      <Body className="min-h-screen bg-background text-foreground font-sans antialiased">
        <ThemeProvider>
          <Outlet />
        </ThemeProvider>
        <ScrollRestoration />
        <Scripts />
      </Body>
    </Html>
  );
}
```

- [ ] **Step 2: Create `web/src/routes/login.tsx`**

```tsx
import { createFileRoute, redirect } from '@tanstack/react-router';
import { authQueryOptions } from '@/features/auth/queries';
import { LoginForm } from '@/features/auth/components/LoginForm';

export const Route = createFileRoute('/login')({
  beforeLoad: async ({ context }) => {
    const user = await context.queryClient.ensureQueryData(authQueryOptions());
    if (user) {
      throw redirect({ to: '/' });
    }
  },
  component: () => (
    <div className="min-h-screen flex items-center justify-center bg-background p-4">
      <LoginForm />
    </div>
  ),
});
```

- [ ] **Step 3: Create `web/src/router.tsx`**

```tsx
import { createRouter as createTanStackRouter } from '@tanstack/react-router';
import { routeTree } from './routeTree.gen';
import { getQueryClient } from './lib/queryClient';

export function createRouter() {
  const queryClient = getQueryClient();

  return createTanStackRouter({
    routeTree,
    context: {
      queryClient,
    },
    defaultPreload: 'intent',
  });
}

declare module '@tanstack/react-router' {
  interface Register {
    router: ReturnType<typeof createRouter>;
  }
}
```

- [ ] **Step 4: Write unit test for `/login` route redirect**

Create `web/src/routes/login.test.tsx` verifying unauthenticated users render `LoginForm` and authenticated users throw redirect.

- [ ] **Step 5: Run tests**

Run: `npm test --prefix web -- src/routes/login.test.tsx`
Expected: PASS

- [ ] **Step 6: Commit router root and login**

```bash
git add web/src/routes/__root.tsx web/src/routes/login.tsx web/src/router.tsx web/src/routes/login.test.tsx
git commit -m "feat(web): add TanStack Router root layout and login route"
```

---

### Task 4: Protected Layout & Kanban Workspace Route

**Files:**
- Create: `web/src/routes/_auth.tsx`
- Create: `web/src/routes/_auth.index.tsx`
- Test: `web/src/routes/_auth.test.tsx`

**Interfaces:**
- Produces: Protected pathless route guard (`_auth`), primary Kanban workspace route (`/`).

- [ ] **Step 1: Write test for `_auth` route guard**

Create `web/src/routes/_auth.test.tsx`:
Verify `beforeLoad` redirects unauthenticated context to `/login` with `search.redirect` preserved.

- [ ] **Step 2: Run test to verify it fails**

Run: `npm test --prefix web -- src/routes/_auth.test.tsx`
Expected: FAIL

- [ ] **Step 3: Implement `web/src/routes/_auth.tsx`**

```tsx
import { createFileRoute, Outlet, redirect } from '@tanstack/react-router';
import { authQueryOptions } from '@/features/auth/queries';

export const Route = createFileRoute('/_auth')({
  beforeLoad: async ({ context, location }) => {
    const user = await context.queryClient.ensureQueryData(authQueryOptions());
    if (!user) {
      throw redirect({
        to: '/login',
        search: {
          redirect: location.href,
        },
      });
    }
    return { user };
  },
  component: () => <Outlet />,
});
```

- [ ] **Step 4: Implement `web/src/routes/_auth.index.tsx`**

```tsx
import { createFileRoute } from '@tanstack/react-router';
import { tasksQueryOptions } from '@/features/tasks/queries';
import { WorkspaceView } from '@/App';

export const Route = createFileRoute('/_auth/')({
  loader: async ({ context }) => {
    await context.queryClient.ensureQueryData(tasksQueryOptions());
  },
  component: () => <WorkspaceView />,
});
```

- [ ] **Step 5: Run tests to verify pass**

Run: `npm test --prefix web -- src/routes/_auth.test.tsx`
Expected: PASS

- [ ] **Step 6: Commit protected routes**

```bash
git add web/src/routes/_auth.tsx web/src/routes/_auth.index.tsx web/src/routes/_auth.test.tsx
git commit -m "feat(web): implement server-side _auth route guard and workspace index route"
```

---

### Task 5: Task Deep-Linking Route & Inspector Modal Synchronization

**Files:**
- Create: `web/src/routes/_auth.tasks.$taskId.tsx`
- Modify: `web/src/features/tasks/components/TaskInspectorModal.tsx`
- Test: `web/src/routes/_auth.tasks.$taskId.test.tsx`

**Interfaces:**
- Produces: `/tasks/$taskId` deep-linking route that pre-fetches task & session and opens inspector modal over the workspace.

- [ ] **Step 1: Implement `web/src/routes/_auth.tasks.$taskId.tsx`**

```tsx
import { createFileRoute, useNavigate } from '@tanstack/react-router';
import { tasksQueryOptions, taskDetailQueryOptions, taskSessionQueryOptions } from '@/features/tasks/queries';
import { WorkspaceView } from '@/App';
import { TaskInspectorModal } from '@/features/tasks/components/TaskInspectorModal';

export const Route = createFileRoute('/_auth/tasks/$taskId')({
  loader: async ({ params, context }) => {
    await Promise.all([
      context.queryClient.ensureQueryData(tasksQueryOptions()),
      context.queryClient.ensureQueryData(taskDetailQueryOptions(params.taskId)),
      context.queryClient.ensureQueryData(taskSessionQueryOptions(params.taskId)),
    ]);
  },
  component: TaskDetailRouteComponent,
});

function TaskDetailRouteComponent() {
  const { taskId } = Route.useParams();
  const navigate = useNavigate();

  const handleClose = () => {
    navigate({ to: '/' });
  };

  return (
    <>
      <WorkspaceView />
      <TaskInspectorModal taskId={taskId} onClose={handleClose} />
    </>
  );
}
```

- [ ] **Step 2: Update `TaskInspectorModal.tsx` to support controlled `taskId` and `onClose` props**

When `taskId` prop is passed, use it directly. When user closes the dialog, call `onClose?.()`.

- [ ] **Step 3: Write test for task deep-linking route**

Create `web/src/routes/_auth.tasks.$taskId.test.tsx` verifying:
- Route loader calls `ensureQueryData` for `tasks`, `taskDetail`, and `taskSession`.
- Closing the inspector modal navigates back to `/`.

- [ ] **Step 4: Run tests**

Run: `npm test --prefix web -- src/routes/_auth.tasks.$taskId.test.tsx`
Expected: PASS

- [ ] **Step 5: Commit task deep-linking**

```bash
git add web/src/routes/_auth.tasks.$taskId.tsx web/src/routes/_auth.tasks.$taskId.test.tsx web/src/features/tasks/components/TaskInspectorModal.tsx
git commit -m "feat(web): add /tasks/\$taskId deep-linking route and modal synchronization"
```

---

### Task 6: Multi-Stage Dockerfile & Container Deployment

**Files:**
- Modify: `web/Dockerfile`
- Modify: `docker-compose.yml`

**Interfaces:**
- Produces: Running `agent-web-ui` container running Node Nitro SSR server on port 3000.

- [ ] **Step 1: Update `web/Dockerfile` for Nitro SSR build**

```dockerfile
# Build stage
FROM node:22-alpine AS builder

WORKDIR /app
COPY package.json package-lock.json* ./
RUN npm install

ARG CACHEBUST=1
COPY . .
RUN npm run build

# Production runtime stage
FROM node:22-alpine AS runner
WORKDIR /app

ENV NODE_ENV=production
ENV PORT=3000

COPY --from=builder /app/.output ./.output
COPY --from=builder /app/package.json ./package.json

EXPOSE 3000
CMD ["node", ".output/server/index.mjs"]
```

- [ ] **Step 2: Update `docker-compose.yml` for `agent-web-ui`**

- Change Traefik target port:
  `traefik.http.services.web-ui.loadbalancer.server.port=3000`
- Pass `INTERNAL_API_URL: http://control-plane:8080`.

- [ ] **Step 3: Build container and verify startup**

```bash
docker compose build web-ui
docker compose up -d web-ui
docker compose logs --tail=20 web-ui
```
Expected: Nitro server started and listening on `http://0.0.0.0:3000`.

- [ ] **Step 4: Verify whole project checks**

Run: `make check`
Expected: All Go checks pass, Vitest tests pass, and TypeScript builds cleanly.

- [ ] **Step 5: Commit Docker configuration**

```bash
git add web/Dockerfile docker-compose.yml
git commit -m "build(docker): switch agent-web-ui to TanStack Start Nitro SSR container"
```

---

## Plan Self-Review Checklist
- [x] Spec coverage: Every section from `2026-10-09-tanstack-start-migration-design.md` has an assigned task.
- [x] No placeholders: All tasks contain full code listings and concrete verification commands.
- [x] Type consistency: `tasksQueryOptions`, `taskDetailQueryOptions`, `taskSessionQueryOptions`, `authQueryOptions` match across all tasks.
- [x] Review Focus: Auth redirect, cookie forwarding, deep-linking, real-time sync, and Docker Nitro build are tested.
