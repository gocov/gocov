# gocov documentation

[Getting started](getting-started.md) is the short path to a first coverage report: sign in at
[app.gocov.dev](https://app.gocov.dev/?ref=docs) or run your own instance, add an upload step to CI, and coverage lands
on the commit, in the pull request and on the badge.

## Contents

| Page                                      | Covers                                                                              |
|-------------------------------------------|-------------------------------------------------------------------------------------|
| [Getting started](getting-started.md)     | Hosted or your own instance, onboarding, workspace settings                           |
| [Forge connections](forge-connections.md) | GitHub App, Bitbucket and GitLab one-click connect — setup and behavior             |
| [Uploading coverage](ci-upload.md)        | Pipelines, Actions, GitLab CI, prebuilt binaries, other ecosystems                    |
| [Coverage gate](coverage-gate.md)         | Minimum/diff/drop rules and making them block merges                                |
| [Parts](parts.md)                         | Combining multiple reports per commit from separate CI jobs                         |
| [Why coverage changed](coverage-changed.md) | Baselines, missing parts, empty diff coverage — the usual surprises                     |
| [API & badge](api.md)                     | The upload API and the SVG badge                                                    |
| [Features](features.md)                   | What gocov does, surface by surface, and how the architecture extends               |
| [Development](development.md)             | Building, testing, the configuration contract                                        |
| [Self-hosting](self-hosting.md)           | Running an instance for real: TLS, Postgres, the secret key, upgrades                |
| [Sign-in](sign-in.md)                     | Enabling web UI sign-in per forge; the access model, hosted mode                     |
| [Configuration](configuration.md)         | Every environment variable                                                           |

## Conventions

Throughout these docs, *forge* means the code host (Bitbucket Cloud, GitHub or GitLab) and *workspace* means the
namespace gocov tracks — a Bitbucket workspace, a GitHub org or user, or a GitLab group, subgroup (by full path) or user
namespace.
