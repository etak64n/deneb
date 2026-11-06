// The parts of the dashboard: buttons, badges, headings, details, dialogs.

import { Check, Copy } from "lucide-react";
import { useEffect, useId, useRef, useState, type ButtonHTMLAttributes, type ReactNode } from "react";
import type { VideoStatus } from "../api.ts";

type Variant = "primary" | "secondary" | "ghost" | "danger";

const VARIANTS: Record<Variant, string> = {
  primary: "bg-accent text-on-accent hover:bg-accent-hover disabled:opacity-50",
  secondary: "bg-surface text-control shadow-[0_0_0_1px_var(--ds-border)] hover:bg-surface-subdued disabled:text-subdued",
  ghost: "text-control hover:bg-surface-hover disabled:text-subdued",
  danger: "bg-critical text-on-accent hover:opacity-90 disabled:opacity-50",
};

export function Button({
  variant = "secondary",
  icon = false,
  className = "",
  type = "button",
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: Variant; icon?: boolean }) {
  const size = icon ? "size-7" : "h-7 px-2";
  return (
    <button
      type={type}
      className={`inline-flex shrink-0 cursor-pointer items-center justify-center gap-2 rounded-sm text-sm font-semibold whitespace-nowrap transition-colors duration-150 disabled:pointer-events-none pointer-coarse:min-h-11 pointer-coarse:min-w-11 [&_svg]:size-4 [&_svg]:shrink-0 ${size} ${VARIANTS[variant]} ${className}`}
      {...props}
    />
  );
}

/** The heading of a page, with its actions on the right, over a line. */
export function PageHeader({ title, actions }: { title: ReactNode; actions?: ReactNode }) {
  return (
    <header className="flex flex-wrap items-center justify-between gap-3 border-b border-border pb-4">
      <h1 className="min-w-0 text-display font-bold break-words text-heading">{title}</h1>
      {actions !== undefined && <div className="flex shrink-0 items-center gap-2">{actions}</div>}
    </header>
  );
}

/** A part of a page under a 20px heading. */
export function Section({ title, actions, children }: { title: string; actions?: ReactNode; children: ReactNode }) {
  return (
    <section className="grid min-w-0 content-start gap-4">
      <div className="flex min-h-7 flex-wrap items-center justify-between gap-3">
        <h2 className="text-figure font-bold text-strong">{title}</h2>
        {actions}
      </div>
      {children}
    </section>
  );
}

/** Facts of one thing, each as its name and the value under it. */
export function DetailList({ items }: { items: { label: string; value: ReactNode }[] }) {
  return (
    <dl className="grid gap-4">
      {items.map((item) => (
        <div key={item.label} className="grid gap-1">
          <dt className="text-sm font-bold text-strong">{item.label}</dt>
          <dd className="text-sm break-words text-label">{item.value}</dd>
        </div>
      ))}
    </dl>
  );
}

const STATUS: Record<VideoStatus, { label: string; tone: string }> = {
  waiting: { label: "Waiting for upload", tone: "bg-surface-subdued text-label shadow-[0_0_0_1px_var(--ds-border)]" },
  queued: { label: "Queued", tone: "bg-surface-subdued text-label shadow-[0_0_0_1px_var(--ds-border)]" },
  processing: { label: "Processing", tone: "bg-accent-subtle text-accent" },
  ready: { label: "Ready", tone: "bg-positive-subtle text-positive shadow-[0_0_0_1px_var(--ds-positive-border)]" },
  failed: { label: "Failed", tone: "bg-critical/10 text-critical-ink" },
};

/** The state of a video as a badge, with the progress while it encodes. */
export function StatusBadge({ status, progress }: { status: VideoStatus; progress?: number }) {
  const { label, tone } = STATUS[status];
  const percent = status === "processing" && progress !== undefined ? ` ${Math.floor(progress * 100)}%` : "";
  return <span className={`numeric inline-flex h-5 items-center rounded-xs px-1.5 text-xs whitespace-nowrap ${tone}`}>{label + percent}</span>;
}

export function ErrorBox({ children }: { children: ReactNode }) {
  return (
    <p role="alert" className="rounded-sm bg-critical/10 px-3 py-2 text-sm font-semibold break-words text-critical-ink">
      {children}
    </p>
  );
}

export function Skeleton({ className = "" }: { className?: string }) {
  return <div aria-hidden="true" className={`animate-pulse rounded-xs bg-surface-subdued ${className}`} />;
}

/**
 * Puts a text on the clipboard. Pages served over plain HTTP, such as deneb on a local
 * address, have no clipboard API, so a selected text area next to the button copies it there.
 */
