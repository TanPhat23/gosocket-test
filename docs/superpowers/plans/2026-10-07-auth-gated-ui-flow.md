# Auth-Gated UI Flow Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Require users to authenticate before accessing the Agent Task Manager workspace, rendering a branded dark-mode login screen when unauthenticated and gating WebSocket/API polling to active sessions.

**Architecture:** Introduce a dedicated `LoginScreen` component for unauthenticated visitors and an auth loading splash screen. In `App.tsx`, check `user` and `isLoading` from `useAuthStore`; render the login screen when `user === null` and mount the workspace, metrics, Kanban board, and real-time WebSocket subscriptions only when `user` is authenticated.

**Tech Stack:** React 19, TypeScript, Tailwind CSS, Base UI, Lucide React, Zustand, Vitest, React Testing Library.

**Spec:** In-chat approved design: Branded login screen with ATM logo, status badges, SSO redirect button (`/api/v1/auth/login`), demo credentials helper (`admin@example.com` / `password123`), smooth loading state, and complete auth-gating of workspace data queries and WebSocket connections.

## Global Constraints

- Preserve all existing dark/light theme tokens and Base UI design system styling.
- Prevent unauthenticated background network traffic (no `fetchTasks()`, `fetchTeams()`, or `useTaskSubscription()` when unauthenticated).
- Maintain 100% test pass rate across `web` vitest test suite and clean Vite production build.

## Review Focus

- Initial session loading: Ensure loading splash screen renders while `isLoading` is true, avoiding flash of unauthenticated UI.
- Unauthenticated state: Ensure `LoginScreen` renders cleanly with SSO button linking to `/api/v1/auth/login`.
- Authenticated transition: Ensure when `user` is populated, the workspace, header, Kanban board, and inspector modals mount seamlessly.
- Sign out transition: Ensure clicking Sign Out in `UserNav` returns user to the `LoginScreen`.
- Error resilience: If `fetchUser` fails (e.g. 401 Unauthorized), `user` remains `null`, `isLoading` resolves to `false`, and `LoginScreen` is rendered without throwing.

---

### Task 1: Create Branded `LoginScreen` Component

**Files:**
- Create: `web/src/features/auth/components/LoginScreen.tsx`
- Test: `web/src/features/auth/components/LoginScreen.test.tsx`

**Interfaces:**
- Consumes: Base UI `Button`, Lucide icons (`LogIn`, `ShieldCheck`, `Terminal`, `Cpu`, `Sparkles`), Tailwind tokens.
- Produces: `export const LoginScreen: React.FC`

- [ ] **Step 1: Write test for LoginScreen**
Create `web/src/features/auth/components/LoginScreen.test.tsx` testing that the logo, title ("Autonomous Coding Control Plane"), sign-in button linking to `/api/v1/auth/login`, and demo credentials hint render correctly.

- [ ] **Step 2: Run test to verify it fails**
Run: `cd web && npx vitest run src/features/auth/components/LoginScreen.test.tsx`
Expected: FAIL (module not found)

- [ ] **Step 3: Implement `LoginScreen` component**
Create `web/src/features/auth/components/LoginScreen.tsx` with:
- Centered grid with glowing subtle ambient gradient
- Agent Task Manager badge & terminal branding
- "Autonomous Coding Control Plane" headline & description
- "Sign in with SSO" button with link/redirect to `/api/v1/auth/login`
- Local developer demo credential hint card (`admin@example.com` / `password123` & `developer@example.com` / `password123`)

- [ ] **Step 4: Run test to verify it passes**
Run: `cd web && npx vitest run src/features/auth/components/LoginScreen.test.tsx`
Expected: PASS

---

### Task 2: Gate `App.tsx` Behind Authentication Guard

**Files:**
- Modify: `web/src/App.tsx`
- Test: `web/src/App.test.tsx`

**Interfaces:**
- Consumes: `useAuthStore` (`user`, `isLoading`, `fetchUser`), `useTaskStore`, `useTeamStore`, `useTaskSubscription`, `LoginScreen`.
- Produces: Conditional workspace rendering based on `isLoading` and `user`.

- [ ] **Step 1: Write unit test for `App` conditional rendering**
Create `web/src/App.test.tsx` testing:
1. Loading state renders loading spinner/badge when `isLoading: true`.
2. Unauthenticated state renders `LoginScreen` when `isLoading: false, user: null`.
3. Authenticated state renders Header, Workspace, and Kanban board when `isLoading: false, user: { ... }`.

- [ ] **Step 2: Run test to verify it fails or tests missing behavior**
Run: `cd web && npx vitest run src/App.test.tsx`

- [ ] **Step 3: Implement Auth-Gated Flow in `App.tsx`**
Refactor `App.tsx` so:
- Only `fetchUser()` is invoked on mount.
- While `isLoading === true`, display loading splash screen.
- If `!user`, return `<LoginScreen />`.
- When `user` is authenticated, render the workspace layout and invoke `fetchTasks()`, `fetchTeams()`, and `useTaskSubscription()`. Note: extract the authenticated workspace into an inner component or hook so hooks aren't conditionally called.

- [ ] **Step 4: Run test to verify it passes**
Run: `cd web && npx vitest run src/App.test.tsx`
Expected: PASS

---

### Task 3: Full Suite Verification & Live Container Build

**Files:**
- Test all: `cd web && npm test`
- Build frontend: `cd web && npm run build`
- Rebuild docker container: `docker compose build web-ui && docker compose up -d web-ui`

- [ ] **Step 1: Run complete web unit test suite**
Run: `cd web && npm test`
Expected: 100% pass

- [ ] **Step 2: Run Vite build**
Run: `cd web && npm run build`
Expected: Exit code 0, no type or bundle errors

- [ ] **Step 3: Rebuild and restart web-ui container**
Run: `docker compose build web-ui && docker compose up -d web-ui`
Expected: Running healthy on port 80
