interface InspectorViewToggleProps {
  view: 'markdown' | 'raw';
  onChange: (view: 'markdown' | 'raw') => void;
  markdownLabel: string;
  rawLabel: string;
  markdownTestId: string;
  rawTestId: string;
}

const optionClass = (active: boolean) =>
  `px-2 py-0.5 rounded text-[10px] font-mono transition-colors cursor-pointer ${
    active ? 'bg-primary/15 text-primary border border-primary/30 font-semibold' : 'text-muted-foreground hover:text-foreground border border-transparent'
  }`;

// Switches a pane between rendered markdown and the raw text.
export function InspectorViewToggle({ view, onChange, markdownLabel, rawLabel, markdownTestId, rawTestId }: InspectorViewToggleProps) {
  return (
    <div className="flex items-center gap-1">
      <button type="button" onClick={() => onChange('markdown')} className={optionClass(view === 'markdown')} data-testid={markdownTestId}>
        {markdownLabel}
      </button>
      <button type="button" onClick={() => onChange('raw')} className={optionClass(view === 'raw')} data-testid={rawTestId}>
        {rawLabel}
      </button>
    </div>
  );
}
