# Database Modeling Dipling

Evidence-aware LLM + DB-DSL workbench for generating database models from textual task specifications.

## Requirements

- Go 1.26+
- Node.js 20+ and npm
- An OpenAI API key for real LLM calls; mock mode works offline

## Run locally

Start the backend from the repository root:

```sh
export OPENAI_API_KEY="your-key"
cd dbdsl
go run ./cmd/dbdsl-server --addr 127.0.0.1:8080
```

In another terminal, start the frontend:

```sh
cd web
npm ci
npm run dev
```

Open <http://127.0.0.1:5173>. The Vite development server proxies `/api` to the backend on port 8080.

The backend creates local runtime state in `.dbdsl_workbench/`; this directory is intentionally ignored by Git.

## Offline development

The UI supports mock LLM calls. Select mock mode when creating a project, or use CLI commands with `--mock` where offered.

## Verify

```sh
cd dbdsl
go test ./...

cd ../web
npm ci
npm test
npm run build
```

## Repository contents

- `dbdsl/` — Go backend, DB-DSL core, CLI and tests
- `web/` — React/Vite workbench
- `poc/` — canonical startup bundle and fixtures required by the backend/test suite
- `fixtures/` — integration-test manifest

Secrets, runtime projects, generated exports, dependency directories and build outputs are not included.
