import { describe, it, expect } from 'vitest';
import { formatBytes, formatUptime } from './format';

describe('formatBytes', () => {
  it('scales to the largest fitting unit', () => {
    expect(formatBytes(512)).toBe('512 B');
    expect(formatBytes(1536)).toBe('1.5 KB');
    expect(formatBytes(300 * 1024 * 1024)).toBe('300 MB');
    expect(formatBytes(2 * 1024 ** 3)).toBe('2.0 GB');
  });
});

describe('formatUptime', () => {
  const now = Date.parse('2026-10-19T12:00:00Z');

  it('shows the two largest units', () => {
    expect(formatUptime('2026-10-16T08:59:00Z', now)).toBe('3d 3h');
    expect(formatUptime('2026-10-19T11:54:48Z', now)).toBe('5m 12s');
    expect(formatUptime('2026-10-19T11:59:30Z', now)).toBe('30s');
  });

  it('survives a start time that is unparsable or in the future', () => {
    expect(formatUptime('nope', now)).toBe('—');
    expect(formatUptime('2026-10-19T12:00:05Z', now)).toBe('0s');
  });
});
