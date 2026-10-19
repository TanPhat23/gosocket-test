import { Check, ChevronDown, ChevronRight, Eye, RefreshCw, X } from 'lucide-react';
import { Badge } from '@/components/ui/badge';
import { Markdown } from '@/components/ui/markdown';
import type { SessionToolCall } from '../../schemas/taskSchemas';
import { findSubagentChild, subagentLatestStep } from '../../utils/subagentActivity';
import { toDisplayText } from '../../utils/sessionFormat';
import { useSubagents, useToolSelectionStore } from './SessionContexts';

interface ToolCallCardProps {
  tc: SessionToolCall;
  // `${msgIdx}-${tcIdx}`: where the tool call sits in the transcript.
  expandKey: string;
}

function ToolStatus({ tc, isSuccess }: { tc: SessionToolCall; isSuccess: boolean }) {
  if (tc.status === 'running') {
    return (
      <span className="inline-flex items-center gap-1.5 text-[11px] text-sky-500">
        <span className="size-1.5 animate-pulse rounded-full bg-sky-500" />
        running
      </span>
    );
  }
  if (!isSuccess) {
    return (
      <span className="inline-flex items-center gap-1 text-[11px] font-medium text-rose-600 dark:text-rose-400">
        <X className="size-3" />
        {tc.exit_code !== undefined && tc.exit_code !== 0 ? `exit ${tc.exit_code}` : 'exit 1'}
      </span>
    );
  }
  return (
    <span className="inline-flex items-center gap-1 text-[11px] text-emerald-600 dark:text-emerald-400">
      <Check className="size-3" />
      exit {tc.exit_code ?? 0}
    </span>
  );
}

// One tool call in the transcript: a row that selects it in the inspector and expands an inline preview.
export function ToolCallCard({ tc, expandKey }: ToolCallCardProps) {
  const isExpanded = useToolSelectionStore((s) => s.expandedTools[expandKey] === true);
  const isSelected = useToolSelectionStore((s) => s.selection?.key === expandKey);
  const toggleExpanded = useToolSelectionStore((s) => s.toggleToolExpanded);
  const select = useToolSelectionStore((s) => s.selectTool);
  const subagents = useSubagents();
  const isSubagent = tc.name === 'subagent';
  const linkedChild = isSubagent ? findSubagentChild(subagents, tc) : undefined;
  const subagentLabel = linkedChild
    ? subagentLatestStep(linkedChild) || 'Subagent is starting…'
    : 'Subagent is executing in child session...';
  const inputObj = typeof tc.input === 'object' && tc.input !== null ? (tc.input as Record<string, unknown>) : null;
  const subagentAgent = inputObj ? String(inputObj.agent || '') : '';
  const subagentDesc = inputObj ? String(inputObj.description || inputObj.prompt || '') : '';
  const inputStr = toDisplayText(tc.input);
  const outputStr = toDisplayText(tc.output);
  const isSuccess = tc.status !== 'error' && (tc.exit_code === undefined || tc.exit_code === 0);

  return (
    <div
      onClick={() => select(expandKey, isSubagent && !tc.output)}
      className={`rounded-lg border transition-colors overflow-hidden text-xs cursor-pointer ${
        isSelected
          ? 'border-primary/60 ring-1 ring-primary/30 bg-primary/5'
          : !isSuccess
          ? 'border-rose-500/40 bg-rose-500/5'
          : 'border-border/70 bg-background/60 hover:border-border'
      }`}
      data-testid="tool-call-card"
    >
      <div className="flex w-full select-none items-center justify-between gap-2 px-2.5 py-1.5 text-left">
        <div className="flex items-center gap-2 min-w-0 flex-1">
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              toggleExpanded(expandKey);
            }}
            className="p-0.5 hover:bg-muted/80 rounded"
            title={isExpanded ? 'Collapse inline preview' : 'Expand inline preview'}
            data-testid={`toggle-tool-${tc.name}`}
          >
            {isExpanded ? (
              <ChevronDown className="size-3 text-muted-foreground shrink-0" />
            ) : (
              <ChevronRight className="size-3 text-muted-foreground shrink-0" />
            )}
          </button>
          <Badge
            variant="outline"
            className={`font-mono text-[10px] px-1.5 py-0 shrink-0 ${
              isSubagent ? 'bg-purple-500/10 text-purple-400 border-purple-500/30' : 'bg-primary/10 text-primary border-primary/20'
            }`}
          >
            {tc.name}
          </Badge>
          {isSubagent && subagentAgent && (
            <Badge variant="outline" className="font-mono text-[9px] px-1 py-0 bg-primary/10 text-primary border-primary/20 shrink-0">
              {subagentAgent}
            </Badge>
          )}
          <span className="min-w-0 flex-1 truncate font-mono text-[11px] text-muted-foreground">
            {isSubagent && subagentDesc ? subagentDesc : inputStr.replace(/\n/g, ' ')}
          </span>
        </div>

        <div className="flex items-center gap-2 shrink-0">
          {isSelected && (
            <span className="hidden items-center gap-1 text-[10px] font-medium text-primary sm:inline-flex">
              <Eye className="size-3 shrink-0" />
              <span>Inspecting in Right Pane</span>
            </span>
          )}
          <ToolStatus tc={tc} isSuccess={isSuccess} />
        </div>
      </div>

      {isExpanded && (
        <div className="p-2.5 border-t border-border/60 space-y-2 bg-background font-mono text-[11px]">
          {isSubagent && inputObj && (
            <div className="rounded p-2 bg-purple-500/5 border border-purple-500/15 space-y-1 text-xs font-sans">
              <div className="flex items-center gap-2">
                <span className="font-semibold text-purple-400">Subagent:</span>
                <span className="font-mono text-[11px] text-foreground">
                  {subagentAgent ? `[${subagentAgent}] ` : ''}
                  {subagentDesc}
                </span>
              </div>
            </div>
          )}

          {inputStr && (
            <div className="space-y-1">
              <div className="text-[10px] text-muted-foreground uppercase font-semibold">
                {isSubagent ? 'Prompt / Task Brief' : 'Input'}
              </div>
              {isSubagent && inputObj?.prompt ? (
                <div className="p-2.5 rounded bg-muted/40 border border-border/50 text-[11px] overflow-x-auto text-foreground max-h-56 font-sans">
                  <Markdown content={String(inputObj.prompt)} />
                </div>
              ) : (
                <pre className="p-2 rounded bg-muted/50 text-[10px] overflow-x-auto whitespace-pre-wrap text-foreground max-h-48 font-mono">
                  {inputStr}
                </pre>
              )}
            </div>
          )}

          <div className="space-y-1">
            <div className="text-[10px] text-muted-foreground uppercase font-semibold">
              {isSubagent ? 'Subagent Output / Results' : 'Output'}
            </div>
            {tc.status === 'running' ? (
              <div className="flex items-center gap-2 p-3 rounded bg-muted/30 border border-border/50 text-xs text-primary font-mono">
                <RefreshCw className="size-3.5 animate-spin text-primary" />
                <span>{isSubagent ? subagentLabel : 'Tool execution in progress...'}</span>
              </div>
            ) : outputStr ? (
              <pre
                className={`p-2 rounded text-[10px] overflow-x-auto whitespace-pre-wrap max-h-60 font-mono ${
                  tc.status === 'error' || !isSuccess
                    ? 'bg-destructive/10 text-destructive border border-destructive/20'
                    : 'bg-muted/50 text-foreground'
                }`}
              >
                {outputStr}
              </pre>
            ) : (
              <div className="text-[10px] text-muted-foreground italic p-1">No output recorded.</div>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
