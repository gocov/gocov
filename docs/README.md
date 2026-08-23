# gocov documentation

Start here if you are new: [Getting started](getting-started.md) takes a fresh instance from `docker compose up` to the
first coverage upload.

## Contents

| Page                                      | Covers                                                                              |
|-------------------------------------------|-------------------------------------------------------------------------------------|
| [Features](features.md)                   | What gocov does, surface by surface, and how the architecture extends               |
| [Getting started](getting-started.md)     | Quick start, onboarding, workspace settings, hosted mode                            |
| [Sign-in](sign-in.md)                     | Enabling web UI sign-in with Bitbucket, GitHub and/or GitLab; the access model      |
| [Forge connections](forge-connections.md) | GitHub App, Bitbucket and GitLab one-click connect — setup and behavior             |
| [Uploading from CI](ci-upload.md)         | Bitbucket Pipelines, GitHub Actions, GitLab CI, prebuilt binaries, other ecosystems |
| [Coverage gate](coverage-gate.md)         | Minimum/diff/drop rules and making them block merges                                |
| [Parts](parts.md)                         | Combining multiple reports per commit from separate CI jobs                         |
| [API & badge](api.md)                     | The upload API and the SVG badge                                                    |
| [Configuration](configuration.md)         | Every environment variable                                                          |
| [Development](development.md)             | Building, testing, health checks                                                    |

## Conventions

Throughout these docs, *forge* means the code host (Bitbucket Cloud, GitHub or GitLab) and *workspace* means the
namespace gocov tracks — a Bitbucket workspace, a GitHub org or user, or a GitLab group, subgroup (by full path) or user
namespace.
