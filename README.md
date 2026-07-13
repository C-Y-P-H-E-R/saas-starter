# saas-starter

Multi-tenant SaaS starter: auth, org/role tenancy, Stripe billing, and a
projects/tasks CRUD app. Backend in Go, deployed to Render. Frontend lives
in the `portfolio-site` repo under `/app/saas-starter/*`.

## Local development

Requires a local Postgres. Export `DATABASE_URL` before running tests or
the server, e.g.:

    export DATABASE_URL=postgres://postgres:postgres@localhost:5432/saas_starter_dev?sslmode=disable
    GOWORK=off go run .

## Testing

    GOWORK=off go test ./...

Tests that touch Postgres skip automatically if `DATABASE_URL` isn't set.

Note: if this checkout sits under a parent Go workspace (a `go.work` file
above this directory that doesn't list this module), plain `go` commands
fail with "directory prefix . does not contain modules listed in go.work" —
`GOWORK=off` bypasses that.
