import { ForgeMark } from "./ForgeMark";
import { Icon } from "./Icon";
import "./Avatar.css";

export type AvatarKind = "forge" | "initial" | "person" | "bot";

interface Props {
  kind?: AvatarKind;
  /** For kind="forge": the forge name. */
  forge?: string;
  /** For kind="initial": the first letter is taken from this. */
  name?: string;
  size?: number;
  /** Set only when the avatar is the sole carrier of the identity. */
  label?: string;
}

/** A square identity tile: a forge mark, an initial, a person or a bot. */
export function Avatar({ kind = "initial", forge = "", name = "", size = 26, label }: Props) {
  const initial = [...name][0]?.toUpperCase() ?? "?";
  const inner =
    kind === "forge" ? (
      <ForgeMark forge={forge} size={Math.round(size * 0.62)} />
    ) : kind === "person" ? (
      <Icon name="person" size={Math.round(size * 0.62)} />
    ) : kind === "bot" ? (
      <Icon name="bot" size={Math.round(size * 0.62)} />
    ) : (
      <span className="Avatar__initial">{initial}</span>
    );
  return (
    <span
      className={`Avatar Avatar--${kind}`}
      style={{ width: size, height: size }}
      role={label ? "img" : undefined}
      aria-label={label}
      aria-hidden={label ? undefined : true}
    >
      {inner}
    </span>
  );
}
