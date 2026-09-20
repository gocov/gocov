import { InlineCode, TokenCard } from "gocov-web";

// Obvious placeholders, in the shape the repo's own fixtures already use.
const reveal = async () => "gocov_live_9f2c41d8a7b3";
const rotate = async () => "gocov_live_0000cafebabe";

export function WorkspaceToken() {
  return (
    <TokenCard
      title="Uploads"
      intro={
        <>
          Every repository under <InlineCode>acme/</InlineCode> uploads with this token. Rotating it takes effect
          immediately &mdash; CI has to be updated in the same change.
        </>
      }
      serverUrl={null}
      tokenMasked="gocov_live_••••••••3f2a"
      owner
      onReveal={reveal}
      onRotate={rotate}
    />
  );
}

export function SelfHostedInstance() {
  return (
    <TokenCard
      title="Uploads"
      intro={
        <>
          Builds for <InlineCode>acme/api</InlineCode> can upload with this repository token. Rotating it takes effect
          immediately &mdash; CI has to be updated in the same change.
        </>
      }
      serverUrl="https://cov.acme.dev"
      tokenMasked="gocov_live_••••••••7b19"
      owner
      onReveal={reveal}
      onRotate={rotate}
    />
  );
}

export function MemberLocked() {
  return (
    <TokenCard
      title="Uploads"
      intro={
        <>
          Every repository under <InlineCode>acme/</InlineCode> uploads with this token.
        </>
      }
      serverUrl={null}
      tokenMasked={null}
      owner={false}
      onReveal={reveal}
      onRotate={rotate}
    />
  );
}
