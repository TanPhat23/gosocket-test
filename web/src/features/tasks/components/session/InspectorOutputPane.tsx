import { RefreshCw } from 'lucide-react';
import { Markdown } from '@/components/ui/markdown';
import { InspectorViewToggle } from './InspectorViewToggle';

interface InspectorOutputPaneProps {
  output: string;
  running: boolean;
  isSuccess: boolean;
  isSubagent: boolean;
  view: 'markdown' | 'raw';
  onViewChange: (view: 'markdown' | 'raw') => void;
}

// A tool's stdout and stderr; a subagent's report can be read as markdown.
export function InspectorOutputPane({ output, running, isSuccess, isSubagent, view, onViewChange }: InspectorOutputPaneProps) {
  return (
    <div className="h-full flex flex-col space-y-2">
      <div className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground flex items-center justify-between">
        <div className="flex items-center gap-2">
          <span>Terminal Logs</span>
          <span className="font-normal opacity-70">{output ? `${output.split('\n').length} lines` : '0 lines'}</span>
        </div>
        {isSubagent && output && (
          <InspectorViewToggle
            view={view}
            onChange={onViewChange}
            markdownLabel="Formatted Report (Markdown)"
            rawLabel="Raw Logs"
            markdownTestId="toggle-output-markdown-btn"
            rawTestId="toggle-output-raw-btn"
          />
        )}
      </div>
      {running ? (
        <div className="flex items-center gap-2 p-4 rounded bg-muted/20 border border-border/50 text-xs text-primary">
          <RefreshCw className="size-4 animate-spin text-primary" />
          <span>Execution running in child environment...</span>
        </div>
      ) : output ? (
        isSubagent && view === 'markdown' ? (
          <div className="flex-1 min-h-[260px] p-3.5 rounded-lg border border-border/60 bg-muted/20 overflow-x-auto text-xs font-sans">
            <Markdown content={output} />
          </div>
        ) : (
          <pre
            className={`flex-1 min-h-[260px] p-3.5 rounded-lg overflow-x-auto whitespace-pre-wrap leading-relaxed text-xs font-mono border ${
              !isSuccess ? 'bg-rose-950/20 text-rose-200 border-rose-500/30' : 'bg-black/40 text-foreground border-border/50'
            }`}
          >
            {output}
          </pre>
        )
      ) : (
        <div className="p-4 text-center text-muted-foreground italic text-xs">No terminal output recorded for this tool run.</div>
      )}
    </div>
  );
}
