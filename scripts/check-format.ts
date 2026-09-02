const gofmt = Bun.spawnSync(['gofmt', '-l', 'cmd', 'internal'], {
  stdout: 'pipe',
  stderr: 'pipe',
});

if (gofmt.exitCode !== 0) {
  process.stderr.write(gofmt.stderr.toString());
  process.exit(gofmt.exitCode);
}

const unformattedGoFiles = gofmt.stdout.toString().trim();
if (unformattedGoFiles !== '') {
  process.stderr.write(`Go files need formatting:\n${unformattedGoFiles}\n`);
  process.exit(1);
}

const prettier = Bun.spawnSync(['bunx', '--no-install', 'prettier', '--check', '.'], {
  stdout: 'inherit',
  stderr: 'inherit',
});
process.exit(prettier.exitCode);
