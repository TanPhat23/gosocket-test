import { Settings2, User } from 'lucide-react';
import { Markdown } from '@/components/ui/markdown';
import { cleanUserPrompt } from '../../utils/promptCleaner';

interface UserMessageProps {
  // The prompt as extracted from the message, before the quote and system-context cleanup.
  rawText: string;
  timeString: string | null;
}

// The user's prompt as a right-aligned bubble, with any injected system context folded away.
export function UserMessage({ rawText, timeString }: UserMessageProps) {
  let text = rawText.trim();
  if (text.startsWith('"') && text.endsWith('"') && text.length >= 2) {
    try {
      text = JSON.parse(text);
    } catch {
      text = text.slice(1, -1);
    }
  }
  const { prompt, systemContext } = cleanUserPrompt(text || '(User Prompt)');

  return (
    <div className="flex justify-end" data-testid="user-message">
      <div className="max-w-[85%] space-y-1">
        <div className="rounded-2xl rounded-br-md border border-primary/20 bg-primary/10 px-3.5 py-2.5 text-sm leading-relaxed text-foreground">
          <Markdown content={prompt} />

          {systemContext && (
            <details className="mt-2 rounded-lg border border-border/60 bg-background/70 p-2 text-[11px] text-foreground/90">
              <summary className="flex cursor-pointer select-none items-center justify-between gap-2 text-[11px] font-medium text-muted-foreground hover:text-foreground">
                <span className="flex items-center gap-1.5">
                  <Settings2 className="size-3 text-primary" /> System context &amp; tool catalog
                </span>
                <span className="text-[10px] font-normal opacity-75">{systemContext.split('\n').length} lines</span>
              </summary>
              <pre className="mt-2 max-h-52 overflow-y-auto whitespace-pre-wrap rounded border border-border/40 bg-muted/40 p-2 font-mono text-[10px] leading-relaxed text-foreground/90">
                {systemContext}
              </pre>
            </details>
          )}
        </div>
        <div className="flex items-center justify-end gap-1.5 px-1 text-[10px] text-muted-foreground">
          <User className="size-3" />
          <span>You</span>
          {timeString && <span className="tabular-nums">· {timeString}</span>}
        </div>
      </div>
    </div>
  );
}
