import React, { useState } from 'react';
import { useStore } from 'zustand';
import { ArrowDown } from 'lucide-react';
import { Button } from '@/components/ui/button';
import type { SessionChild } from '../schemas/taskSchemas';
import { useSessionActions } from '../hooks/useSessionActions';
import { useSessionLiveUpdates } from '../hooks/useSessionLiveUpdates';
import { useSessionQuery } from '../hooks/useSessionQuery';
import { useSessionTranscript } from '../hooks/useSessionTranscript';
import { useSteerDraft } from '../hooks/useSteerDraft';
import { useSteerSubmit } from '../hooks/useSteerSubmit';
import { createToolSelectionStore } from '../hooks/useToolSelection';
import { useTranscriptScroll } from '../hooks/useTranscriptScroll';
import { parseSessionMessageContent, type ParsedMessageData } from '../utils/sessionMessage';
import { SessionContexts } from './session/SessionContexts';
import { SessionInfoBar } from './session/SessionInfoBar';
import { SessionStateBanner } from './session/SessionStateBanner';
import { SessionTranscriptMessages } from './session/SessionTranscriptMessages';
import { SteerComposer } from './session/SteerComposer';
import { ToolInspector } from './session/ToolInspector';

export { parseSessionMessageContent };
export type { ParsedMessageData };

const NO_SUBAGENTS: SessionChild[] = [];

interface AgentSessionViewProps {
  taskId: string;
  defaultSessionId?: string | null;
  isActive?: boolean;
}

// The agent's session for a task: its transcript, a box to steer it from, and an inspector for any tool call.
export const AgentSessionView: React.FC<AgentSessionViewProps> = ({ taskId, defaultSessionId, isActive = true }) => {
  const { session, error, isUnavailable, isSimulated, loading, refresh, updateSession, setSession } = useSessionQuery(taskId);
  const { transcript, toolCalls } = useSessionTranscript(session?.messages);
  const scroll = useTranscriptScroll(isActive, session?.messages);
  const draft = useSteerDraft();
  const actions = useSessionActions(taskId, refresh);
  const [toolStore] = useState(createToolSelectionStore);
  const selected = useStore(toolStore, (s) => s.selection);
  const steer = useSteerSubmit({
    taskId,
    draft,
    updateSession,
    refresh,
    scrollToEnd: () => scroll.endRef.current?.scrollIntoView({ behavior: 'smooth' }),
  });

  useSessionLiveUpdates({
    taskId,
    status: session?.status,
    isSteeringActive: steer.isSteeringActive,
    setSession,
    refresh,
    onSteeringSettled: () => steer.setIsSteeringActive(false),
    onPolled: () => scroll.endRef.current?.scrollIntoView({ behavior: 'smooth' }),
  });

  const sessionId = session?.session_id || defaultSessionId || 'Unavailable';
  const readOnly = isUnavailable || isSimulated;
  const selectedTool = selected ? toolCalls.get(selected.key) : undefined;

  const bannerState = loading && !session ? 'loading' : isSimulated ? 'simulated' : isUnavailable ? 'unavailable' : error ? 'error' : !session?.messages?.length ? 'empty' : null;

  return (
    <SessionContexts
      toolStore={toolStore}
      subagents={session?.children ?? NO_SUBAGENTS}
      submitAnswer={steer.submitAnswer}
      isSubmitting={steer.isSubmitting}
      isSteeringActive={steer.isSteeringActive}
    >
      <div className="flex flex-col h-full overflow-hidden bg-background text-foreground space-y-2.5">
        <SessionInfoBar
          session={session}
          sessionId={sessionId}
          showStatus={!readOnly}
          loading={loading}
          controlsDisabled={readOnly}
          actions={actions}
          onRefresh={() => void refresh()}
        />

        <div
          className={
            selected
              ? 'flex-1 min-h-0 grid grid-cols-1 lg:grid-cols-12 gap-3.5 h-full overflow-hidden relative'
              : 'flex-1 min-h-0 flex flex-col w-full h-full overflow-hidden relative'
          }
        >
          <div
            className={
              selected
                ? 'hidden lg:flex lg:col-span-6 flex-col h-full min-h-0 gap-3 overflow-hidden'
                : 'w-full flex flex-col h-full min-h-0 gap-3 overflow-hidden'
            }
          >
            <div className="flex-1 min-h-0 flex flex-col rounded-lg border border-border bg-card/60 overflow-hidden relative">
              {bannerState ? (
                <SessionStateBanner state={bannerState} error={error} onRetry={() => void refresh()} />
              ) : (
                <div
                  ref={scroll.containerRef}
                  onScroll={scroll.handleScroll}
                  className="flex-1 overflow-y-auto px-3 py-4 space-y-5 sm:px-4 sm:py-5"
                  data-testid="session-transcript-container"
                >
                  <SessionTranscriptMessages transcript={transcript} endRef={scroll.endRef} />
                </div>
              )}

              {scroll.showScrollBottomBtn && (
                <Button
                  type="button"
                  variant="secondary"
                  size="sm"
                  onClick={() => scroll.scrollToBottom(true)}
                  className="absolute bottom-4 right-4 z-20 rounded-full shadow-lg border border-border bg-background/95 hover:bg-muted text-xs font-medium gap-1.5 transition-all animate-in fade-in slide-in-from-bottom-2"
                  data-testid="scroll-to-bottom-btn"
                  aria-label="Scroll to bottom"
                >
                  <ArrowDown className="size-3.5 text-primary" />
                  <span>Scroll to bottom</span>
                </Button>
              )}
            </div>

            <SteerComposer
              draft={draft}
              disabled={readOnly}
              isSubmitting={steer.isSubmitting}
              isWorking={steer.isSteeringActive}
              onSubmit={steer.send}
            />
          </div>

          {selected && selectedTool && (
            <ToolInspector
              key={`${selected.key}-${selected.nonce}`}
              tc={selectedTool.tc}
              msg={selectedTool.msg}
              openInputTab={selected.openInputTab}
              onInsertFix={draft.setPrompt}
            />
          )}
        </div>
      </div>
    </SessionContexts>
  );
};
