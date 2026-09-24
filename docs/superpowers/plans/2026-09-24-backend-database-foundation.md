# Backend Database Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make backend schema changes ordered, auditable, and safe on concurrent startup; require authenticated PostgreSQL TLS when enabled.

**Architecture:** Embed ordered SQL files and apply them in a single transaction protected by a PostgreSQL advisory transaction lock. Track name and SHA-256 digest in `schema_migrations`; baseline `0001_init.sql` remains idempotent for already deployed databases. Let pgx parse TLS settings and reject any unverified or plaintext fallback when `DATABASE_SSL` is enabled.

**Tech Stack:** Go 1.25, pgx/v5, PostgreSQL 16, embedded SQL, Go tests.

**Spec:** `docs/superpowers/specs/2026-09-24-backend-mobile-platform-design.md`

## Global Constraints

- Preserve existing data and the deployed cookie API.
- Do not run local Docker, services, or PowerShell; CI performs integration tests.
- Keep the Windows Compose deployment working with `DATABASE_SSL=false`.
- Do not log connection strings, secrets, or SQL bodies.
- Do not change the contents of `0001_init.sql` after recording its hash.

## Review Focus

- Concurrent API startups must not execute the baseline twice.
- Existing databases with the old schema and no `schema_migrations` must be adopted.
- A modified migration must abort before applying later migrations.
- A TLS primary with an unencrypted fallback must be rejected.
- An invalid `DATABASE_SSL` value must fail closed.

---

### Task 1: Versioned migration catalog and runner

**Files:**
- Modify: `backend/migrations/migrations.go`
- Modify: `backend/internal/db/db.go`
- Test: `backend/internal/db/migrations_test.go`

**Interfaces:**
- Consumes: existing `0001_init.sql`, `DB.Migrate(context.Context)` call at API startup.
- Produces: `migrations.All() ([]migrations.Migration, error)` where `Migration` has `Name`, `SQL`, and `Checksum`; `DB.Migrate` records applied checksums.

- [ ] **Step 1: Add catalog tests before production code.** Verify `All()` returns `0001_init.sql`, its SHA-256, and names strictly ordered. Keep the existing baseline content test.
- [ ] **Step 2: Run `cd backend && go test ./internal/db ./migrations` and confirm a compile failure because `All` is absent.**
- [ ] **Step 3: Embed `*.sql`, validate names `NNNN_name.sql`, sort, reject duplicate versions and empty files, compute SHA-256.** Keep `InitialSchema` for existing tests.
- [ ] **Step 4: Add tests for migration decision logic before implementing it.** Given applied checksums, require the pending suffix; reject changed hashes and missing older applied versions. Run the focused tests and see the expected failure.
- [ ] **Step 5: Implement `DB.Migrate`: acquire one connection, begin a transaction, take one constant advisory transaction lock, create `schema_migrations`, load applied rows, compare all known migrations, execute pending SQL and insert checksums, commit. Roll back on any failure.** A previous deployment with no ledger replays the idempotent baseline once and records it.
- [ ] **Step 6: Add CI-only integration assertions under `TEST_DATABASE_URL`: two repeated calls record one baseline row.** Do not alter existing data or delete the ledger.
- [ ] **Step 7: Run `cd backend && go test ./...` and commit the focused change.**

### Task 2: PostgreSQL TLS and config validation

**Files:**
- Modify: `backend/internal/db/db.go`
- Remove: `backend/internal/db/tls.go`
- Modify: `backend/cmd/api/main.go`
- Test: `backend/internal/db/tls_test.go`
- Test: `backend/cmd/api/main_test.go`
- Modify: `backend/.env.example`
- Modify: `backend/README.md`

**Interfaces:**
- Consumes: `DATABASE_URL` and the existing `DATABASE_SSL` flag.
- Produces: parsed pgx config that rejects unverified TLS or plaintext fallback when the flag is enabled; strict boolean parsing at process start.

- [ ] **Step 1: Write tests that `DATABASE_SSL=true` rejects `sslmode=disable`, `prefer`, `require`, and `verify-ca`, but accepts `verify-full`; test fallback validation and an invalid flag.** Run focused tests to see failure.
- [ ] **Step 2: Extract `parsePoolConfig(databaseURL, requireSSL)` and validate pgx's parsed primary and every fallback TLS config.** Require `TLSConfig != nil`, `InsecureSkipVerify == false`, nonempty `ServerName`; leave pgx's CA settings untouched.
- [ ] **Step 3: Replace permissive `normalizeBool` with strict `parseDatabaseSSL`, allowing blank/false/0/no/off and true/1/yes/on/require, returning an error otherwise.** Fail before connecting.
- [ ] **Step 4: Remove insecure override, update example and backend instructions to use `sslmode=verify-full` and a trusted CA for production.** Keep the current local default.
- [ ] **Step 5: Run focused tests, `cd backend && go test ./...`, `git diff --check`, then commit.**

### Task 3: Final verification and handoff

**Files:**
- Review: all files changed by Tasks 1 and 2.

**Interfaces:**
- Consumes: both implemented tasks.
- Produces: a tested first epic ready for CI and review, with later epics remaining separate.

- [ ] **Step 1: Run the complete non-runtime quality command `npm run quality` if web dependencies are present, otherwise run all available backend and API-doc tests and state the limitation.**
- [ ] **Step 2: Inspect the diff, confirm no public routes or Compose services changed, and review startup behavior on an existing database.**
- [ ] **Step 3: Run a fresh review of the branch, fix material findings, and report the exact commit(s) and remaining CI-only verification.**
