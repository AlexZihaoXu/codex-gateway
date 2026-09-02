import { rm } from 'node:fs/promises';
import { isAbsolute, relative, resolve } from 'node:path';

const repositoryRoot = resolve(import.meta.dirname, '..');
const targets = ['build', 'coverage', 'internal/dashboard/dist'];

for (const target of targets) {
  const absoluteTarget = resolve(repositoryRoot, target);
  const relativeTarget = relative(repositoryRoot, absoluteTarget);
  if (relativeTarget === '' || relativeTarget.startsWith('..') || isAbsolute(relativeTarget)) {
    throw new Error(`Refusing to clean unsafe path: ${absoluteTarget}`);
  }
  await rm(absoluteTarget, { force: true, recursive: true });
}
