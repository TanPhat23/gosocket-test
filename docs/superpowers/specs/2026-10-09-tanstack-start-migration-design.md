# Design Document: Migration to TanStack Start (SSR)

## 1. Executive Summary

This specification outlines the migration of the Agent Task Manager web frontend from a single-page application (SPA) served by Nginx to a full-stack Server-Side Rendered (SSR) application using **TanStack Start** (`@tanstack/react-start` + `@tanstack/react-router` + `@tanstack/react-query`).

### Core Objectives
1. **Server-Side Route Protection & Instant Hydration**: Eliminate client-side "Verifying session..." flashes by performing authentication checks in the route `beforeLoad` hook on the SSR server.
2. **Deep Linking & Type-Safe URL Routing**: Enable shareable URLs (`/tasks/$taskId`) that open the Kanban workspace with the target task inspector modal pre-loaded and opened.
3. **Unified TanStack Data Architecture**: Standardize data fetching and cache invalidation on `@tanstack/react-query` as the single source of truth, integrated with real-time WebSocket event updates and `@tanstack/react-form`.
4. **Nitro Server Deployment**: Replace the Alpine Nginx static server with a Node.js Nitro container integrated with the existing Traefik reverse proxy and Go Fiber control plane.

---

## 2. System Architecture & Topology

### 2.1 Deployment Topology
```
                  ┌──────────────────────────────┐
                  │    Traefik Reverse Proxy     │
                  │        (Port 80/443)         │
                  └──────────────┬───────────────┘
                                 │
           ┌─────────────────────┴─────────────────────┐
           │                                           │
  (PathPrefix: /api/v1, /ws)                 (All other web paths: /, /tasks/*)
           │                                           │
           ▼                                           ▼
┌───────────────────────────┐             ┌─────────────────────────────┐
│ agent-control-plane:8080  │ ◄────────── │     agent-web-ui:3000       │
│      (Go Fiber API)       │ (SSR fetch) │ (TanStack Start Nitro Node) │
└───────────────────────────┘             └─────────────────────────────┘
```

1. **External Ingress**:
   - Traefik routes `/api/v1/*` and `/ws` directly to `agent-control-plane:8080`.
   - Traefik routes web routes (`/`, `/tasks/*`, `/login`, `/_build/*`) to `agent-web-ui:3000`.
2. **Internal SSR Communications**:
   - The TanStack Start SSR server forwards client cookies/headers to `http://control-plane:8080` over the internal Docker network during route loader execution.
3. **Client-Side Real-Time Hydration**:
   - Once hydrated, the browser connects directly to `ws://[host]/ws` for real-time task status transitions, terminal log streams, and agent events.

---

## 3. Package Dependencies & Build Scaffolding

### 3.1 Dependencies
In `web/package.json`:
- **New Core Dependencies**:
  - `@tanstack/react-start`: `^1.168.0`
  - `@tanstack/react-router`: `^1.168.0`
  - `@tanstack/react-query`: `^5.67.0`
  - `@tanstack/react-query-devtools`: `^5.67.0`
- **Retained UI & Utilities**:
  - `react`: `^19.0.0`, `react-dom`: `^19.0.0`
  - `@tanstack/react-form`: `^1.33.5`
  - `@base-ui/react`: `^1.8.0`, `@shadcn/react`: `^0.3.1`
  - `@tailwindcss/vite`: `^4.0.9`, `tailwindcss`: `^4.0.9`
  - `lucide-react`, `class-variance-authority`, `clsx`, `cn`, `zod`
  - `@xterm/xterm`, `@xterm/addon-fit`

### 3.2 Build Configuration (`app.config.ts`)
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

---

## 4. Route Hierarchy & File Structure

```
web/
├── app.config.ts                     # TanStack Start configuration
├── src/
│   ├── router.tsx                    # createRouter entry point
│   ├── routes/
│   │   ├── __root.tsx                # Shell layout, QueryClientProvider, ThemeProvider
│   │   ├── login.tsx                 # Public authentication route
│   │   ├── _auth.tsx                 # Protected route layout (beforeLoad auth guard)
│   │   ├── _auth.index.tsx           # Primary Kanban workspace route (/)
│   │   └── _auth.tasks.$taskId.tsx   # Direct task deep link route (/tasks/$taskId)
│   ├── features/
│   │   ├── auth/queries.ts           # authQueryOptions
│   │   ├── tasks/queries.ts          # tasksQueryOptions, taskDetailQueryOptions, taskSessionQueryOptions
│   │   └── teams/queries.ts          # teamsQueryOptions
│   ├── lib/
│   │   ├── api.ts                    # SSR-aware fetch client (forwards cookie on server)
│   │   └── queryClient.ts            # QueryClient factory
│   └── components/                   # shadcn UI & layout components
```

