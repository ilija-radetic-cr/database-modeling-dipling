# Database Modeling Dipling

Evidence-aware LLM + DB-DSL workbench for turning textual task specifications into traceable relational database models.

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

The backend creates local runtime state, project artifacts and LLM audit logs in `.dbdsl_workbench/`; this directory is intentionally ignored by Git.

The application workflow covers source intake, source segmentation, deterministic text normalization, source-unit review, requirement extraction, functional and CRUD analysis, human review, conceptual modeling, logical DB-DSL projection, validation and final DBML generation.

## Offline development

The UI and backend support mock LLM calls. Select mock mode when creating a project, or use CLI commands with `--mock` where offered. To run the frontend without a backend, start it with `VITE_API_MODE=mock npm run dev`.

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
- `go.work` — repository-level Go workspace configuration

Secrets, runtime projects, generated exports, dependency directories and build outputs are not included.
