# Development

```sh
go test ./...
go build ./...
```

The store, forge and blobstore interfaces each have test doubles (`internal/store/memory`, `internal/forge/fake`,
`internal/blobstore/memory`), so handlers are fully testable without Postgres or a forge.

The Postgres store additionally has integration tests that run against a real server when `GOCOV_TEST_DATABASE_URL` is
set (they are skipped otherwise). Each test creates and drops its own scratch database:

```sh
docker run --rm -d --name gocov-test-db -p 5433:5432 \
  -e POSTGRES_USER=gocov -e POSTGRES_PASSWORD=gocov -e POSTGRES_DB=gocov \
  postgres:18-alpine
GOCOV_TEST_DATABASE_URL=postgres://gocov:gocov@localhost:5433/gocov go test ./...
docker stop gocov-test-db
```

`GET /healthz` reports readiness (checks database connectivity) for load balancers and container orchestrators; the
server shuts down gracefully on SIGINT/SIGTERM.

## Extending gocov

Coverage formats sit behind `profile.Parser`, forges behind
`forge.Forge`, and raw profile storage behind `blobstore.Store`; the database schema stores a format-agnostic normalized
model. GitHub and GitLab were each added this way — new formats or S3 storage slot in without rewrites.
