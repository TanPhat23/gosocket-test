import { useState } from 'react';
import { Activity, AlertCircle, Bot, CornerDownLeft, FileCode, Terminal, X } from 'lucide-react';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import type { SessionMessage, SessionToolCall } from '../../schemas/taskSchemas';
import { formatTimestamp, toDisplayText } from '../../utils/sessionFormat';
import { findSubagentChild, subagentSteps, subagentTranscript } from '../../utils/subagentActivity';
import { useSubagents, useToolSelectionStore } from './SessionContexts';
import { InspectorInputPane } from './InspectorInputPane';
import { InspectorOutputPane } from './InspectorOutputPane';
import { SubagentActivityTimeline } from './SubagentActivityTimeline';

type InspectorTab = 'activity' | 'output' | 'input';
type TextView = 'markdown' | 'raw';

interface ToolInspectorProps {
  tc: SessionToolCall;
  msg: SessionMessage;
  // Opens on the input tab; otherwise on a subagent's activity when it has any, and on the output when not.
  openInputTab: boolean;
  onInsertFix: (text: string) => void;
}

function ExitBadge({ tc, isSuccess }: { tc: SessionToolCall; isSuccess: boolean }) {
  const base = 'font-mono text-[9px] px-1.5 py-0';
  if (tc.status === 'running') {
    return (
      <Badge variant="outline" className={`${base} text-sky-500 border-sky-500/30 flex items-center gap-1`}>
        <span className="size-1.5 rounded-full bg-sky-500 animate-pulse" />
        running
      </Badge>
    );
  }
  if (!isSuccess) {
    return (
      <Badge variant="outline" className={`${base} bg-rose-500/15 text-rose-600 dark:text-rose-400 border-rose-500/40`}>
        exit {tc.exit_code !== undefined && tc.exit_code !== 0 ? tc.exit_code : 1}
      </Badge>
    );
  }
  return (
    <Badge variant="outline" className={`${base} bg-emerald-500/15 text-emerald-600 dark:text-emerald-400 border-emerald-500/30`}>
      exit {tc.exit_code ?? 0}
    </Badge>
  );
}

function TabButton({ active, onClick, icon, label, testId }: { active: boolean; onClick: () => void; icon: React.ReactNode; label: string; testId?: string }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`px-2.5 py-1 rounded text-[11px] font-mono transition-colors cursor-pointer flex items-center gap-1.5 ${
        active ? 'bg-card text-foreground font-semibold shadow-sm border border-border/80' : 'text-muted-foreground hover:text-foreground'
      }`}
      data-testid={testId}
    >
      {icon}
      <span>{label}</span>
    </button>
  );
}

