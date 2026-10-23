import React, { useCallback, useLayoutEffect, useRef, useState } from 'react';
import { taskApi } from '../api/taskApi';
import { useTaskStore } from '../store/useTaskStore';
import type { SessionMessage, TaskSessionResponse } from '../schemas/taskSchemas';
import type { SteerDraft } from './useSteerDraft';

interface SteerSubmitOptions {
  taskId: string;
  draft: SteerDraft;
  updateSession: (update: (prev: TaskSessionResponse) => TaskSessionResponse) => void;
  refresh: () => Promise<unknown>;
  scrollToEnd: () => void;
}

// Delivers steer prompts to the agent. The prompt shows in the transcript at once and the task moves to the working
// lane; a failed delivery restores the attached images and reports the error on the draft.
export function useSteerSubmit({ taskId, draft, updateSession, refresh, scrollToEnd }: SteerSubmitOptions) {
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [isSteeringActive, setIsSteeringActive] = useState(false);

  const send = async (e?: React.FormEvent, customPrompt?: string) => {
    if (e) e.preventDefault();
    const text = (customPrompt !== undefined ? customPrompt : draft.prompt).trim();
    if (!text || isSubmitting) return;

    // Suggestion chips and questionnaire answers send text only; a typed steer carries the attached images.
    const images = customPrompt === undefined ? draft.images : [];
    setIsSubmitting(true);
    draft.setError(null);
    if (customPrompt === undefined) {
      draft.setPrompt('');
      draft.setImages([]);
    }
    setIsSteeringActive(true);

    const optimistic: SessionMessage = {
      id: `opt-user-${Date.now()}`,
      type: 'user',
      role: 'user',
      text: images.length ? `${text}\n[${images.length} image${images.length > 1 ? 's' : ''} attached]` : text,
      created_at: new Date().toISOString(),
    };
    updateSession((prev) => ({ ...prev, messages: [...(prev.messages || []), optimistic] }));
    useTaskStore.getState().updateTaskStatus(taskId, 'in_progress');
    setTimeout(scrollToEnd, 50);

    try {
      await taskApi.promptTaskSession(taskId, text, images);
      await refresh();
      setTimeout(scrollToEnd, 100);
    } catch (err: unknown) {
      draft.setError((err as { message?: string })?.message || 'Failed to deliver steer prompt.');
      setIsSteeringActive(false);
      if (images.length) draft.setImages(images);
    } finally {
      setIsSubmitting(false);
    }
  };

  // Questionnaire answers call the latest send through a ref so submitAnswer keeps one identity.
  const sendRef = useRef(send);
  useLayoutEffect(() => {
    sendRef.current = send;
  });
  const submitAnswer = useCallback(async (answerText: string) => {
    await sendRef.current(undefined, answerText);
  }, []);

  return { isSubmitting, isSteeringActive, setIsSteeringActive, send, submitAnswer };
}
