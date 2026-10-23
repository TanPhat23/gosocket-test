import { createStore, type StoreApi } from 'zustand';

export interface ToolSelection {
  key: string;
  // Opens the inspector on the input tab, for a subagent that has no result yet.
  openInputTab: boolean;
  // Distinguishes selecting the same tool again, which resets the inspector's tab and views.
  nonce: number;
}

export interface ToolSelectionState {
  // The tool call the inspector shows, keyed `${msgIdx}-${tcIdx}`.
  selection: ToolSelection | null;
  // The tool calls expanded inline in the transcript.
  expandedTools: Record<string, boolean>;
  toggleToolExpanded: (key: string) => void;
  selectTool: (key: string, openInputTab: boolean) => void;
  clearSelection: () => void;
}

export type ToolSelectionStore = StoreApi<ToolSelectionState>;

// A store rather than component state: each tool card subscribes to just its own expanded and selected flags, so
// selecting or expanding one tool re-renders that card and not the whole transcript.
export function createToolSelectionStore(): ToolSelectionStore {
  return createStore<ToolSelectionState>((set) => ({
    selection: null,
    expandedTools: {},
    toggleToolExpanded: (key) => set((s) => ({ expandedTools: { ...s.expandedTools, [key]: !s.expandedTools[key] } })),
    selectTool: (key, openInputTab) => set((s) => ({ selection: { key, openInputTab, nonce: (s.selection?.nonce ?? 0) + 1 } })),
    clearSelection: () => set({ selection: null }),
  }));
}
