import { create } from 'zustand';
import { useShallow } from 'zustand/react/shallow';
import { taskApi } from '../api/taskApi';
import type { TaskDTO as Task, CreateTaskInput as CreateTaskRequest, TaskStatus } from '../schemas/taskSchemas';

interface TaskState {
  tasks: Record<string, Task>;
  taskIds: string[];
  selectedTaskId: string | null;
  isLoading: boolean;
  error: string | null;

  setTasksData: (taskList: Task[]) => void;
  fetchTasks: (projectId?: string) => Promise<void>;
  createTask: (req: CreateTaskRequest) => Promise<Task>;
  updateTask: (task: Task) => void;
  updateTaskStatus: (taskId: string, status: TaskStatus) => void;
  selectTask: (taskId: string | null) => void;
  cancelTask: (taskId: string) => Promise<void>;
}

export const useTaskStore = create<TaskState>((set, get) => ({
  tasks: {},
  taskIds: [],
  selectedTaskId: null,
  isLoading: false,
  error: null,

  setTasksData: (taskList: Task[]) => {
    const tasks: Record<string, Task> = {};
    const taskIds: string[] = [];
    for (const t of taskList) {
      tasks[t.id] = t;
      taskIds.push(t.id);
    }
    set({ tasks, taskIds, isLoading: false, error: null });
  },

  fetchTasks: async (projectId) => {
    set({ isLoading: true, error: null });
    try {
      const taskList = await taskApi.getTasks(projectId);
      const tasks: Record<string, Task> = {};
      const taskIds: string[] = [];

      for (const t of taskList) {
        tasks[t.id] = t;
        taskIds.push(t.id);
      }

      set({ tasks, taskIds, isLoading: false, error: null });
    } catch (err) {
      set({
        isLoading: false,
        error: err instanceof Error ? err.message : 'Failed to fetch tasks',
      });
    }
  },

  createTask: async (req) => {
    const newTask = await taskApi.createTask(req);
    set((state) => ({
      tasks: { ...state.tasks, [newTask.id]: newTask },
      taskIds: [newTask.id, ...state.taskIds],
    }));
    return newTask;
  },

  updateTask: (task) => {
    set((state) => ({
      tasks: { ...state.tasks, [task.id]: task },
    }));
  },

  updateTaskStatus: (taskId, status) => {
    const existing = get().tasks[taskId];
    if (existing) {
      if (existing.status === status) return;
      set((state) => ({
        tasks: {
          ...state.tasks,
          [taskId]: {
            ...existing,
            status,
            updated_at: new Date().toISOString(),
          },
        },
      }));
    } else {
      // Re-fetch if newly created elsewhere
      get().fetchTasks();
    }
  },

  selectTask: (taskId) => {
    set({ selectedTaskId: taskId });
  },

  cancelTask: async (taskId) => {
    await taskApi.cancelTask(taskId);
    const existing = get().tasks[taskId];
    if (existing) {
      set((state) => ({
        tasks: {
          ...state.tasks,
          [taskId]: { ...existing, status: 'cancelled' },
        },
      }));
    }
  },
}));

// Fine-grained selectors avoiding parent re-renders:
// 1. Selector for a single task: only re-renders the specific component rendering this task ID
export const useTask = (id: string): Task | undefined =>
  useTaskStore((state) => state.tasks[id]);

// 2. Selector for task IDs matching a status lane
export const useTaskIdsByStatus = (status: TaskStatus) =>
  useTaskStore(
    useShallow((state) =>
      state.taskIds.filter((id) => state.tasks[id]?.status === status)
    )
  );

// 3. Selected task
export const useSelectedTask = (): Task | null =>
  useTaskStore((state) => (state.selectedTaskId ? state.tasks[state.selectedTaskId] ?? null : null));

export const useSelectedTaskId = () =>
  useTaskStore((state) => state.selectedTaskId);

export const useTasksLoading = () =>
  useTaskStore((state) => state.isLoading);
