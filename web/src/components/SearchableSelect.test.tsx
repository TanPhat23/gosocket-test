import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { SearchableSelect } from './SearchableSelect';

const items = [
  { value: 'id-1', label: 'Alpha Team' },
  { value: 'id-2', label: 'Beta Team' },
];

describe('SearchableSelect', () => {
  it('shows the selected label instead of the value', () => {
    render(<SearchableSelect items={items} value="id-2" onValueChange={vi.fn()} ariaLabel="Team" />);

    expect(screen.getByRole('combobox', { name: 'Team' })).toHaveTextContent('Beta Team');
    expect(screen.queryByText('id-2')).not.toBeInTheDocument();
  });

  it('searches by label and reports the chosen value', () => {
    const onValueChange = vi.fn();
    render(<SearchableSelect items={items} value="" onValueChange={onValueChange} ariaLabel="Team" />);

    fireEvent.click(screen.getByRole('combobox', { name: 'Team' }));
    const search = screen.getByPlaceholderText('Search...');
    fireEvent.change(search, { target: { value: 'beta' } });

    expect(screen.queryByText('Alpha Team')).not.toBeInTheDocument();
    fireEvent.click(screen.getByText('Beta Team'));

    expect(onValueChange).toHaveBeenCalledWith('id-2');
  });
});
