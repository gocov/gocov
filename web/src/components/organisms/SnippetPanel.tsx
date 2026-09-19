import { useMemo, useState } from "react";
import { Link } from "react-router";
import { CodeBlock, Mono, Notice } from "@/components/atoms";
import { CopyButton, SecretField, SegmentedControl } from "@/components/molecules";
import { track } from "@/lib/analytics";
import type { SetupInfo } from "@/lib/api/types";
import { readStored, writeStored } from "@/lib/storage";
import { buildSnippet, languageSpec, languages, tokenWhere, type LanguageId } from "@/lib/snippets";
import { routes } from "@/lib/urls";
import "./SnippetPanel.css";

/** The picked language outlives the page; a stale or unreadable value is Go. */
const STORAGE_KEY = "gocov.setup.language";

/** Exported so the card around the panel can name the language in an event. */
export function storedLanguage(): LanguageId {
  const saved = readStored("localStorage", STORAGE_KEY);
  return saved ? languageSpec(saved).id : "go";
}

const rememberLanguage = (id: LanguageId) => writeStored("localStorage", STORAGE_KEY, id);

interface Props {
  info: SetupInfo;
  /** Fetches the upload token. Called at most once; the value stays in state. */
  onReveal: () => Promise<string>;
  /** The snippet reached the clipboard — the point where CI work starts. */
  onCopied?: () => void;
}

/**
 * The one thing setup actually asks of someone: this file, in your repo. The
 * language picker is here because gocov reads six formats and the old wizard
 * only ever showed Go; everything else — where a secret goes, the token
 * itself — folds away behind the single primary action.
 */
export function SnippetPanel({ info, onReveal, onCopied }: Props) {
  const [language, setLanguage] = useState<LanguageId>(storedLanguage);
  const [tokenOpen, setTokenOpen] = useState(false);

  const { forge, forge_label: forgeLabel, prefix } = info.workspace;
  // The alternative path rewrites the snippet while it is open: what is on
  // screen is always what the chosen authentication needs.
  const tokenless = info.tokenless && !tokenOpen;
  const events = { forge, auth: tokenless ? "oidc" : "token", language };

  const { filename, code } = useMemo(
    () =>
      buildSnippet({
        forge,
        language,
        tokenless,
        serverImplicit: info.server_implicit,
        baseUrl: info.base_url,
        gitlabCatalog: info.gitlab_catalog,
        cliVersion: info.cli_version,
      }),
    [forge, language, tokenless, info.server_implicit, info.base_url, info.gitlab_catalog, info.cli_version],
  );

  function pickLanguage(value: string) {
    const id = languageSpec(value).id;
    setLanguage(id);
    rememberLanguage(id);
    track("language_selected", { ...events, language: id });
  }

  // Even a blocked clipboard means the snippet was asked for: setup moves on.
  function copied() {
    track("copy_snippet_clicked", events);
    onCopied?.();
  }

  function toggleToken(e: { preventDefault: () => void }) {
    e.preventDefault();
    setTokenOpen((open) => {
      if (!open) track("show_token_clicked", events);
      return !open;
    });
  }

  const tokenField = (
    <SecretField
      name="GOCOV_TOKEN"
      kind="Secret"
      note={
        info.token_masked === null
          ? "Shown to workspace owners only — ask one for it."
          : "Shown once — rotate it any time in workspace settings"
      }
      masked={info.token_masked ?? ""}
      locked={info.token_masked === null}
      onReveal={
        info.token_masked === null
          ? undefined
          : () => {
              track("reveal_token_clicked", events);
              return onReveal();
            }
      }
      onCopy={() => track("copy_token_clicked", events)}
    />
  );

  return (
    <div className="SnippetPanel stack">
      <SegmentedControl
        label="Language"
        value={language}
        onChange={pickLanguage}
        options={languages.map((l) => ({ value: l.id, label: l.label }))}
      />

      <p className="SnippetPanel__auth">
        {tokenless
          ? `No secret needed — each job proves which repository it is with a short-lived identity token from ${forgeLabel}.`
          : tokenWhere(forge, info.server_implicit)}
      </p>

      {info.connection_broken && (
        <Notice tone="warn">
          The workspace&rsquo;s connection to {forgeLabel} no longer works, so jobs need the token below.{" "}
          <Link to={routes.workspace(forge, prefix)}>Reconnect it</Link> and uploads go back to needing no secret.
        </Notice>
      )}

      <div className="stack stack-1">
        <Mono className="SnippetPanel__file">{filename}</Mono>
        <CodeBlock label={`${filename} snippet`}>{code}</CodeBlock>
        <div className="row">
          <CopyButton variant="primary" label="Copy snippet" value={code} onCopied={copied} />
        </div>
      </div>

      {!info.server_implicit && (
        <SecretField
          name="GOCOV_SERVER"
          kind="Variable"
          note="Your gocov instance — a plain value, not a secret"
          value={info.base_url}
        />
      )}

      {info.tokenless ? (
        <details className="SnippetPanel__alt" open={tokenOpen}>
          <summary onClick={toggleToken}>Use a token instead</summary>
          <div className="SnippetPanel__altBody stack stack-1">
            {/* The line under the picker already switched to "where the
                secret goes"; saying it twice is the old wizard's mistake. */}
            <p className="muted small">
              A pasted token always takes precedence over the identity token, and it is the way in for CI that cannot
              mint one. The snippet above switches to it while this is open.
            </p>
            {tokenField}
          </div>
        </details>
      ) : (
        tokenField
      )}

      <p className="muted small">Commit, branch, repository and pull request are detected automatically.</p>
    </div>
  );
}
