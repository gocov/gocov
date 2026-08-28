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

## Configuration

Every environment variable the binaries read is declared as a tagged struct field in `internal/config`:

```go
type Server struct {
	DatabaseURL       string   `env:"DATABASE_URL,required,notEmpty"`
	Addr              string   `env:"GOCOV_ADDR" envDefault:":8080"`
	AllowedWorkspaces []string `env:"GOCOV_ALLOWED_WORKSPACES" envSeparator:","`
	GitHub            OAuthApp `envPrefix:"GOCOV_OAUTH_GITHUB_"`
	...
}
```

That package is the authoritative list, and a test enforces it: `TestConfigurationDocIsInSync` walks the struct tags
with `env.GetFieldParams` and fails if a variable has no row in [configuration](configuration.md), if a row survives a
variable that was removed, or if a documented default no longer matches the tag. `main` parses and validates
once at start-up and then only reads the struct, so a new setting means a new field there, not another `os.Getenv` at
the point of use. Presence is the tags' job — `required,notEmpty`, because `required` alone
would accept a variable passed through as `""`. Only what the tag vocabulary cannot say is written out: `validate` for
what must stop the process (a malformed `GOCOV_SECRET_KEY`, `GOCOV_MODE=hosted` with no sign-in provider) and
`Warnings` for what is survivable (half a credential pair — logged, feature left off). `LoadServerFrom` parses an explicit environment map
instead of the process environment, so the whole contract is covered by ordinary table tests.

## Extending gocov

Coverage formats sit behind `profile.Parser`, forges behind
`forge.Forge`, and raw profile storage behind `blobstore.Store`; the database schema stores a format-agnostic normalized
model. GitHub and GitLab were each added this way — new formats or S3 storage slot in without rewrites.
