function composeOutput(args: string[]): string[] {
  const result = Bun.spawnSync(['docker', 'compose', ...args], {
    stderr: 'pipe',
    stdout: 'pipe',
  });

  if (result.exitCode !== 0) {
    throw new Error(result.stderr.toString().trim() || 'docker compose failed');
  }

  return result.stdout
    .toString()
    .trim()
    .split(/\r?\n/u)
    .filter((line) => line.length > 0);
}

function expectLines(actual: string[], expected: string[], description: string): void {
  if (actual.join('\n') !== expected.join('\n')) {
    throw new Error(
      `${description}: expected ${expected.join(', ')}, received ${actual.join(', ')}`,
    );
  }
}

expectLines(composeOutput(['config', '--profiles']), ['dev', 'prod'], 'Compose profiles');
expectLines(
  composeOutput(['--profile', 'dev', 'config', '--services']),
  ['gateway-dev'],
  'Development services',
);
expectLines(
  composeOutput(['--profile', 'prod', 'config', '--services']),
  ['gateway'],
  'Production services',
);

composeOutput(['--profile', 'dev', 'config', '--quiet']);
composeOutput(['--profile', 'prod', 'config', '--quiet']);

const developmentConfig: unknown = JSON.parse(
  composeOutput(['--profile', 'dev', 'config', '--format', 'json']).join('\n'),
);
if (
  !isRecord(developmentConfig) ||
  !isRecord(developmentConfig.services) ||
  !isRecord(developmentConfig.services['gateway-dev']) ||
  !Array.isArray(developmentConfig.services['gateway-dev'].command) ||
  !developmentConfig.services['gateway-dev'].command.some(
    (part) => typeof part === 'string' && part.includes('bun install --frozen-lockfile'),
  )
) {
  throw new Error('Development service does not reconcile its persistent dependency volume');
}

const productionConfig: unknown = JSON.parse(
  composeOutput(['--profile', 'prod', 'config', '--format', 'json']).join('\n'),
);
for (const [name, config] of [
  ['development', developmentConfig],
  ['production', productionConfig],
] as const) {
  if (!isRecord(config) || !isRecord(config.services)) {
    throw new Error(`${name} Compose configuration has no services`);
  }
  for (const service of Object.values(config.services)) {
    if (
      isRecord(service) &&
      isRecord(service.environment) &&
      Object.hasOwn(service.environment, 'CODEX_HOME')
    ) {
      throw new Error(`${name} service still configures the removed Codex child runtime`);
    }
  }
}

const dockerfile = await Bun.file('Dockerfile').text();
const developmentRuntime = dockerfile.match(
  /FROM bun-base AS development-runtime(?<body>[\s\S]*?)FROM development-runtime AS development/u,
);
if (!developmentRuntime?.groups?.body.includes('ca-certificates')) {
  throw new Error('Development image must install CA certificates before downloading Go modules');
}
for (const removedRuntimeTrace of ['codex-install', 'CODEX_HOME', 'install-codex.sh']) {
  if (dockerfile.includes(removedRuntimeTrace)) {
    throw new Error(`Dockerfile still contains removed runtime trace: ${removedRuntimeTrace}`);
  }
}

console.log('Docker Compose dev and prod profiles are valid.');

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null;
}
