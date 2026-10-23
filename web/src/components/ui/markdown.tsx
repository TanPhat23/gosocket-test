import React from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { cn } from '@/lib/utils';

interface MarkdownProps {
  content: string;
  className?: string;
}

export const Markdown: React.FC<MarkdownProps> = ({ content, className }) => {
  if (!content) return null;

  return (
    <div
      className={cn(
        'markdown-content text-foreground font-sans text-xs leading-relaxed space-y-2',
        className
      )}
    >
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
          h1: ({ children }) => (
            <h1 className="text-sm font-bold text-foreground mt-3 mb-1.5 pb-1 border-b border-border/40">
              {children}
            </h1>
          ),
          h2: ({ children }) => (
            <h2 className="text-xs font-semibold text-foreground mt-2.5 mb-1 pb-0.5 border-b border-border/30">
              {children}
            </h2>
          ),
          h3: ({ children }) => (
            <h3 className="text-xs font-semibold text-foreground mt-2 mb-1 flex items-center gap-1.5">
              {children}
            </h3>
          ),
          h4: ({ children }) => (
            <h4 className="text-[11px] font-semibold text-muted-foreground uppercase tracking-wider mt-1.5 mb-0.5">
              {children}
            </h4>
          ),
          p: ({ children }) => <p className="leading-relaxed mb-1.5 last:mb-0">{children}</p>,
          ul: ({ children }) => <ul className="list-disc list-inside space-y-1 my-1.5 pl-1">{children}</ul>,
          ol: ({ children }) => <ol className="list-decimal list-inside space-y-1 my-1.5 pl-1">{children}</ol>,
          li: ({ children }) => <li className="leading-relaxed text-foreground/90">{children}</li>,
          strong: ({ children }) => <strong className="font-semibold text-foreground">{children}</strong>,
          em: ({ children }) => <em className="italic text-foreground/90">{children}</em>,
          code: ({ className: codeClassName, children, ...props }) => {
            const isInline = !codeClassName && typeof children === 'string' && !children.includes('\n');
            if (isInline) {
              return (
                <code
                  className="bg-muted/70 text-foreground px-1.5 py-0.5 rounded font-mono text-[11px] border border-border/50"
                  {...props}
                >
                  {children}
                </code>
              );
            }
            return (
              <code className={cn('font-mono text-[11px]', codeClassName)} {...props}>
                {children}
              </code>
            );
          },
          pre: ({ children }) => (
            <pre className="p-2.5 rounded-lg bg-muted/50 border border-border/60 overflow-x-auto font-mono text-[11px] leading-snug my-2">
              {children}
            </pre>
          ),
          blockquote: ({ children }) => (
            <blockquote className="border-l-2 border-primary/50 pl-2.5 my-2 italic text-muted-foreground">
              {children}
            </blockquote>
          ),
          hr: () => <hr className="my-3 border-border/50" />,
        }}
      >
        {content}
      </ReactMarkdown>
    </div>
  );
};
