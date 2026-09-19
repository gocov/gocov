// The CI recipe, built in the browser from SetupInfo. The four snippets are
// the ones the old onboarding wizard rendered server-side, with one thing
// added: a language dimension, because gocov reads six formats and the
// wizard only ever showed Go. The CLI detects the format from the file, so
// nothing here passes -format; only the test command and the file path move.
//
// The pinned release always comes from `cliVersion` (the server's
// hosted.PinnedCLIVersion), never from a constant here — release-please
// bumps one place.

import type { Forge } from "./api/types";

export type LanguageId = "go" | "js" | "java" | "python" | "php" | "ruby";

export interface LanguageSpec {
  id: LanguageId;
  /** The picker's label. */
  label: string;
  /** The command that writes the coverage file. */
  test: string;
  /** Where it leaves it. */
  file: string;
  /** The container the GitLab and Bitbucket recipes run in. */
  image: string;
}

/** Straight from docs/languages.md: what each tool writes, and where. */
export const languages: LanguageSpec[] = [
  {
    id: "go",
    label: "Go",
    test: "go test ./... -covermode=atomic -coverprofile=coverage.out",
    file: "coverage.out",
    image: "golang:1.27",
  },
  {
    id: "js",
    label: "JS / TS",
    test: "npx jest --coverage",
    file: "coverage/lcov.info",
    image: "node:24",
  },
  {
    id: "java",
    label: "Java / Kotlin",
    test: "mvn verify",
    file: "target/site/jacoco/jacoco.xml",
    image: "maven:3-eclipse-temurin-21",
  },
  {
    id: "python",
    label: "Python",
    test: "pytest --cov --cov-report=xml",
    file: "coverage.xml",
    image: "python:3.13",
  },
  {
    id: "php",
    label: "PHP",
    test: "phpunit --coverage-clover clover.xml",
    file: "clover.xml",
    image: "php:8.4-cli",
  },
  {
    id: "ruby",
    label: "Ruby",
    test: "bundle exec rspec",
    file: "coverage/.resultset.json",
    image: "ruby:3.4",
  },
];

/** Unknown ids (a stale localStorage value) fall back to Go. */
export const languageSpec = (id: string): LanguageSpec => languages.find((l) => l.id === id) ?? languages[0]!;

export interface SnippetInput {
  forge: Forge;
  language: LanguageId;
  /** The workspace is connected: the job authenticates with an identity token. */
  tokenless: boolean;
  /** The hosted service: the CLI already knows the server. */
  serverImplicit: boolean;
  /** This server's URL — the OIDC audience, and GOCOV_SERVER. */
  baseUrl: string;
  /** GitLab only: the CI/CD Catalog component is reachable. */
  gitlabCatalog: boolean;
  /** The CLI release the raw-download recipes pin, in the form "v1.2.3". */
  cliVersion: string;
}

export interface Snippet {
  /** The file the snippet belongs in. */
  filename: string;
  code: string;
}

type Line = string | false | null | undefined;

const join = (lines: Line[]) => lines.filter((l): l is string => typeof l === "string").join("\n");

function githubCode(o: SnippetInput, l: LanguageSpec): string {
  if (o.tokenless) {
    return join([
      "permissions:",
      "  contents: read",
      "  id-token: write   # the job's identity token; no secret needed",
      "steps:",
      `  - run: ${l.test}`,
      "  - uses: gocov/gocov-action@v1",
      "    with:",
      `      files: ${l.file}`,
      !o.serverImplicit && "      server: ${{ vars.GOCOV_SERVER }}",
    ]);
  }
  return join([
    `- run: ${l.test}`,
    "- uses: gocov/gocov-action@v1",
    "  with:",
    `    files: ${l.file}`,
    "    token: ${{ secrets.GOCOV_TOKEN }}",
    !o.serverImplicit && "    server: ${{ vars.GOCOV_SERVER }}",
  ]);
}

/** The workflow rules that run a pipeline on MRs and the default branch once. */
const gitlabWorkflow = [
  "workflow:",
  "  rules:",
  '    - if: $CI_PIPELINE_SOURCE == "merge_request_event"',
  "    - if: $CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH",
];

function gitlabCatalogCode(o: SnippetInput, l: LanguageSpec): string {
  return join([
    "include:",
    "  - component: gitlab.com/gocov/gocov/upload@1",
    "    inputs:",
    `      files: ${l.file}`,
    "      needs: [test]",
    !o.serverImplicit && `      server: ${o.baseUrl}`,
    "",
    ...gitlabWorkflow,
    "",
    "test:",
    `  image: ${l.image}`,
    "  script:",
    `    - ${l.test}`,
    "  artifacts:",
    `    paths: [${l.file}]`,
  ]);
}

function gitlabRawCode(o: SnippetInput, l: LanguageSpec): string {
  const release = `https://github.com/gocov/gocov/releases/download/${o.cliVersion}`;
  return join([
    ...gitlabWorkflow,
    "",
    "coverage:",
    `  image: ${l.image}`,
    o.tokenless && "  id_tokens:",
    o.tokenless && "    GOCOV_ID_TOKEN:",
    o.tokenless && `      aud: ${o.baseUrl}`,
    "  script:",
    `    - ${l.test}`,
    `    - curl -fsSLO ${release}/gocov-linux-amd64`,
    `    - curl -fsSL ${release}/checksums.txt`,
    "      | grep ' gocov-linux-amd64$' | sha256sum -c -",
    "    - chmod +x gocov-linux-amd64",
    `    - ./gocov-linux-amd64 upload ${l.file}`,
  ]);
}

