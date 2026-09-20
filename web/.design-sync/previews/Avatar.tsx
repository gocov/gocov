import { Avatar, Mono } from "gocov-web";

export function Kinds() {
  return (
    <div className="row row-2">
      <span className="row">
        <Avatar kind="forge" forge="github" /> <Mono>acme/api</Mono>
      </span>
      <span className="row">
        <Avatar name="acme" /> acme
      </span>
      <span className="row">
        <Avatar kind="person" /> @omer
      </span>
      <span className="row">
        <Avatar kind="bot" /> <Mono>gocov[bot]</Mono>
      </span>
    </div>
  );
}

export function Forges() {
  return (
    <div className="row row-2">
      <span className="row">
        <Avatar kind="forge" forge="github" label="GitHub" /> GitHub
      </span>
      <span className="row">
        <Avatar kind="forge" forge="gitlab" label="GitLab" /> GitLab
      </span>
      <span className="row">
        <Avatar kind="forge" forge="bitbucket" label="Bitbucket" /> Bitbucket
      </span>
    </div>
  );
}

export function Sizes() {
  return (
    <div className="row row-2">
      <span className="row">
        <Avatar kind="forge" forge="gitlab" size={20} /> <span className="mono muted">20</span>
      </span>
      <span className="row">
        <Avatar kind="forge" forge="gitlab" /> <span className="mono muted">26 · the default</span>
      </span>
      <span className="row">
        <Avatar kind="forge" forge="gitlab" size={40} /> <span className="mono muted">40</span>
      </span>
      <span className="row">
        <Avatar name="acme-labs" size={40} /> <span className="mono muted">40 · initial</span>
      </span>
    </div>
  );
}

export function Initials() {
  return (
    <div className="row row-2">
      <span className="row">
        <Avatar name="acme" /> acme
      </span>
      <span className="row">
        <Avatar name="acme-labs" /> acme-labs
      </span>
      <span className="row">
        <Avatar name="Örnek" /> Örnek
      </span>
      <span className="row">
        <Avatar name="" /> unnamed
      </span>
    </div>
  );
}