### 4.1 Root Route (`src/routes/__root.tsx`)
- Renders `<html>`, `<head>`, `<HeadContent />`, `<body>`, `<Outlet />`, and `<Scripts />`.
- Providers:
  - `QueryClientProvider` with dehydrated SSR cache.
  - `ThemeProvider` to prevent theme flashing.
  - Global error boundaries and 404 handlers.

### 4.2 Server-Side Auth Guard (`src/routes/_auth.tsx`)
```tsx
export const Route = createFileRoute('/_auth')({
  beforeLoad: async ({ context, location }) => {
    const user = await context.queryClient.ensureQueryData(authQueryOptions());
    if (!user) {
      throw redirect({
        to: '/login',
        search: { redirect: location.href },
      });
    }
    return { user };
  },
  component: AuthLayout,
});
```

### 4.3 Task Deep Link Route (`src/routes/_auth.tasks.$taskId.tsx`)
```tsx
export const Route = createFileRoute('/_auth/tasks/$taskId')({
  loader: async ({ params, context }) => {
    await Promise.all([
      context.queryClient.ensureQueryData(tasksQueryOptions()),
      context.queryClient.ensureQueryData(taskDetailQueryOptions(params.taskId)),
      context.queryClient.ensureQueryData(taskSessionQueryOptions(params.taskId)),
    ]);
  },
  component: TaskRouteComponent,
});
```
- In `TaskRouteComponent`:
  - Renders the `WorkspaceView` in the background with tasks pre-hydrated.
  - Automatically opens the `TaskInspectorModal` for `params.taskId`.
  - Closing the modal calls `navigate({ to: '/' })`.

---

## 5. Data Layer & Real-Time Sync

### 5.1 Query Options Factories (`src/features/tasks/queries.ts`)
```tsx
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

### 5.2 Real-Time WebSocket Cache Integration (`useTaskSubscription`)
Browser connects to `/ws` on mount.
- On `task_status_changed`: Optimistically patches `['tasks']` query cache.
- On `agent_event` / `tool_use`: Invalidates or patches `['tasks', taskId, 'session']` query cache.
- On steering submit: Dispatches `useSteerSessionMutation` with optimistic message append and automatic query refetch on settlement.

### 5.3 Forms with `@tanstack/react-form`
- `LoginForm`: Uses `useForm` with validation and redirects to `search.redirect || '/'`.
- `TaskModal`: Uses `useForm` for creating new tasks with Zod schema validation.
- `AgentQuestionnaire`: Preserves shadcn `Questionnaire` UI, connected to `useSteerSessionMutation`.

---

## 6. Docker & Deployment Specifications

### 6.1 Multi-Stage Dockerfile (`web/Dockerfile`)
```dockerfile
FROM node:22-alpine AS builder
WORKDIR /app
COPY package.json package-lock.json* ./
RUN npm install
COPY . .
RUN npm run build

FROM node:22-alpine AS runner
WORKDIR /app
ENV NODE_ENV=production
ENV PORT=3000
COPY --from=builder /app/.output ./.output
COPY --from=builder /app/package.json ./package.json

EXPOSE 3000
CMD ["node", ".output/server/index.mjs"]
```

### 6.2 Docker Compose Update (`docker-compose.yml`)
- Update `agent-web-ui`:
  - Expose port `3000` internally.
  - Update Traefik service port to `3000`.
  - Pass environment variable `INTERNAL_API_URL=http://control-plane:8080`.

---

## 7. Verification & Quality Gates

1. **Automated Unit Tests**:
   - `npm test --prefix web` passes all component, questionnaire, and route suites.
2. **Build Integrity**:
   - `npm run build --prefix web` compiles Nitro server to `.output/server/index.mjs` cleanly with zero TypeScript errors.
3. **Whole System Integration**:
   - `make check` passes Go staticcheck, Go vet, Go test suites, and frontend verification.
4. **Live Verification**:
   - `docker compose up -d web-ui` starts healthy on port 3000.
   - Visiting `/` pre-renders workspace or redirects to `/login`.
   - Visiting `/tasks/:id` pre-renders workspace and opens the task inspector modal directly.
