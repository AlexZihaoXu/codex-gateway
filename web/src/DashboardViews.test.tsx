import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { APIKeysView, ProjectsView, SettingsView, UsageView } from './DashboardViews';
import { formatCodexPlanName } from './CodexAccountCard';

describe('dashboard management views', () => {
  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it('creates a project from the empty state', async () => {
    const fetchMock = mockFetch(
      jsonResponse({ projects: [] }),
      jsonResponse(
        {
          created_at: '2026-09-02T15:00:00Z',
          id: 'prj_1',
          limits: [],
          name: 'OpenCode',
        },
        201,
      ),
    );
    render(<ProjectsView />);

    expect(await screen.findByText('No projects yet')).toBeVisible();
    fireEvent.click(screen.getByRole('button', { name: 'Create project' }));
    fireEvent.change(screen.getByLabelText('Project name'), { target: { value: 'OpenCode' } });
    fireEvent.click(screen.getByRole('button', { name: 'Create project' }));

    expect(await screen.findByRole('rowheader', { name: 'OpenCode' })).toBeVisible();
    expect(screen.getByRole('gridcell', { name: 'Unlimited' })).toBeVisible();
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      '/api/projects',
      expect.objectContaining({ body: JSON.stringify({ name: 'OpenCode' }), method: 'POST' }),
    );
  });

  it('reports a project list failure', async () => {
    mockFetch(jsonResponse({ detail: 'Database is unavailable.' }, 500));
    render(<ProjectsView />);

    expect(await screen.findByText('Database is unavailable.')).toBeVisible();
    expect(screen.getByText('Gateway request failed')).toBeVisible();
  });

  it('adds, validates, and removes multiple usage-limit rows', async () => {
    mockFetch(jsonResponse({ projects: [] }));
    render(<ProjectsView />);

    expect(await screen.findByText('No projects yet')).toBeVisible();
    fireEvent.click(screen.getByRole('button', { name: 'Create project' }));
    fireEvent.change(screen.getByLabelText('Project name'), { target: { value: 'OpenCode' } });
    fireEvent.click(screen.getByRole('button', { name: 'Add usage limit' }));
    fireEvent.click(screen.getByRole('button', { name: 'Add usage limit' }));

    expect(screen.getAllByRole('textbox', { name: /Token limit/ })).toHaveLength(2);
    expect(screen.getByText('Each interval window must be unique.')).toBeVisible();
    expect(screen.getByRole('button', { name: 'Create project' })).toBeDisabled();
    fireEvent.input(screen.getByRole('textbox', { name: 'Interval 2' }), {
      target: { value: '2' },
    });
    fireEvent.blur(screen.getByRole('textbox', { name: 'Interval 2' }));
    expect(screen.queryByText('Each interval window must be unique.')).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Create project' })).toBeEnabled();

    fireEvent.click(screen.getByRole('button', { name: 'Remove usage limit 1' }));
    expect(screen.getAllByRole('textbox', { name: /Token limit/ })).toHaveLength(1);
  });

  it('edits and deletes a project only after explicit confirmation', async () => {
    const fetchMock = mockFetch(
      jsonResponse({
        projects: [
          { created_at: '2026-09-02T15:00:00Z', id: 'prj_1', limits: [], name: 'OpenCode' },
        ],
      }),
      jsonResponse({
        created_at: '2026-09-02T15:00:00Z',
        id: 'prj_1',
        limits: [{ interval_count: 6, interval_unit: 'hour', token_limit: 500000 }],
        name: 'OpenCode Desktop',
      }),
      jsonResponse({ status: 'ok' }),
    );
    render(<ProjectsView />);

    expect(await screen.findByRole('rowheader', { name: 'OpenCode' })).toBeVisible();
    fireEvent.click(screen.getByRole('button', { name: 'Edit OpenCode' }));
    expect(await screen.findByRole('heading', { name: 'Edit project' })).toBeVisible();
    fireEvent.change(screen.getByLabelText('Project name'), {
      target: { value: 'OpenCode Desktop' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Add usage limit' }));
    const limitInput = screen.getByRole('textbox', { name: 'Token limit 1' });
    fireEvent.input(limitInput, {
      target: { value: '500000' },
    });
    fireEvent.blur(limitInput);
    const intervalInput = screen.getByRole('textbox', { name: 'Interval 1' });
    fireEvent.input(intervalInput, { target: { value: '6' } });
    fireEvent.blur(intervalInput);
    fireEvent.click(screen.getByRole('button', { name: 'Save changes' }));

    expect(await screen.findByRole('rowheader', { name: 'OpenCode Desktop' })).toBeVisible();
    expect(screen.getByRole('gridcell', { name: '500,000 every 6 hours' })).toBeVisible();
    fireEvent.click(screen.getByRole('button', { name: 'Delete OpenCode Desktop' }));
    expect(await screen.findByRole('alertdialog')).toHaveTextContent(
      'Deletes this project, all of its API keys, and its recorded usage. This cannot be undone.',
    );
    fireEvent.click(screen.getByRole('button', { name: 'Delete project' }));

    expect(await screen.findByText('No projects yet')).toBeVisible();
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      '/api/projects/prj_1',
      expect.objectContaining({
        body: JSON.stringify({
          limits: [{ interval_count: 6, interval_unit: 'hour', token_limit: 500000 }],
          name: 'OpenCode Desktop',
        }),
        method: 'PUT',
      }),
    );
    expect(fetchMock).toHaveBeenNthCalledWith(
      3,
      '/api/projects/prj_1',
      expect.objectContaining({ method: 'DELETE' }),
    );
  });

  it('creates an API key, shows it once, and copies it', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText },
    });
    const key = {
      created_at: '2026-09-02T15:00:00Z',
      id: 'key_1',
      name: 'Development',
      prefix: 'cg_example',
      project_id: 'prj_1',
    };
    mockFetch(
      jsonResponse({
        projects: [
          { created_at: '2026-09-02T15:00:00Z', id: 'prj_1', limits: [], name: 'OpenCode' },
        ],
      }),
      jsonResponse({ api_keys: [] }),
      jsonResponse({ api_key: key, secret: 'cg_plaintext_once' }, 201),
    );
    render(<APIKeysView />);

    const emptyTitle = await screen.findByText('No API keys yet');
    expect(emptyTitle).toBeVisible();
    expect(emptyTitle).toHaveClass('typography--align-center');
    expect(
      screen.getByText('Keys are displayed once at creation and stored only as hashes.'),
    ).toHaveClass('typography--align-center');
    fireEvent.click(screen.getByRole('button', { name: 'Create API key' }));
    fireEvent.change(screen.getByLabelText('Key name'), { target: { value: 'Development' } });
    fireEvent.click(screen.getByRole('button', { name: 'Create API key' }));

    expect(await screen.findByText('cg_plaintext_once')).toBeVisible();
    fireEvent.click(screen.getByRole('button', { name: 'Copy key' }));
    await waitFor(() => expect(writeText).toHaveBeenCalledWith('cg_plaintext_once'));
    expect(await screen.findByRole('button', { name: 'Copied' })).toBeVisible();
    fireEvent.click(screen.getByRole('button', { name: 'Done' }));
    expect(screen.queryByText('cg_plaintext_once')).not.toBeInTheDocument();
    expect(screen.getByRole('rowheader', { name: 'Development' })).toBeVisible();
  });

  it('revokes an API key only after confirmation', async () => {
    const key = {
      created_at: '2026-09-02T15:00:00Z',
      id: 'key_1',
      name: 'Development',
      prefix: 'cg_example',
      project_id: 'prj_1',
    };
    const fetchMock = mockFetch(
      jsonResponse({
        projects: [
          { created_at: '2026-09-02T15:00:00Z', id: 'prj_1', limits: [], name: 'OpenCode' },
        ],
      }),
      jsonResponse({ api_keys: [key] }),
      jsonResponse({ status: 'ok' }),
    );
    render(<APIKeysView />);

    expect(await screen.findByRole('rowheader', { name: 'Development' })).toBeVisible();
    expect(screen.getByRole('rowheader', { name: 'Development' }).closest('section')).toHaveClass(
      'w-full',
      'min-w-0',
    );
    fireEvent.click(screen.getByRole('button', { name: 'Revoke' }));
    expect(await screen.findByRole('alertdialog')).toBeVisible();
    fireEvent.click(screen.getByRole('button', { name: 'Revoke key' }));

    expect(await screen.findByText('No API keys yet')).toBeVisible();
    expect(screen.queryByText('Revoked')).not.toBeInTheDocument();
    expect(screen.queryByRole('rowheader', { name: 'Development' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Revoke' })).not.toBeInTheDocument();
    expect(fetchMock).toHaveBeenNthCalledWith(
      3,
      '/api/api-keys/key_1',
      expect.objectContaining({ method: 'DELETE' }),
    );
  });

  it('reports a clipboard failure without hiding the one-time key', async () => {
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText: vi.fn().mockRejectedValue(new Error('Clipboard unavailable')) },
    });
    const key = {
      created_at: '2026-09-02T15:00:00Z',
      id: 'key_1',
      name: 'Development',
      prefix: 'cg_example',
      project_id: 'prj_1',
    };
    mockFetch(
      jsonResponse({
        projects: [
          { created_at: '2026-09-02T15:00:00Z', id: 'prj_1', limits: [], name: 'OpenCode' },
        ],
      }),
      jsonResponse({ api_keys: [] }),
      jsonResponse({ api_key: key, secret: 'cg_plaintext_once' }, 201),
    );
    render(<APIKeysView />);

    expect(await screen.findByText('No API keys yet')).toBeVisible();
    fireEvent.click(screen.getByRole('button', { name: 'Create API key' }));
    fireEvent.change(screen.getByLabelText('Key name'), { target: { value: 'Development' } });
    fireEvent.click(screen.getByRole('button', { name: 'Create API key' }));
    expect(await screen.findByText('cg_plaintext_once')).toBeVisible();

    fireEvent.click(screen.getByRole('button', { name: 'Copy key' }));

    expect(await screen.findByText('Clipboard unavailable')).toBeVisible();
    expect(screen.getByText('cg_plaintext_once')).toBeVisible();
    expect(screen.getByRole('button', { name: 'Copy key' })).toBeVisible();
  });

  it('shows monthly usage metrics and project progress', async () => {
    mockFetch(
      jsonResponse({
        api_keys: [
          {
            api_key_id: 'key_1',
            cached_input_tokens: 35,
            input_tokens: 200,
            name: 'Development',
            output_tokens: 50,
            prefix: 'cg_example',
            project_id: 'prj_1',
            reasoning_output_tokens: 13,
            requests: 2,
            total_tokens: 250,
          },
        ],
        cached_input_tokens: 35,
        input_tokens: 200,
        output_tokens: 50,
        period_end: '2026-10-01T00:00:00Z',
        period_start: '2026-09-01T00:00:00Z',
        projects: [
          {
            cached_input_tokens: 35,
            input_tokens: 200,
            limits: [
              {
                cached_input_tokens: 35,
                input_tokens: 200,
                interval_count: 6,
                interval_unit: 'hour',
                output_tokens: 50,
                period_end: '2026-09-02T18:00:00Z',
                period_start: '2026-09-02T12:00:00Z',
                reasoning_output_tokens: 13,
                requests: 2,
                token_limit: 1000,
                total_tokens: 250,
              },
            ],
            name: 'OpenCode',
            output_tokens: 50,
            project_id: 'prj_1',
            requests: 2,
            reasoning_output_tokens: 13,
            total_tokens: 250,
          },
          {
            cached_input_tokens: 0,
            input_tokens: 0,
            limits: [],
            name: 'Scripts',
            output_tokens: 0,
            project_id: 'prj_2',
            requests: 0,
            reasoning_output_tokens: 0,
            total_tokens: 0,
          },
        ],
        requests: 2,
        reasoning_output_tokens: 13,
        total_tokens: 250,
      }),
    );
    render(<UsageView />);

    expect(await screen.findByText('OpenCode')).toBeVisible();
    expect(screen.getByText('Scripts')).toBeVisible();
    expect(screen.getByText('250 / 1,000 tokens every 6 hours')).toBeVisible();
    expect(screen.getByText('0 tokens')).toBeVisible();
    const apiKeyUsageTable = screen.getByRole('grid', { name: 'API key usage' });
    expect(apiKeyUsageTable).toBeVisible();
    expect(apiKeyUsageTable.closest('.table-root')).toHaveClass('table-root--primary');
    expect(screen.getByRole('rowheader', { name: 'Development' })).toBeVisible();
    expect(screen.getByRole('gridcell', { name: 'cg_example...' })).toBeVisible();
    expect(screen.getByRole('gridcell', { name: '35' })).toBeVisible();
    expect(screen.getByRole('gridcell', { name: '13' })).toBeVisible();
    expect(
      screen.getByRole('progressbar', { name: 'OpenCode 6-hour token usage' }),
    ).toHaveAttribute('aria-valuenow', '25');
  });

  it('shows an empty usage period and the connected account setting', async () => {
    mockFetch(
      jsonResponse({
        api_keys: [],
        cached_input_tokens: 0,
        input_tokens: 0,
        output_tokens: 0,
        period_end: '2026-10-01T00:00:00Z',
        period_start: '2026-09-01T00:00:00Z',
        projects: [],
        requests: 0,
        reasoning_output_tokens: 0,
        total_tokens: 0,
      }),
      jsonResponse({
        connected: true,
        plan_type: 'plus',
        requires_openai_auth: true,
        type: 'chatgpt',
      }),
    );
    const { rerender } = render(<UsageView />);
    expect(await screen.findByText('No usage this month')).toBeVisible();

    rerender(<SettingsView />);
    expect(await screen.findByText('ChatGPT account connected')).toBeVisible();
    expect(screen.getAllByText('Loopback only').length).toBeGreaterThan(0);
  });

  it('labels the connected plan and disconnects Codex only after confirmation', async () => {
    const fetchMock = mockFetch(
      jsonResponse({
        connected: true,
        plan_type: 'prolite',
        requires_openai_auth: true,
        type: 'chatgpt',
      }),
      jsonResponse({ status: 'ok' }),
    );
    render(<SettingsView />);

    expect(await screen.findByText('Pro 5x')).toBeVisible();
    const disconnectButton = screen.getByRole('button', { name: 'Disconnect Codex account' });
    expect(disconnectButton.closest('.card__footer')).toHaveClass('mt-auto');
    expect(
      screen.getByText('This account supplies models for project API requests.'),
    ).toBeVisible();
    fireEvent.click(disconnectButton);
    expect(await screen.findByRole('alertdialog')).toHaveTextContent(
      'Disconnects the local Codex session. Project API requests will stop until you connect again.',
    );
    fireEvent.click(screen.getByRole('button', { name: 'Disconnect account' }));

    expect(await screen.findByRole('button', { name: 'Connect account' })).toBeVisible();
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      '/api/codex/logout',
      expect.objectContaining({ credentials: 'same-origin', method: 'POST' }),
    );
  });

  it('formats known and future Codex plan identifiers for display', () => {
    expect(formatCodexPlanName('free')).toBe('Free');
    expect(formatCodexPlanName('go')).toBe('Go');
    expect(formatCodexPlanName('plus')).toBe('Plus');
    expect(formatCodexPlanName('prolite')).toBe('Pro 5x');
    expect(formatCodexPlanName('pro')).toBe('Pro 20x');
    expect(formatCodexPlanName('future_enterprise-tier<script>')).toBe(
      'Future Enterprise Tier Script',
    );
  });
});

function jsonResponse(body: object, status = 200): Response {
  return new Response(JSON.stringify(body), {
    headers: { 'Content-Type': 'application/json' },
    status,
  });
}

function mockFetch(...responses: Response[]) {
  const fetchMock = vi.fn();
  for (const response of responses) {
    fetchMock.mockResolvedValueOnce(response);
  }
  vi.stubGlobal('fetch', fetchMock);
  return fetchMock;
}