function bitbucketCode(o: SnippetInput, l: LanguageSpec): string {
  return join([
    `image: ${l.image}`,
    "",
    "definitions:",
    "  steps:",
    "    - step: &coverage",
    "        name: Test + upload coverage",
    o.tokenless && "        oidc:",
    o.tokenless && "          audiences:",
    o.tokenless && `            - ${o.baseUrl}   # the step's identity token; no secret needed`,
    "        script:",
    `          - ${l.test}`,
    "          - pipe: docker://gocov/upload-pipe:0",
    "            variables:",
    `              FILES: ${l.file}`,
    !o.tokenless && "              TOKEN: $GOCOV_TOKEN",
    !o.serverImplicit && "              SERVER: $GOCOV_SERVER",
    "",
    "pipelines:",
    "  branches:",
    "    main:",
    "      - step: *coverage",
    "  pull-requests:",
    "    '**':",
    "      - step: *coverage",
  ]);
}

/** The file each forge's recipe belongs in. */
export function snippetFilename(forge: Forge): string {
  if (forge === "gitlab") return ".gitlab-ci.yml";
  if (forge === "bitbucket") return "bitbucket-pipelines.yml";
  return ".github/workflows/ci.yml";
}

/** The whole recipe for one forge, language and authentication mode. */
export function buildSnippet(o: SnippetInput): Snippet {
  const l = languageSpec(o.language);
  const filename = snippetFilename(o.forge);
  if (o.forge === "gitlab") return { filename, code: o.gitlabCatalog ? gitlabCatalogCode(o, l) : gitlabRawCode(o, l) };
  if (o.forge === "bitbucket") return { filename, code: bitbucketCode(o, l) };
  return { filename, code: githubCode(o, l) };
}

/**
 * Where GOCOV_TOKEN (and GOCOV_SERVER) go on this forge. One sentence, and
 * the only piece of the old wizard's prose worth keeping whole: GitLab's
 * "masked but not protected" is the difference between a merge-request
 * pipeline that uploads and one that silently cannot.
 */
export function tokenWhere(forge: Forge, serverImplicit: boolean): string {
  if (forge === "gitlab") {
    return (
      "In your GitLab group or project, open Settings, then CI/CD, then Variables. Add GOCOV_TOKEN, marked " +
      "masked but not protected — GitLab checks “Protect variable” by default, and protected variables never " +
      "reach merge request pipelines" +
      (serverImplicit ? "." : ". Add GOCOV_SERVER beside it.")
    );
  }
  if (forge === "bitbucket") {
    return (
      "In Bitbucket, open Workspace settings, then Workspace variables. Add GOCOV_TOKEN, marked secured" +
      (serverImplicit ? "" : ", and GOCOV_SERVER beside it") +
      ", and pass it to the pipe as TOKEN: $GOCOV_TOKEN."
    );
  }
  return (
    "In your GitHub organisation or repository, open Settings, then Secrets and variables, then Actions. Add " +
    "GOCOV_TOKEN as a secret" +
    (serverImplicit ? "" : " and GOCOV_SERVER as a variable") +
    ", and pass the secret to the action as token: ${{ secrets.GOCOV_TOKEN }}."
  );
}

/**
 * The three things most likely to be wrong when no upload arrives — the help
 * the first outside signup never got. Ordered by how often each one is the
 * answer: the credential, then whether the pipeline ran at all, then whether
 * the upload saw the file.
 */
export function checklist(forge: Forge, tokenless: boolean): string[] {
  if (forge === "gitlab") {
    return [
      tokenless
        ? "The upload job declares id_tokens with GOCOV_ID_TOKEN and aud set to this gocov server — the component does that for you; a hand-written job has to."
        : "The variable is named exactly GOCOV_TOKEN and is masked but not protected — a protected variable never reaches a merge request pipeline.",
      "The pipeline ran at all: the workflow rules cover this branch or merge request, and on gitlab.com shared runners need a verified account.",
      "The upload job runs after the tests and can see the coverage file — through needs:, as an artifact from the test job — and the path matches.",
    ];
  }
  if (forge === "bitbucket") {
    return [
      tokenless
        ? "The step lists your gocov server under oidc.audiences — that is what hands the step its identity token."
        : "The variable is named exactly GOCOV_TOKEN, is secured, is set on this workspace or repository, and the pipe is passed TOKEN: $GOCOV_TOKEN.",
      "The pipeline ran at all: Pipelines is enabled for the repository, and the branches or pull-requests section covers this run.",
      "The pipe runs after the tests, and FILES matches the path they wrote.",
    ];
  }
  return [
    tokenless
      ? "The job has permissions: id-token: write — without it there is no identity token, and the upload has nothing to prove the repository with."
      : "The secret is named exactly GOCOV_TOKEN and is visible to this repository — an organisation secret has to list it under its repository access.",
    "The workflow ran at all: it triggers on the branch or pull request you pushed, and the run is not still queued.",
    "The upload step runs after the tests, and files: matches the path they wrote.",
  ];
}

/** The docs recipe for this forge, relative to docs.gocov.dev. */
export function docsRecipe(forge: Forge): string {
  if (forge === "gitlab") return "gitlab-ci/";
  if (forge === "bitbucket") return "bitbucket-pipelines/";
  return "github-actions/";
}
