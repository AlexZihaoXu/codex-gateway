import { mkdir } from 'node:fs/promises';
import { resolve } from 'node:path';

const repositoryRoot = resolve(import.meta.dirname, '..');
const coverageDirectory = resolve(repositoryRoot, 'coverage');
const coverageProfile = resolve(coverageDirectory, 'go.out');
await mkdir(coverageDirectory, { recursive: true });

const test = Bun.spawnSync(
  ['go', 'test', '-p', '1', `-coverprofile=${coverageProfile}`, './internal/...'],
  { cwd: repositoryRoot, stdout: 'inherit', stderr: 'inherit' },
);
if (test.exitCode !== 0) {
  process.exit(test.exitCode);
}

const report = Bun.spawnSync(['go', 'tool', 'cover', `-func=${coverageProfile}`], {
  cwd: repositoryRoot,
  stdout: 'pipe',
  stderr: 'pipe',
});
if (report.exitCode !== 0) {
  process.stderr.write(report.stderr.toString());
  process.exit(report.exitCode);
}

const output = report.stdout.toString();
process.stdout.write(output);
const match = /^total:\s+\(statements\)\s+([\d.]+)%$/m.exec(output);
if (!match?.[1]) {
  throw new Error('Unable to read total Go coverage.');
}

const coverage = Number.parseFloat(match[1]);
if (coverage < 80) {
  process.stderr.write(`Go coverage ${coverage.toFixed(1)}% is below the required 80%.\n`);
  process.exit(1);
}
