import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { ChatMarkdown, splitReasoningSummary } from './ChatMarkdown';

describe('chat markdown', () => {
  afterEach(cleanup);

  it('renders structured Markdown while a response is still streaming', () => {
    render(
      <ChatMarkdown
        content={
          '### Live result\n\n- First item\n- Second item\n\n| Name | Value |\n| --- | --- |\n| Cache | 8 |'
        }
        streaming
      />,
    );

    expect(screen.getByRole('heading', { name: 'Live result' })).toBeVisible();
    expect(screen.getByRole('list')).toHaveTextContent('First item');
    expect(screen.getByRole('table')).toHaveTextContent('Cache');
    expect(screen.queryByText(/### Live result/u)).not.toBeInTheDocument();
  });

  it('defers an incomplete trailing table instead of rendering unstable rows', () => {
    render(<ChatMarkdown content={'Before\n\n| Name | Value |\n| ---'} streaming />);

    expect(screen.getByText('Before')).toBeVisible();
    expect(screen.queryByText('Name')).not.toBeInTheDocument();
  });

  it('uses only an emitted leading reasoning heading as the thinking title', () => {
    expect(splitReasoningSummary('**Inspecting the request**\n\nChecked the route.')).toEqual({
      body: 'Checked the route.',
      title: 'Inspecting the request',
    });
    expect(splitReasoningSummary('**Important:** keep the public schema stable.')).toEqual({
      body: '**Important:** keep the public schema stable.',
      title: undefined,
    });
  });

  it('renders code tools, external links, and math from completed messages', () => {
    render(
      <ChatMarkdown
        content={'```ts\nconst answer = 42;\n```\n\n[Reference](https://example.com)\n\n$$x^2$$'}
      />,
    );

    expect(screen.getByText('TypeScript')).toBeVisible();
    expect(screen.getByRole('button', { name: 'Copy code' })).toBeVisible();
    expect(screen.getByRole('link', { name: 'Reference' })).toHaveAttribute(
      'rel',
      'noreferrer noopener',
    );
    expect(document.querySelector('mjx-container')).not.toBeNull();
  });

  it('offers completed HTML code to the preview workspace', () => {
    const openPreview = vi.fn();
    render(
      <ChatMarkdown
        content={'```html\n<!doctype html><button>Play</button>\n```'}
        onOpenHtmlPreview={openPreview}
      />,
    );

    fireEvent.click(screen.getByRole('button', { name: 'Open HTML preview' }));
    expect(openPreview).toHaveBeenCalledWith('<!doctype html><button>Play</button>\n');
  });

  it('recognizes a complete HTML document in an unlabeled code fence', () => {
    const openPreview = vi.fn();
    render(
      <ChatMarkdown
        content={'```\n<!doctype html><html><body>Preview</body></html>\n```'}
        onOpenHtmlPreview={openPreview}
      />,
    );

    fireEvent.click(screen.getByRole('button', { name: 'Open HTML preview' }));
    expect(openPreview).toHaveBeenCalledOnce();
  });
});
