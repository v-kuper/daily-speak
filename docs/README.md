# Documentation map

The files in this directory describe the system that exists on `main`. Pull
requests and Git history retain completed implementation plans; those plans do
not remain here as competing architecture documentation.

## Canonical documents

- [`ARCHITECTURE.md`](ARCHITECTURE.md): service boundaries, ownership, data and
  request flows, and scaling model.
- [`api-compatibility.md`](api-compatibility.md): stable `/api/v1` compatibility
  and deprecation rules for mobile clients.
- [`BACKEND_OPERATIONS.md`](BACKEND_OPERATIONS.md): readiness, limits, metrics,
  scaling, backups, load tests, and failure drills.
- [`LOCAL_WINDOWS_CICD.md`](LOCAL_WINDOWS_CICD.md): current Windows test-host
  deployment and GitHub Actions configuration.
- [`TECH_DEBT.md`](TECH_DEBT.md): unfinished platform work, ordered by priority.

Component-specific instructions remain next to their owners:

- [`../web/README.md`](../web/README.md)
- [`../backend/README.md`](../backend/README.md)
- [`../backend/docs/README.md`](../backend/docs/README.md)
- [`../backend/tools/whisper/README.md`](../backend/tools/whisper/README.md)

The generated OpenAPI artifact is
[`../backend/docs/openapi.json`](../backend/docs/openapi.json). It is the API
schema source of truth and is served by the backend at `/openapi.json`; Swagger
UI is served at `/docs`.

## Maintenance rule

Update the owning canonical document in the same pull request as an
architecture, deployment, API, or operational change. Temporary implementation
plans, local screenshots, agent prompts, absolute workstation paths, and
completed checklists do not belong on the default branch.
