# Project workflow

- For authorized development, follow this order: change, validate, commit, push, then deploy to the production server. The user explicitly requested this standing workflow.
- Always exclude the 13 Persian documents under `docs/fa/` from commits and deployment. Do not ask about including them.
- Network-reward development is paused. Keep `backend/internal/networkreward/` outside commits and deployment until the user explicitly resumes it.
- Use `deploy.ps1` for the full production release. It deploys committed source only, builds both applications, preserves server runtime credentials, backs up production, and verifies health.
- Preserve the server's existing services and primary-site access. Moving Xray from port 443 to 24443 was explicitly authorized for enabling seller-domain HTTPS; do not make additional unrelated service changes.
- Never print deployment passwords, private keys, API keys, or full runtime environments. Backups containing secrets stay on the server with restricted permissions.