// One tool call in full: its subagent activity, output and input, and a shortcut to steer the agent about it.
// It owns its tab and view state, so the parent keys it by the selection to reset them.
export function ToolInspector({ tc, msg, openInputTab, onInsertFix }: ToolInspectorProps) {
  const subagents = useSubagents();
  const onClose = useToolSelectionStore((s) => s.clearSelection);
  const [tab, setTab] = useState<InspectorTab | 'auto'>(openInputTab ? 'input' : 'auto');
  const [inputView, setInputView] = useState<TextView>('markdown');
  const [outputView, setOutputView] = useState<TextView>('markdown');

  const isSubagent = tc.name === 'subagent';
  const subagentChild = isSubagent ? findSubagentChild(subagents, tc) : undefined;
  const activitySteps = subagentChild ? subagentSteps(subagentChild.messages) : [];
  const hasActivity = activitySteps.length > 0;
  const activeTab: InspectorTab = tab === 'auto' ? (hasActivity ? 'activity' : 'output') : tab;

  const inputObject = typeof tc.input === 'object' && tc.input !== null ? (tc.input as Record<string, unknown>) : null;
  const subagentAgent = inputObject ? String(inputObject.agent || '') : '';
  const inputText = toDisplayText(tc.input);
  // A running subagent has no output yet, so its child session's transcript stands in for it.
  const liveChild = isSubagent && !tc.output ? subagentChild : undefined;
  const outputText = liveChild ? subagentTranscript(liveChild) : toDisplayText(tc.output);

  const isSuccess = tc.status !== 'error' && (tc.exit_code === undefined || tc.exit_code === 0);
  const detectedError = !isSuccess ? outputText || 'Process terminated with non-zero exit code.' : null;

  const insertFix = () => {
    const snippet = (detectedError ?? (outputText || inputText)).trim().slice(0, 500);
    onInsertFix(`Fix the error in tool "${tc.name}":\n${snippet}\nPlease resolve and re-run.`);
  };

  return (
    <div className="w-full lg:col-span-6 flex flex-col h-full min-h-0 rounded-lg border border-border bg-card/95 overflow-hidden shadow-sm" data-testid="tool-inspector-panel">
      <div className="flex flex-col h-full min-h-0">
        <div className="p-3 border-b border-border bg-card flex items-center justify-between gap-2 shrink-0">
          <div className="flex items-center gap-2 min-w-0">
            {isSubagent ? <Bot className="size-4 text-purple-400 shrink-0" /> : <Terminal className="size-4 text-primary shrink-0" />}
            <div className="min-w-0 flex items-center gap-1.5">
              <span className="font-mono font-semibold text-xs text-foreground truncate">{tc.name}</span>
              {isSubagent && subagentAgent && (
                <Badge variant="outline" className="font-mono text-[9px] px-1 py-0 text-purple-400 border-purple-500/30">
                  {subagentAgent}
                </Badge>
              )}
            </div>
            <div className="shrink-0">
              <ExitBadge tc={tc} isSuccess={isSuccess} />
            </div>
          </div>
          <Button
            type="button"
            variant="ghost"
            size="icon"
            className="h-7 w-7 text-muted-foreground hover:text-foreground"
            onClick={onClose}
            title="Close Inspector"
            aria-label="Close Inspector"
            data-testid="close-inspector-header-btn"
          >
            <X className="size-4" />
          </Button>
        </div>

        <div className="flex items-center justify-between px-3 py-1.5 border-b border-border/60 bg-muted/30 text-xs">
          <div className="flex items-center gap-1">
            {hasActivity && (
              <TabButton
                active={activeTab === 'activity'}
                onClick={() => setTab('activity')}
                icon={<Activity className="size-3" />}
                label="Activity"
                testId="subagent-activity-tab"
              />
            )}
            <TabButton active={activeTab === 'output'} onClick={() => setTab('output')} icon={<Terminal className="size-3" />} label="Terminal Output (Stdout/Stderr)" />
            <TabButton active={activeTab === 'input'} onClick={() => setTab('input')} icon={<FileCode className="size-3" />} label="Prompt Brief / Input" />
          </div>
          {msg.created_at && (
            <span className="text-[10px] font-mono text-muted-foreground hidden sm:inline">{formatTimestamp(msg.created_at)}</span>
          )}
        </div>

        <div className="flex-1 min-h-0 overflow-y-auto p-3.5 space-y-3 font-mono text-xs">
          {detectedError && (
            <div className="rounded-lg p-3 bg-rose-500/10 border border-rose-500/30 space-y-1.5">
              <div className="flex items-center gap-1.5 text-rose-600 dark:text-rose-400 font-semibold text-xs">
                <AlertCircle className="size-3.5 shrink-0" />
                <span>Detected Error</span>
              </div>
              <p className="text-[11px] text-muted-foreground font-sans">
                The tool invocation returned non-zero exit status. Review diagnostic logs below or insert a targeted fix into the steer prompt.
              </p>
            </div>
          )}

          {activeTab === 'activity' ? (
            <SubagentActivityTimeline steps={activitySteps} running={tc.status === 'running'} />
          ) : activeTab === 'output' ? (
            <InspectorOutputPane
              output={outputText}
              running={tc.status === 'running'}
              isSuccess={isSuccess}
              isSubagent={isSubagent}
              view={outputView}
              onViewChange={setOutputView}
            />
          ) : (
            <InspectorInputPane
              inputText={inputText}
              inputObject={inputObject}
              inputIsString={typeof tc.input === 'string'}
              isSubagent={isSubagent}
              view={inputView}
              onViewChange={setInputView}
            />
          )}
        </div>

        <div className="p-2.5 border-t border-border bg-card/90 flex flex-wrap items-center justify-between gap-2 shrink-0">
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={onClose}
            className="h-7 text-xs font-mono gap-1 text-muted-foreground hover:text-foreground"
            data-testid="close-inspector-footer-btn"
          >
            <X className="size-3" />
            <span>Close / Unselect</span>
          </Button>

          <Button
            type="button"
            variant="default"
            size="sm"
            onClick={insertFix}
            className="h-7 text-xs font-mono gap-1.5 bg-primary text-primary-foreground hover:bg-primary/90"
            data-testid="insert-fix-btn"
          >
            <CornerDownLeft className="size-3" />
            <span>Insert Fix into Steer Input ↵</span>
          </Button>
        </div>
      </div>
    </div>
  );
}
