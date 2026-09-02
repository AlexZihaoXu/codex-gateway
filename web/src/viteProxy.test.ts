import { describe, expect, it } from 'vitest';

import viteConfig from '../vite.config';

describe('development proxy', () => {
  it('forwards documentation aliases and every OpenAPI representation', () => {
    expect(viteConfig.server?.proxy).toHaveProperty('/docs');
    expect(viteConfig.server?.proxy).not.toHaveProperty('/doc');
    expect(viteConfig.server?.proxy).toHaveProperty('/openapi');
  });
});