async function copyText(text: string, near: HTMLElement): Promise<void> {
  if (window.isSecureContext && navigator.clipboard !== undefined) {
    await navigator.clipboard.writeText(text);
    return;
  }
  const area = document.createElement("textarea");
  area.value = text;
  area.readOnly = true;
  area.className = "fixed top-0 left-0 opacity-0";
  // Inside an open dialog, the rest of the page is inert and cannot be selected.
  near.after(area);
  area.select();
  const copied = document.execCommand("copy");
  area.remove();
  if (!copied) throw new Error("The browser did not copy the text.");
}

/** A button that copies a text and says so for a moment. */
export function CopyButton({ text, label = "Copy" }: { text: string; label?: string }) {
  const [result, setResult] = useState<"copied" | "failed" | null>(null);
  useEffect(() => {
    if (result === null) return;
    const timer = setTimeout(() => setResult(null), 1500);
    return () => clearTimeout(timer);
  }, [result]);
  return (
    <Button
      onClick={(event) => {
        copyText(text, event.currentTarget)
          .then(() => setResult("copied"))
          .catch(() => setResult("failed"));
      }}
    >
      {result === "copied" ? <Check aria-hidden="true" /> : <Copy aria-hidden="true" />}
      {result === "copied" ? "Copied" : result === "failed" ? "Copy failed" : label}
    </Button>
  );
}

/** A text that the reader copies, such as a key or a snippet, in a monospace box. */
export function CodeBox({ text }: { text: string }) {
  return (
    <div className="flex items-start gap-2">
      <code className="min-w-0 flex-1 rounded-sm bg-surface-subdued px-3 py-2 font-mono text-xs break-all text-strong">{text}</code>
      <CopyButton text={text} />
    </div>
  );
}

/**
 * A window over the page, on the native dialog element, which keeps the focus inside. It
 * closes on Escape and on a click on the backdrop, unless it is not dismissible, as when
 * it shows a secret that would be lost.
 */
export function Dialog({
  open,
  onClose,
  title,
  dismissible = true,
  children,
}: {
  open: boolean;
  onClose: () => void;
  title: string;
  dismissible?: boolean;
  children: ReactNode;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  const pressed = useRef<EventTarget | null>(null);
  const titleId = useId();
  useEffect(() => {
    const dialog = ref.current;
    if (dialog === null) return;
    if (open && !dialog.open) dialog.showModal();
    if (!open && dialog.open) dialog.close();
  }, [open]);
  return (
    <dialog
      ref={ref}
      aria-labelledby={titleId}
      onCancel={(event) => {
        if (!dismissible) event.preventDefault();
      }}
      onClose={onClose}
      onPointerDown={(event) => {
        pressed.current = event.target;
      }}
      onClick={(event) => {
        // The backdrop is the dialog element itself. A selection that started inside and
        // ended on the backdrop is not a click on it.
        if (dismissible && event.target === ref.current && pressed.current === ref.current) onClose();
      }}
      className="m-auto w-[calc(100%-2rem)] max-w-md rounded-xl bg-surface p-0 text-ink shadow-popover"
    >
      {open && (
        <div className="grid gap-4 p-5">
          <h2 id={titleId} className="text-figure font-bold [overflow-wrap:anywhere] text-strong">
            {title}
          </h2>
          {children}
        </div>
      )}
    </dialog>
  );
}

/** Asks before an action that cannot be undone. */
export function ConfirmDialog({
  open,
  title,
  children,
  confirmLabel,
  onConfirm,
  onClose,
}: {
  open: boolean;
  title: string;
  children?: ReactNode;
  confirmLabel: string;
  onConfirm: () => Promise<void>;
  onClose: () => void;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  return (
    <Dialog
      open={open}
      title={title}
      onClose={() => {
        setError("");
        onClose();
      }}
    >
      {children}
      {error && <ErrorBox>{error}</ErrorBox>}
      <div className="flex justify-end gap-2">
        <Button onClick={onClose}>Cancel</Button>
        <Button
          variant="danger"
          disabled={busy}
          onClick={() => {
            setBusy(true);
            setError("");
            onConfirm()
              .catch((reason: unknown) => setError(reason instanceof Error ? reason.message : String(reason)))
              .finally(() => setBusy(false));
          }}
        >
          {confirmLabel}
        </Button>
      </div>
    </Dialog>
  );
}

const TABLE_HEAD = "h-9 px-2 text-left align-middle text-xs font-semibold whitespace-nowrap text-label";
const TABLE_CELL = "px-2 py-2 align-middle text-strong";

/** The classes of the cells of tables, as one place to change them. */
export const table = { head: TABLE_HEAD, cell: TABLE_CELL, row: "border-b border-hairline", headRow: "border-b border-border" };
