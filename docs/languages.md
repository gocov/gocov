# Languages & formats

Upload whatever your test tool already writes — the format is detected from the file's content, no flag needed. The
snippets below show the plain CLI; in CI, the same file goes in the `files:` of the
[GitHub Action](github-actions.md) or the [Bitbucket pipe](bitbucket-pipelines.md).

| Format          | Written by                                          |
|-----------------|-----------------------------------------------------|
| Go cover profile| `go test -coverprofile`                             |
| LCOV            | Jest, Vitest, nyc, c8, and most JS/TS tools         |
| JaCoCo XML      | Maven, Gradle, Android                              |
| Cobertura XML   | coverage.py / pytest-cov; also coverlet, gcovr      |
| Clover XML      | PHPUnit, Istanbul                                   |
| SimpleCov       | RSpec / minitest with simplecov                     |
| OpenCover XML   | Coverlet, OpenCover (.NET)                          |

## Go

```sh
go test ./... -covermode=atomic -coverprofile=coverage.out
gocov upload coverage.out
```

Go profiles record paths under the module path; the CLI reads it from `go.mod` automatically so diff coverage lines
up. Only when uploading from outside the module directory pass `-path-prefix` yourself.

## JavaScript / TypeScript

```sh
npx jest --coverage             # or vitest run --coverage, nyc, c8 ...
gocov upload coverage/lcov.info
```

## Java / Kotlin

```sh
mvn verify                      # with the jacoco-maven-plugin
gocov upload target/site/jacoco/jacoco.xml
```

```sh
gradle test jacocoTestReport    # xml.required = true
gocov upload build/reports/jacoco/test/jacocoTestReport.xml
```

JaCoCo paths are package-qualified (`com/example/Foo.java`); diff coverage matches them against repo paths by suffix,
so source roots like `src/main/java` need no configuration.

## Python

```sh
pytest --cov --cov-report=xml   # coverage.py / pytest-cov
gocov upload coverage.xml
```

## PHP

```sh
phpunit --coverage-clover clover.xml
gocov upload clover.xml
```

## Ruby

```sh
bundle exec rspec               # with simplecov enabled
gocov upload coverage/.resultset.json
```

## C# / .NET

```sh
dotnet test --collect:"XPlat Code Coverage" --results-directory coverage \
  -- DataCollectionRunSettings.DataCollectors.DataCollector.Configuration.Format=opencover
gocov upload coverage/*/coverage.opencover.xml
```

Coverlet writes each run under a directory with a generated name, hence the wildcard; with more than one test project,
upload each report as its own [part](parts.md). Coverlet's Cobertura output (the collector's default) works too.

OpenCover records every file by its absolute path on the machine the tests ran on. The CLI recognizes the git checkout
directory at the head of those paths and maps them to repo paths itself; when the tests ran somewhere else, such as a
container, pass that directory as `-path-prefix`.

## Several languages in one repo

Upload each report as its own [part](parts.md) — a backend suite, a frontend suite, an e2e run — and gocov merges
them into one report per commit:

```sh
gocov upload -part backend  coverage.out
gocov upload -part frontend coverage/lcov.info
```
