import { describe, it, expect } from 'vitest';
import { MAX_STEER_IMAGES, MAX_STEER_IMAGE_BYTES, readSteerImages } from './steerImages';

const png = (name = 'shot.png') => new File(['png-bytes'], name, { type: 'image/png' });

describe('readSteerImages', () => {
  it('reads an image as base64 without the data: prefix and keeps the preview', async () => {
    const { images, error } = await readSteerImages([png()], 0);

    expect(error).toBeNull();
    expect(images).toHaveLength(1);
    expect(images[0].mime).toBe('image/png');
    expect(images[0].name).toBe('shot.png');
    expect(images[0].data).toBe(btoa('png-bytes'));
    expect(images[0].preview.startsWith('data:image/png;base64,')).toBe(true);
  });

  it('rejects files that are not images', async () => {
    const txt = new File(['hello'], 'notes.txt', { type: 'text/plain' });

    const { images, error } = await readSteerImages([txt], 0);

    expect(images).toHaveLength(0);
    expect(error).toMatch(/Only PNG, JPEG, GIF or WebP/);
  });

  it('rejects an SVG even though it is an image', async () => {
    const svg = new File(['<svg/>'], 'x.svg', { type: 'image/svg+xml' });

    const { images, error } = await readSteerImages([svg], 0);

    expect(images).toHaveLength(0);
    expect(error).toMatch(/not a PNG, JPEG, GIF or WebP image/);
  });

  it('rejects images over 5 MB', async () => {
    const big = new File(['x'], 'big.png', { type: 'image/png' });
    Object.defineProperty(big, 'size', { value: MAX_STEER_IMAGE_BYTES + 1 });

    const { images, error } = await readSteerImages([big], 0);

    expect(images).toHaveLength(0);
    expect(error).toMatch(/larger than 5 MB/);
  });

  it('keeps only as many images as the steer has room for', async () => {
    const files = Array.from({ length: 3 }, (_, i) => png(`${i}.png`));

    const { images, error } = await readSteerImages(files, MAX_STEER_IMAGES - 1);

    expect(images).toHaveLength(1);
    expect(error).toMatch(/At most 5 images/);
  });
});
