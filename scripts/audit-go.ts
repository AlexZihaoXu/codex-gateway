const goModule = await Bun.file('go.mod').text();
const toolchain = /^toolchain\s+(go\S+)$/m.exec(goModule)?.[1];
if (!toolchain) {
  throw new Error('go.mod must pin a Go toolchain for reproducible vulnerability scans.');
}

const audit = Bun.spawn(
  ['go', 'run', 'golang.org/x/vuln/cmd/govulncheck@v1.7.0', './cmd/...', './internal/...'],
  {
    env: { ...process.env, GOTOOLCHAIN: `${toolchain}+auto` },
    stderr: 'inherit',
    stdout: 'inherit',
  },
);

process.exit(await audit.exited);
