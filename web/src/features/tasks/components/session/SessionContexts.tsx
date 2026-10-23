import { createContext, use, useMemo, type ReactNode } from 'react';
import { useStore } from 'zustand';
import type { SessionChild } from '../../schemas/taskSchemas';
import type { ToolSelectionState, ToolSelectionStore } from '../../hooks/useToolSelection';

// The transcript's leaf components read what they need from these contexts instead of receiving it through every
// level of the list. Each context holds one kind of value, so a change re-renders only the components that use it.

const ToolSelectionContext = createContext<ToolSelectionStore | null>(null);
const SubagentsContext = createContext<SessionChild[]>([]);

export interface SteerContextValue {
  submitAnswer: (answerText: string) => Promise<void>;
  isSubmitting: boolean;
  isSteeringActive: boolean;
}
const SteerContext = createContext<SteerContextValue | null>(null);

interface SessionContextsProps {
  toolStore: ToolSelectionStore;
  subagents: SessionChild[];
  submitAnswer: SteerContextValue['submitAnswer'];
  isSubmitting: boolean;
  isSteeringActive: boolean;
  children: ReactNode;
}

export function SessionContexts({ toolStore, subagents, submitAnswer, isSubmitting, isSteeringActive, children }: SessionContextsProps) {
  const steer = useMemo(() => ({ submitAnswer, isSubmitting, isSteeringActive }), [submitAnswer, isSubmitting, isSteeringActive]);
  return (
    <ToolSelectionContext value={toolStore}>
      <SubagentsContext value={subagents}>
        <SteerContext value={steer}>{children}</SteerContext>
      </SubagentsContext>
    </ToolSelectionContext>
  );
}

export function useToolSelectionStore<T>(selector: (state: ToolSelectionState) => T): T {
  const store = use(ToolSelectionContext);
  if (!store) throw new Error('useToolSelectionStore must be used inside SessionContexts');
  return useStore(store, selector);
}

export const useSubagents = () => use(SubagentsContext);

export function useSteer(): SteerContextValue {
  const steer = use(SteerContext);
  if (!steer) throw new Error('useSteer must be used inside SessionContexts');
  return steer;
}
