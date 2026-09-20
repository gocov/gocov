import { ForgeMark } from "gocov-web";

export function TheThreeForges() {
  return (
    <div className="row row-2">
      <span className="row">
        <ForgeMark forge="github" size={20} label="GitHub" /> GitHub
      </span>
      <span className="row">
        <ForgeMark forge="gitlab" size={20} label="GitLab" /> GitLab
      </span>
      <span className="row">
        <ForgeMark forge="bitbucket" size={20} label="Bitbucket" /> Bitbucket
      </span>
    </div>
  );
}

export function Sizes() {
  return (
    <div className="row row-2">
      <ForgeMark forge="github" size={14} label="GitHub, 14px" />
      <ForgeMark forge="github" size={20} label="GitHub, 20px" />
      <ForgeMark forge="github" size={28} label="GitHub, 28px" />
      <ForgeMark forge="github" size={40} label="GitHub, 40px" />
    </div>
  );
}

export function OnSignInButtons() {
  return (
    <div className="stack stack-1">
      <span className="row">
        <ForgeMark forge="github" size={20} /> Continue with GitHub
      </span>
      <span className="row">
        <ForgeMark forge="gitlab" size={20} /> Continue with GitLab
      </span>
      <span className="row">
        <ForgeMark forge="bitbucket" size={20} /> Continue with Bitbucket
      </span>
    </div>
  );
}
