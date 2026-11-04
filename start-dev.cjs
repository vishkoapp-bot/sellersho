const { spawn } = require('child_process');

console.log('Starting Go API backend and services via Docker...');
const docker = spawn('cmd.exe', ['/c', 'docker compose -f backend/docker-compose.yml up -d'], {
  stdio: 'inherit',
  shell: false,
});

docker.on('close', (code) => {
  console.log(`Docker services ready (exit code ${code}). Starting Vite frontend on port 5173...`);
  const vite = spawn('cmd.exe', ['/c', 'pnpm exec vite --port 5173 --host'], {
    stdio: 'inherit',
    shell: false,
  });
});
