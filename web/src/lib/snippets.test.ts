import { buildSnippet, checklist, docsRecipe, languages, languageSpec, snippetFilename, tokenWhere } from "./snippets";
import type { LanguageId, SnippetInput } from "./snippets";

const base: SnippetInput = {
  forge: "github",
  language: "go",
  tokenless: false,
  serverImplicit: true,
  baseUrl: "https://app.gocov.dev",
  gitlabCatalog: false,
  cliVersion: "v0.25.0",
};

const snippet = (o: Partial<SnippetInput>) => buildSnippet({ ...base, ...o });
const code = (o: Partial<SnippetInput>) => snippet(o).code;

// ---- filenames -------------------------------------------------------------

test("each forge's snippet names the file it belongs in", () => {
  expect(snippet({ forge: "github" }).filename).toBe(".github/workflows/ci.yml");
  expect(snippet({ forge: "gitlab" }).filename).toBe(".gitlab-ci.yml");
  expect(snippet({ forge: "bitbucket" }).filename).toBe("bitbucket-pipelines.yml");
  // An unknown forge is treated as GitHub rather than rendering nothing.
  expect(snippetFilename("codeberg")).toBe(".github/workflows/ci.yml");
});

// ---- GitHub Actions --------------------------------------------------------

test("GitHub tokenless asks for the id-token permission and pastes no secret", () => {
  const out = code({ forge: "github", tokenless: true });
  expect(out).toContain("permissions:");
  expect(out).toContain("  id-token: write");
  expect(out).toContain("  - uses: gocov/gocov-action@v1");
  expect(out).toContain("      files: coverage.out");
  expect(out).not.toContain("secrets.GOCOV_TOKEN");
  expect(out).not.toContain("server:");
});

test("GitHub with a token passes the secret and no permissions block", () => {
  const out = code({ forge: "github", tokenless: false });
  expect(out).not.toContain("permissions:");
  expect(out).toContain("    token: ${{ secrets.GOCOV_TOKEN }}");
  expect(out).not.toContain("vars.GOCOV_SERVER");
});

test("a self-hosted GitHub snippet carries the server in both modes", () => {
  expect(code({ forge: "github", tokenless: true, serverImplicit: false })).toContain(
    "      server: ${{ vars.GOCOV_SERVER }}",
  );
  expect(code({ forge: "github", tokenless: false, serverImplicit: false })).toContain(
    "    server: ${{ vars.GOCOV_SERVER }}",
  );
});

// ---- GitLab ----------------------------------------------------------------

test("the GitLab component snippet includes the catalog component and a test job", () => {
  const out = code({ forge: "gitlab", gitlabCatalog: true, tokenless: true });
  expect(out).toContain("  - component: gitlab.com/gocov/gocov/upload@1");
  expect(out).toContain("      needs: [test]");
  expect(out).toContain('    - if: $CI_PIPELINE_SOURCE == "merge_request_event"');
  expect(out).toContain("  image: golang:1.27");
  expect(out).toContain("    paths: [coverage.out]");
  // The component declares its own id_tokens; the snippet does not repeat it.
  expect(out).not.toContain("id_tokens:");
  expect(out).not.toContain("server:");
});

test("the GitLab component snippet names the server when self-hosted", () => {
  const out = code({ forge: "gitlab", gitlabCatalog: true, serverImplicit: false, baseUrl: "https://cov.acme.dev" });
  expect(out).toContain("      server: https://cov.acme.dev");
});

test("the raw GitLab job declares the identity token only when tokenless", () => {
  const withOidc = code({ forge: "gitlab", gitlabCatalog: false, tokenless: true, baseUrl: "https://cov.acme.dev" });
  expect(withOidc).toContain("  id_tokens:");
  expect(withOidc).toContain("    GOCOV_ID_TOKEN:");
  expect(withOidc).toContain("      aud: https://cov.acme.dev");

  const withToken = code({ forge: "gitlab", gitlabCatalog: false, tokenless: false });
  expect(withToken).not.toContain("id_tokens:");
  // The raw job reads GOCOV_TOKEN and GOCOV_SERVER from the job environment,
  // so neither appears as a line — exactly as the old wizard rendered it.
  expect(withToken).not.toContain("GOCOV_TOKEN");
  expect(code({ forge: "gitlab", gitlabCatalog: false, serverImplicit: false })).not.toContain("GOCOV_SERVER");
});

test("the raw GitLab job pins the CLI release it was handed, with a checksum", () => {
  const out = code({ forge: "gitlab", gitlabCatalog: false, cliVersion: "v9.9.9" });
  expect(out).toContain("    - curl -fsSLO https://github.com/gocov/gocov/releases/download/v9.9.9/gocov-linux-amd64");
  expect(out).toContain("    - curl -fsSL https://github.com/gocov/gocov/releases/download/v9.9.9/checksums.txt");
  expect(out).toContain("      | grep ' gocov-linux-amd64$' | sha256sum -c -");
  expect(out).toContain("    - ./gocov-linux-amd64 upload coverage.out");
  // No version is baked in anywhere.
  expect(out).not.toContain("v0.25.0");
});

// ---- Bitbucket -------------------------------------------------------------

