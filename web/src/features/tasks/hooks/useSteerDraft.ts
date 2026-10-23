import { useState } from 'react';
import { readSteerImages, type SteerImage } from '../utils/steerImages';

// What the user is composing for the agent: the text, the attached images and the last delivery or attach error.
export function useSteerDraft() {
  const [prompt, setPrompt] = useState('');
  const [images, setImages] = useState<SteerImage[]>([]);
  const [error, setError] = useState<string | null>(null);

  const attachImages = async (files: File[]) => {
    const { images: added, error: attachError } = await readSteerImages(files, images.length);
    if (attachError) setError(attachError);
    if (added.length) setImages((prev) => [...prev, ...added]);
  };

  const removeImage = (id: string) => setImages((prev) => prev.filter((img) => img.id !== id));

  return { prompt, setPrompt, images, setImages, error, setError, attachImages, removeImage };
}

export type SteerDraft = ReturnType<typeof useSteerDraft>;
