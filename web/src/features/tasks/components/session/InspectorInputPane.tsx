import { Markdown } from '@/components/ui/markdown';
import { InspectorViewToggle } from './InspectorViewToggle';

interface InspectorInputPaneProps {
  inputText: string;
  // The tool's input when it is an object, for its `prompt` and `description` fields.
  inputObject: Record<string, unknown> | null;
  inputIsString: boolean;
  isSubagent: boolean;
  view: 'markdown' | 'raw';
  onViewChange: (view: 'markdown' | 'raw') => void;
}

// A tool's input arguments; a prompt or a subagent brief can be read as markdown.
export function InspectorInputPane({ inputText, inputObject, inputIsString, isSubagent, view, onViewChange }: InspectorInputPaneProps) {
  const hasReadableBrief = isSubagent || Boolean(inputObject?.prompt) || inputIsString;
  const brief = inputObject?.prompt ? String(inputObject.prompt) : inputText;

  return (
    <div className="h-full flex flex-col space-y-2">
      {Boolean(inputObject?.description) && (
        <div className="p-2.5 rounded-lg bg-purple-500/10 border border-purple-500/20 text-xs font-sans space-y-1">
          <div className="font-semibold text-purple-600 dark:text-purple-300 text-[11px] uppercase tracking-wider">Subagent Objective:</div>
          <div className="text-foreground leading-relaxed">{String(inputObject?.description)}</div>
        </div>
      )}

      <div className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground flex items-center justify-between">
        <div className="flex items-center gap-2">
          <span>Command / Prompt Input</span>
          <span className="font-normal opacity-70">{inputText ? `${inputText.split('\n').length} lines` : '0 lines'}</span>
        </div>
        {hasReadableBrief && inputText && (
          <InspectorViewToggle
            view={view}
            onChange={onViewChange}
            markdownLabel="Formatted Brief (Markdown)"
            rawLabel="Raw Input (JSON)"
            markdownTestId="toggle-input-markdown-btn"
            rawTestId="toggle-input-raw-btn"
          />
        )}
      </div>

      {inputText ? (
        hasReadableBrief && view === 'markdown' ? (
          <div className="flex-1 min-h-[260px] p-3.5 rounded-lg bg-muted/20 border border-border/60 overflow-x-auto text-xs font-sans leading-relaxed">
            <Markdown content={brief} />
          </div>
        ) : (
          <pre className="flex-1 min-h-[260px] p-3.5 rounded-lg bg-black/40 text-foreground border border-border/50 overflow-x-auto whitespace-pre-wrap leading-relaxed text-xs font-mono">
            {inputText}
          </pre>
        )
      ) : (
        <div className="p-4 text-center text-muted-foreground italic text-xs">No input arguments provided.</div>
      )}
    </div>
  );
}
