import { AlertTriangle, ImagePlus, RefreshCw, Send, Sparkles, X } from 'lucide-react';
import { useRef } from 'react';
import { Button } from '@/components/ui/button';
import { Textarea } from '@/components/ui/textarea';
import type { SteerDraft } from '../../hooks/useSteerDraft';
import { MAX_STEER_IMAGES, STEER_IMAGE_TYPES } from '../../utils/steerImages';

const SUGGESTIONS = [
  { label: 'Fix failed step', prompt: 'Fix the failed step and run tests again.' },
  { label: 'Retry subagent', prompt: 'Retry subagent execution with the specified guidelines.' },
  { label: 'Resume session', prompt: 'Resume session and continue with remaining tasks.' },
] as const;

interface SteerComposerProps {
  draft: SteerDraft;
  disabled: boolean;
  isSubmitting: boolean;
  isWorking: boolean;
  onSubmit: (e?: React.FormEvent) => void;
}

function Suggestions({ disabled, onPick }: { disabled: boolean; onPick: (prompt: string) => void }) {
  return (
    <div className="flex flex-wrap items-center gap-1.5" aria-label="Suggested steering prompts">
      {SUGGESTIONS.map((s) => (
        <button
          key={s.label}
          type="button"
          onClick={() => onPick(s.prompt)}
          disabled={disabled}
          className="inline-flex items-center gap-1 rounded-full border border-border/70 bg-muted/40 px-2.5 py-1 text-xs text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:pointer-events-none disabled:opacity-50 cursor-pointer"
        >
          <Sparkles className="size-3 text-primary/70" />
          {s.label}
        </button>
      ))}
    </div>
  );
}

function ImageThumbnails({ draft }: { draft: SteerDraft }) {
  return (
    <div className="flex flex-wrap gap-2 px-3 pt-3" data-testid="steer-image-list">
      {draft.images.map((img) => (
        <div key={img.id} className="relative size-16 overflow-hidden rounded-lg border border-border bg-muted/30">
          <img src={img.preview} alt={img.name} className="size-full object-cover" />
          <button
            type="button"
            onClick={() => draft.removeImage(img.id)}
            aria-label={`Remove ${img.name}`}
            className="absolute right-1 top-1 rounded-full bg-background/90 p-0.5 text-muted-foreground shadow-sm hover:text-foreground cursor-pointer"
          >
            <X className="size-3" />
          </button>
        </div>
      ))}
    </div>
  );
}

function WorkingIndicator() {
  return (
    <span className="ml-1 inline-flex items-center gap-1.5 truncate text-xs text-primary">
      <span className="relative flex size-1.5">
        <span className="absolute inline-flex size-full animate-ping rounded-full bg-primary/60" />
        <span className="relative inline-flex size-1.5 rounded-full bg-primary" />
      </span>
      Agent is working
    </span>
  );
}

// The box the user steers the agent from: text, pasted or attached images, and a send button.
export function SteerComposer({ draft, disabled, isSubmitting, isWorking, onSubmit }: SteerComposerProps) {
  const imageInputRef = useRef<HTMLInputElement>(null);
  const isEmpty = !draft.prompt && draft.images.length === 0;

  return (
    <form onSubmit={onSubmit} className="shrink-0 space-y-2" data-testid="steer-prompt-form">
      {isEmpty && <Suggestions disabled={isSubmitting || disabled} onPick={draft.setPrompt} />}

      <div className="rounded-xl border border-border bg-card shadow-xs transition-colors focus-within:border-ring focus-within:ring-[3px] focus-within:ring-ring/30">
        {draft.error && (
          <div role="alert" className="mx-3 mt-3 flex items-start gap-2 rounded-md border border-destructive/20 bg-destructive/10 px-2.5 py-2 text-xs text-destructive">
            <AlertTriangle className="mt-0.5 size-3.5 shrink-0" />
            <span>{draft.error}</span>
          </div>
        )}

        {draft.images.length > 0 && <ImageThumbnails draft={draft} />}

        <Textarea
          id="steer-prompt-input"
          aria-label="Steer the agent"
          value={draft.prompt}
          onChange={(e) => draft.setPrompt(e.target.value)}
          onPaste={(e) => {
            const files = Array.from(e.clipboardData?.files ?? []);
            if (files.some((f) => f.type.startsWith('image/'))) {
              e.preventDefault();
              void draft.attachImages(files);
            }
          }}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
              e.preventDefault();
              onSubmit();
            }
          }}
          placeholder="Message the agent: guidance, fixes, or corrections. Paste a screenshot to attach it."
          disabled={isSubmitting || disabled}
          rows={2}
          className="min-h-[3.25rem] max-h-40 resize-none border-0 bg-transparent px-3 py-3 text-sm shadow-none focus-visible:ring-0 focus-visible:border-0 dark:bg-transparent"
          data-testid="steer-prompt-textarea"
        />

        <div className="flex items-center justify-between gap-2 px-2 pb-2">
          <div className="flex min-w-0 items-center gap-1">
            <input
              ref={imageInputRef}
              type="file"
              accept={STEER_IMAGE_TYPES.join(',')}
              multiple
              className="hidden"
              data-testid="steer-image-input"
              onChange={(e) => {
                void draft.attachImages(Array.from(e.target.files ?? []));
                e.target.value = '';
              }}
            />
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              onClick={() => imageInputRef.current?.click()}
              disabled={isSubmitting || disabled || draft.images.length >= MAX_STEER_IMAGES}
              className="text-muted-foreground hover:text-foreground"
              aria-label="Attach images"
              title={`Attach images (PNG, JPEG, GIF or WebP, up to ${MAX_STEER_IMAGES})`}
              data-testid="attach-steer-image-btn"
            >
              <ImagePlus className="size-4" />
            </Button>
            {draft.images.length > 0 && (
              <span className="text-xs text-muted-foreground tabular-nums">
                {draft.images.length}/{MAX_STEER_IMAGES}
              </span>
            )}
            {isWorking && <WorkingIndicator />}
          </div>

          <div className="flex shrink-0 items-center gap-2">
            <span className="hidden text-[11px] text-muted-foreground sm:inline">
              <kbd className="rounded border border-border bg-muted/50 px-1 font-sans">Ctrl</kbd>
              {' + '}
              <kbd className="rounded border border-border bg-muted/50 px-1 font-sans">Enter</kbd>
              {' to send'}
            </span>
            <Button
              type="submit"
              size="sm"
              disabled={!draft.prompt.trim() || isSubmitting || disabled}
              className="gap-1.5 rounded-lg"
              aria-label="Steer"
              data-testid="send-steer-btn"
            >
              {isSubmitting ? (
                <>
                  <RefreshCw className="size-3.5 animate-spin" />
                  <span>Sending</span>
                </>
              ) : (
                <>
                  <Send className="size-3.5" />
                  <span>Steer</span>
                </>
              )}
            </Button>
          </div>
        </div>
      </div>
    </form>
  );
}
