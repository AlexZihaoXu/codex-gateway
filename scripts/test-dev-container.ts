const compose = ['docker', 'compose', '--profile', 'dev'] as const;

function runCompose(args: string[]): string {
  const result = Bun.spawnSync([...compose, ...args], {
    stderr: 'pipe',
    stdout: 'pipe',
  });

  if (result.exitCode !== 0) {
    throw new Error(result.stderr.toString().trim() || `docker compose ${args.join(' ')} failed`);
  }

  return result.stdout.toString();
}

const processes = runCompose(['exec', '-T', 'gateway-dev', 'ps', '-o', 'args=']);
if (processes.includes('codex app-server')) {
  throw new Error('Development gateway still supervises a Codex app-server child');
}

const origin = process.env.CODEX_GATEWAY_TEST_ORIGIN ?? 'http://127.0.0.1:11435';
const doc = await fetch(`${origin}/doc`);
const docHTML = await doc.text();
if (
  !doc.ok ||
  !docHTML.includes('<title>Codex Gateway</title>') ||
  !docHTML.includes('id="root"')
) {
  throw new Error(`/doc did not return the Swagger UI; received ${doc.status}`);
}

const docs = await fetch(`${origin}/docs`, { redirect: 'manual' });
if (docs.status !== 308 || docs.headers.get('location') !== '/doc') {
  throw new Error(`/docs did not redirect to /doc; received ${docs.status}`);
}

const clientRoute = await fetch(`${origin}/usage`);
const clientRouteHTML = await clientRoute.text();
if (!clientRoute.ok || !clientRouteHTML.includes('id="root"')) {
  throw new Error(`/usage did not return the dashboard shell; received ${clientRoute.status}`);
}

const schemaResponse = await fetch(`${origin}/openapi.yaml`);
if (!schemaResponse.ok) {
  throw new Error(`/openapi.yaml returned ${schemaResponse.status}`);
}

const schema: unknown = Bun.YAML.parse(await schemaResponse.text());
if (!isRecord(schema) || !('openapi' in schema) || schema.openapi !== '3.1.0') {
  throw new Error('/openapi.yaml is not a valid OpenAPI 3.1 document');
}

if (!isRecord(schema.paths)) {
  throw new Error('/openapi.yaml does not define paths');
}
for (const path of [
  '/api/chat/sessions',
  '/api/chat/sessions/{id}',
  '/v1/models',
  '/v1/responses',
  '/v1/chat/completions',
]) {
  if (!(path in schema.paths)) {
    throw new Error(`/openapi.yaml does not define ${path}`);
  }
}

if (
  !isRecord(schema.components) ||
  !isRecord(schema.components.securitySchemes) ||
  !('ProjectAPIKey' in schema.components.securitySchemes)
) {
  throw new Error('/openapi.yaml does not define the project API key security scheme');
}

const models = await fetch(`${origin}/v1/models`);
if (models.status !== 401) {
  throw new Error(`/v1/models accepted a request without an API key; received ${models.status}`);
}

const chatHistory = await fetch(`${origin}/api/chat/sessions`);
if (chatHistory.status !== 401) {
  throw new Error(
    `/api/chat/sessions accepted a request without an administrator session; received ${chatHistory.status}`,
  );
}

console.log('Development container runtime checks passed.');

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null;
}
