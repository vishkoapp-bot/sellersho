import { readdir } from 'node:fs/promises';
import { resolve, relative } from 'node:path';
import { spawn } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const root = fileURLToPath(new URL('../', import.meta.url));
async function collect(directory) {
  const entries = await readdir(directory, { withFileTypes: true });
  const groups = await Promise.all(entries.map(entry => {
    const path = resolve(directory, entry.name);
    return entry.isDirectory() ? collect(path) : /\.test\.tsx?$/.test(entry.name) ? [relative(root, path)] : [];
  }));
  return groups.flat();
}
const files = (await collect(resolve(root, 'src'))).sort();
if (files.length === 0) throw new Error('No frontend test files found');
const child = spawn(process.execPath, ['--import', 'tsx', '--test', '--test-concurrency=2', '--test-timeout=120000', ...files], {
  cwd: root, stdio: 'inherit', shell: false,
});
child.on('error', error => { console.error(error); process.exitCode = 1; });
child.on('exit', (code, signal) => { process.exitCode = signal ? 1 : code ?? 1; });
