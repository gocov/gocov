import { useEffect, useRef, type ReactNode } from "react";
import { Button } from "../atoms";
import "./ConfirmDialog.css";

interface Props {
  open: boolean;
  title: ReactNode;
  children: ReactNode;
  confirmLabel?: string;
  cancelLabel?: string;
  /** The move cannot be undone: the confirm button says so. */
  danger?: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}

/**
 * The one place the app asks "are you sure". It is a real <dialog>: Escape
 * closes it, the page behind it is inert, and focus goes back where it was.
 */
export function ConfirmDialog({
  open,
  title,
  children,
  confirmLabel = "Confirm",
  cancelLabel = "Cancel",
  danger,
  onConfirm,
  onCancel,
}: Props) {
  const ref = useRef<HTMLDialogElement>(null);
  const opener = useRef<Element | null>(null);

  useEffect(() => {
    const dialog = ref.current;
    if (!dialog) return;
    if (open) {
      opener.current = document.activeElement;
      // jsdom (and very old browsers) have the element but not the method.
      if (typeof dialog.showModal === "function") dialog.showModal();
      else dialog.setAttribute("open", "");
      return;
    }
    if (dialog.open) {
      if (typeof dialog.close === "function") dialog.close();
      else dialog.removeAttribute("open");
    }
    if (opener.current instanceof HTMLElement) opener.current.focus();
    opener.current = null;
  }, [open]);

  return (
    <dialog
      className={`ConfirmDialog${danger ? " ConfirmDialog--danger" : ""}`}
      ref={ref}
      onCancel={(e) => {
        // Escape: let the owner of `open` do the closing.
        e.preventDefault();
        onCancel();
      }}
    >
      <h2 className="ConfirmDialog__title">{title}</h2>
      <div className="ConfirmDialog__body">{children}</div>
      <div className="ConfirmDialog__actions">
        <Button onClick={onCancel}>{cancelLabel}</Button>
        <Button variant={danger ? "danger" : "primary"} onClick={onConfirm}>
          {confirmLabel}
        </Button>
      </div>
    </dialog>
  );
}