test("the Bitbucket step takes its identity token from oidc.audiences", () => {
  const out = code({ forge: "bitbucket", tokenless: true, baseUrl: "https://cov.acme.dev" });
  expect(out).toContain("        oidc:");
  expect(out).toContain("          audiences:");
  expect(out).toContain("            - https://cov.acme.dev   # the step's identity token; no secret needed");
  expect(out).toContain("          - pipe: docker://gocov/upload-pipe:0");
  expect(out).not.toContain("TOKEN: $GOCOV_TOKEN");
  expect(out).toContain("  pull-requests:");
});

test("the Bitbucket pipe takes the token and the server when it needs them", () => {
  const out = code({ forge: "bitbucket", tokenless: false, serverImplicit: false });
  expect(out).not.toContain("oidc:");
  expect(out).toContain("              TOKEN: $GOCOV_TOKEN");
  expect(out).toContain("              SERVER: $GOCOV_SERVER");
});

// ---- every combination -----------------------------------------------------

test("every forge, auth mode, server mode and catalog flag builds a snippet", () => {
  for (const forge of ["github", "gitlab", "bitbucket"]) {
    for (const tokenless of [true, false]) {
      for (const serverImplicit of [true, false]) {
        for (const gitlabCatalog of [true, false]) {
          const out = snippet({ forge, tokenless, serverImplicit, gitlabCatalog });
          expect(out.filename).not.toBe("");
          expect(out.code.trim()).not.toBe("");
          expect(out.code).toContain("coverage.out");
          // A snippet never leaks a token value; it names the variable.
          expect(out.code).not.toContain("gcv_");
          // The server is named exactly when the CLI cannot assume it.
          const mentionsServer = /GOCOV_SERVER|server: https/.test(out.code);
          if (serverImplicit) expect(mentionsServer).toBe(false);
        }
      }
    }
  }
});

// ---- languages -------------------------------------------------------------

const testCommand = (language: LanguageId, o: Partial<SnippetInput> = {}) => code({ ...o, language });

test("the language swaps the test command and the coverage path on GitHub", () => {
  expect(testCommand("js", { forge: "github" })).toContain("- run: npx jest --coverage");
  expect(testCommand("js", { forge: "github" })).toContain("    files: coverage/lcov.info");

  expect(testCommand("java", { forge: "github" })).toContain("- run: mvn verify");
  expect(testCommand("java", { forge: "github" })).toContain("    files: target/site/jacoco/jacoco.xml");

  expect(testCommand("python", { forge: "github" })).toContain("- run: pytest --cov --cov-report=xml");
  expect(testCommand("python", { forge: "github" })).toContain("    files: coverage.xml");
});

test("the language swaps the image too where the snippet names one", () => {
  expect(testCommand("python", { forge: "gitlab", gitlabCatalog: true })).toContain("  image: python:3.13");
  expect(testCommand("ruby", { forge: "gitlab", gitlabCatalog: false })).toContain("  image: ruby:3.4");
  expect(testCommand("php", { forge: "bitbucket" })).toContain("image: php:8.4-cli");
  expect(testCommand("php", { forge: "bitbucket" })).toContain("              FILES: clover.xml");
});

test("no snippet ever passes a format flag — the CLI detects it", () => {
  for (const l of languages) {
    for (const forge of ["github", "gitlab", "bitbucket"]) {
      expect(code({ forge, language: l.id })).not.toContain("-format");
      expect(code({ forge, language: l.id })).toContain(l.file);
    }
  }
});

test("an unknown stored language falls back to Go", () => {
  expect(languageSpec("erlang").id).toBe("go");
  expect(languageSpec("ruby").file).toBe("coverage/.resultset.json");
});

// ---- the words -------------------------------------------------------------

test("tokenWhere says where the secret goes on each forge", () => {
  expect(tokenWhere("github", true)).toContain("Secrets and variables");
  expect(tokenWhere("github", true)).not.toContain("GOCOV_SERVER");
  expect(tokenWhere("github", false)).toContain("GOCOV_SERVER as a variable");

  const gitlab = tokenWhere("gitlab", true);
  expect(gitlab).toContain("masked but not protected");
  expect(gitlab).toContain("never reach merge request pipelines");
  expect(tokenWhere("gitlab", false)).toContain("GOCOV_SERVER");

  expect(tokenWhere("bitbucket", true)).toContain("Workspace variables");
  expect(tokenWhere("bitbucket", true)).toContain("TOKEN: $GOCOV_TOKEN");
});

test("the help checklist is three forge- and mode-specific things to check", () => {
  for (const forge of ["github", "gitlab", "bitbucket"]) {
    for (const tokenless of [true, false]) {
      const items = checklist(forge, tokenless);
      expect(items).toHaveLength(3);
      for (const item of items) expect(item.length).toBeGreaterThan(20);
    }
  }
  expect(checklist("github", true)[0]).toContain("id-token: write");
  expect(checklist("github", false)[0]).toContain("GOCOV_TOKEN");
  expect(checklist("gitlab", true)[0]).toContain("GOCOV_ID_TOKEN");
  expect(checklist("gitlab", false)[0]).toContain("not protected");
  expect(checklist("bitbucket", true)[0]).toContain("oidc.audiences");
  expect(checklist("bitbucket", false)[0]).toContain("secured");
});

test("each forge points at its own docs recipe", () => {
  expect(docsRecipe("github")).toBe("github-actions/");
  expect(docsRecipe("gitlab")).toBe("gitlab-ci/");
  expect(docsRecipe("bitbucket")).toBe("bitbucket-pipelines/");
});
