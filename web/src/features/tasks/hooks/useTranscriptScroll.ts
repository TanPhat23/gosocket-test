import { useEffect, useRef, useState } from 'react';
import type { SessionMessage } from '../schemas/taskSchemas';

// Scrolling for the transcript: jump to the bottom when the tab opens and as messages arrive, and offer a way back
// down once the reader has scrolled up.
export function useTranscriptScroll(isActive: boolean, messages: SessionMessage[] | undefined) {
  const containerRef = useRef<HTMLDivElement>(null);
  const endRef = useRef<HTMLDivElement>(null);
  const prevCountRef = useRef(0);
  const [showScrollBottomBtn, setShowScrollBottomBtn] = useState(false);

  const scrollToBottom = (smooth = true) => {
    const el = containerRef.current;
    if (el) {
      try {
        if (typeof el.scrollTo === 'function') {
          el.scrollTo({ top: el.scrollHeight, behavior: smooth ? 'smooth' : 'auto' });
        } else {
          el.scrollTop = el.scrollHeight;
        }
      } catch {
        // Fallback if environment (e.g. mock DOM where scrollTop is non-writable property) prevents direct assignment
      }
    }
    endRef.current?.scrollIntoView({ behavior: smooth ? 'smooth' : 'auto' });
  };

  const handleScroll = () => {
    const el = containerRef.current;
    if (!el) return;
    // Show the button when scrolled up by more than 100px.
    setShowScrollBottomBtn(el.scrollHeight - el.scrollTop - el.clientHeight > 100);
  };

  // When the tab becomes active, scroll again after layout has painted.
  useEffect(() => {
    if (!isActive) return;
    const t1 = setTimeout(() => scrollToBottom(false), 50);
    const t2 = setTimeout(() => scrollToBottom(false), 150);
    return () => {
      clearTimeout(t1);
      clearTimeout(t2);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isActive]);

  useEffect(() => {
    const currentCount = messages?.length ?? 0;
    if (currentCount > 0) {
      if (prevCountRef.current === 0) {
        const t = setTimeout(() => scrollToBottom(false), 50);
        return () => clearTimeout(t);
      } else if (currentCount > prevCountRef.current) {
        const t = setTimeout(() => scrollToBottom(true), 50);
        return () => clearTimeout(t);
      }
    }
    prevCountRef.current = currentCount;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [messages]);

  return { containerRef, endRef, showScrollBottomBtn, handleScroll, scrollToBottom };
}
