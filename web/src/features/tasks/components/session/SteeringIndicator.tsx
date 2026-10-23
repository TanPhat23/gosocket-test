import { Bot } from 'lucide-react';

// Shown at the end of the transcript while a steer prompt is being worked on.
export function SteeringIndicator() {
  return (
    <div className="flex items-center gap-3 animate-fade-in" data-testid="steering-live-indicator">
      <div className="flex size-7 shrink-0 items-center justify-center rounded-full bg-primary/10 text-primary">
        <Bot className="size-4 animate-pulse" />
      </div>
      <div className="flex items-center gap-2 text-xs text-muted-foreground">
        <span className="flex items-center gap-1" aria-hidden="true">
          <span className="size-1.5 animate-bounce rounded-full bg-primary [animation-delay:-0.3s]" />
          <span className="size-1.5 animate-bounce rounded-full bg-primary [animation-delay:-0.15s]" />
          <span className="size-1.5 animate-bounce rounded-full bg-primary" />
        </span>
        Agent is working: generating tool calls and response…
      </div>
    </div>
  );
}
