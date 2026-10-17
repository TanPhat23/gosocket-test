import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { ProviderKeyDialog } from './ProviderKeyDialog';
import { axiosClient } from '@/lib/axios';

describe('ProviderKeyDialog', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('renders nothing for non-operators', async () => {
    const get = vi.spyOn(axiosClient, 'get').mockResolvedValue({ data: { operator: false } });
    const { container } = render(<ProviderKeyDialog />);
    await waitFor(() => expect(get).toHaveBeenCalled());
    expect(container).toBeEmptyDOMElement();
  });

  it('renders nothing when the status call fails', async () => {
    const get = vi.spyOn(axiosClient, 'get').mockRejectedValue(new Error('401'));
    const { container } = render(<ProviderKeyDialog />);
    await waitFor(() => expect(get).toHaveBeenCalled());
    expect(container).toBeEmptyDOMElement();
  });

  it('lets an operator save a key and clears the field', async () => {
    vi.spyOn(axiosClient, 'get').mockResolvedValue({ data: { operator: true, writable: true, configured: false } });
    const put = vi.spyOn(axiosClient, 'put').mockResolvedValue({ data: { status: 'ok' } });
    render(<ProviderKeyDialog />);
    fireEvent.click(await screen.findByTestId('provider-key-btn'));
    const input = (await screen.findByTestId('provider-key-input')) as HTMLInputElement;
    fireEvent.change(input, { target: { value: 'AIzaSecretKey123' } });
    fireEvent.click(screen.getByTestId('provider-key-save'));
    await waitFor(() => expect(put).toHaveBeenCalledWith('/operator/provider-key', { key: 'AIzaSecretKey123' }));
    await waitFor(() => expect(screen.getByTestId('provider-key-message')).toHaveTextContent('saved'));
    expect(input.value).toBe('');
  });
});
