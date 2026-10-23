// Images a user attaches to a steer. The server takes them as base64 data, so each one is read in full on attach.
export interface SteerImage {
  id: string;
  name: string;
  mime: string;
  // Standard base64 of the file, without a data: prefix.
  data: string;
  // The whole data: URL, for the thumbnail.
  preview: string;
}

export const STEER_IMAGE_TYPES = ['image/png', 'image/jpeg', 'image/gif', 'image/webp'];
export const MAX_STEER_IMAGES = 5;
export const MAX_STEER_IMAGE_BYTES = 5 * 1024 * 1024;

export interface ReadSteerImagesResult {
  images: SteerImage[];
  error: string | null;
}

function readAsDataUrl(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result));
    reader.onerror = () => reject(reader.error);
    reader.readAsDataURL(file);
  });
}

// Checks the files the user picked or pasted and reads the valid ones. Anything rejected is named in `error`, and
// the rest are still attached.
export async function readSteerImages(files: File[], alreadyAttached: number): Promise<ReadSteerImagesResult> {
  const errors: string[] = [];
  const images = files.filter((file) => file.type.startsWith('image/') || file.type === '');
  if (files.length > images.length) {
    errors.push('Only PNG, JPEG, GIF or WebP images can be attached.');
  }

  const room = MAX_STEER_IMAGES - alreadyAttached;
  if (images.length > room) {
    errors.push(`At most ${MAX_STEER_IMAGES} images can be attached to a steer.`);
  }

  const accepted: SteerImage[] = [];
  for (const file of images.slice(0, Math.max(room, 0))) {
    if (!STEER_IMAGE_TYPES.includes(file.type)) {
      errors.push(`${file.name || 'A file'} is not a PNG, JPEG, GIF or WebP image.`);
      continue;
    }
    if (file.size > MAX_STEER_IMAGE_BYTES) {
      errors.push(`${file.name || 'An image'} is larger than 5 MB.`);
      continue;
    }
    const preview = await readAsDataUrl(file);
    accepted.push({
      id: `${Date.now()}-${Math.random().toString(36).slice(2)}`,
      name: file.name || 'pasted-image',
      mime: file.type,
      data: preview.slice(preview.indexOf(',') + 1),
      preview,
    });
  }

  return { images: accepted, error: errors.length ? errors.join(' ') : null };
}
