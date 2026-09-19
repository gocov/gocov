import { useEffect, useState } from "react";
import { Button } from "../atoms";
import "./CopyButton.css";

/** The text to copy, or a way to obtain it — a token has to be fetched first. */
export type CopySource = string | (() => string | Promise<string>);

const SHOWN_MS = 1200;

/** execCommand is gone from the modern platform but is all a plain-http page has. */
function copyFallback(text: string): boolean {
  const area = document.createElement("textarea");
  area.value = text;
  area.setAttribute("readonly", "");
  area.style.position = "fixed";
  area.style.opacity = "0";
  document.body.appendChild(area);
  area.select();
  let ok = false;
  try {
    ok = typeof document.execCommand === "function" && document.execCommand("copy");
  } catch {
    ok = false;
  }
  area.remove();
  return ok;
}

async function writeClipboard(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch {
    // Fall through: a denied permission is the same as no clipboard at all.
  }
  return copyFallback(text);
}

/** Copies `value` and says so for a moment. Silent — and harmless — when it cannot. */
export function CopyButton({
  value,
  label = "Copy",
  size = "md",
}: {
  value: CopySource;
  label?: string;
  size?: "md" | "sm";
}) {
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    if (!copied) return;
    const timer = setTimeout(() => setCopied(false), SHOWN_MS);
    return () => clearTimeout(timer);
  }, [copied]);

  async function copy() {
    try {
      const text = typeof value === "function" ? await value() : value;
      if (await writeClipboard(text)) setCopied(true);
    } catch {
      // Fetching the value failed; whoever owns it reports that itself.
    }
  }

  return (
    <Button className="CopyButton" size={size} icon={copied ? "check" : "copy"} onClick={() => void copy()}>
      {copied ? "Copied" : label}
    </Button>
  );
}
