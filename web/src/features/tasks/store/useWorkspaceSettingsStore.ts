import { create } from 'zustand';
import { persist } from 'zustand/middleware';
import { taskApi } from '../api/taskApi';

export interface ModelOption {
  id: string;
  name: string;
  provider: string;
  active?: boolean;
}

export const DEFAULT_OPENCODE_MODELS: ModelOption[] = [
  { id: 'google/gemini-3.8-flash', name: 'Gemini 3.8 Flash (Active)', provider: 'google', active: true },
  { id: 'google/gemini-2.5-pro', name: 'Gemini 2.5 Pro', provider: 'google' },
  { id: 'anthropic/claude-sonnet-4-5', name: 'Claude Sonnet 4.5', provider: 'anthropic' },
  { id: 'anthropic/claude-opus-4-6', name: 'Claude Opus 4.6', provider: 'anthropic' },
  { id: 'openai/gpt-5.5', name: 'GPT-5.5', provider: 'openai' },
  { id: 'openai/gpt-5.6-luna', name: 'GPT-5.6 Luna', provider: 'openai' },
  { id: 'opencode-go/qwen3.8-max', name: 'Qwen 3.8 Max', provider: 'opencode-go' },
  { id: 'opencode-go/deepseek-v4-pro', name: 'DeepSeek V4 Pro', provider: 'opencode-go' },
  { id: 'opencode/big-pickle', name: 'Big Pickle', provider: 'opencode' },
];

export interface WorkspaceSettingsState {
  defaultModel: string;
  defaultPriority: number;
  searchQuery: string;
  priorityFilter: string; // 'all' | 'high' | 'normal' | 'low'
  availableModels: ModelOption[];

  setDefaultModel: (model: string) => void;
  setDefaultPriority: (priority: number) => void;
  setSearchQuery: (query: string) => void;
  setPriorityFilter: (filter: string) => void;
  setAvailableModels: (models: ModelOption[]) => void;
  fetchModels: () => Promise<void>;
}

export const useWorkspaceSettingsStore = create<WorkspaceSettingsState>()(
  persist(
    (set) => ({
      defaultModel: 'google/gemini-3.8-flash',
      defaultPriority: 50,
      searchQuery: '',
      priorityFilter: 'all',
      availableModels: DEFAULT_OPENCODE_MODELS,

      setDefaultModel: (defaultModel) => set({ defaultModel }),
      setDefaultPriority: (defaultPriority) => set({ defaultPriority }),
      setSearchQuery: (searchQuery) => set({ searchQuery }),
      setPriorityFilter: (priorityFilter) => set({ priorityFilter }),
      setAvailableModels: (availableModels) => set({ availableModels }),

      fetchModels: async () => {
        try {
          const models = await taskApi.getModels();
          if (models && models.length > 0) {
            set({ availableModels: models });
          }
        } catch {
          // Fallback to default models
        }
      },
    }),
    {
      name: 'atm-workspace-settings',
      partialize: (state) => ({
        defaultModel: state.defaultModel,
        defaultPriority: state.defaultPriority,
      }),
    }
  )
);
