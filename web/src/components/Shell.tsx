// The frame of the dashboard: the sidebar of the pages on the left, the page on the
// right, the uploads in progress above the page, and the cover while a file is
// dragged over the window.

import { Film, KeyRound, LogOut, Upload, Webhook, X } from "lucide-react";
import { useEffect, useRef, useState, type ReactNode } from "react";
import type { User } from "../api.ts";
import { formatBytes } from "../format.ts";
import { Link, usePathname } from "../router.tsx";
import { useUploads, videoFiles, type UploadItem } from "../uploads.tsx";
import { Button } from "./ui.tsx";

const PAGES = [
  { to: "/", label: "Videos", icon: <Film />, match: (path: string) => path === "/" || path.startsWith("/videos") },
  { to: "/api-keys", label: "API keys", icon: <KeyRound />, match: (path: string) => path === "/api-keys" },
  { to: "/webhooks", label: "Webhooks", icon: <Webhook />, match: (path: string) => path === "/webhooks" || path.startsWith("/webhooks/") },
];

export function Shell({ user, onLogout, children }: { user: User; onLogout: () => void; children: ReactNode }) {
  const pathname = usePathname();
  // After a move to another page, screen readers start reading at the new page.
  const main = useRef<HTMLElement>(null);
  const first = useRef(true);
  useEffect(() => {
    if (first.current) {
      first.current = false;
      return;
    }
    main.current?.focus({ preventScroll: true });
  }, [pathname]);
  return (
    <>
      <a
        href="#main"
        className="sr-only focus:not-sr-only focus:fixed focus:top-2 focus:left-2 focus:z-50 focus:rounded-sm focus:bg-surface focus:px-3 focus:py-2 focus:font-semibold focus:shadow-popover"
      >
        Skip to content
      </a>
      <aside className="fixed inset-y-0 left-0 z-20 hidden w-sidebar border-r border-hairline bg-surface lg:flex lg:flex-col">
        <div className="flex h-16 shrink-0 items-center px-5">
          <Brand />
        </div>
        <nav className="grid gap-0.5 px-4" aria-label="Pages">
          {PAGES.map((page) => (
            <Link
              key={page.to}
              to={page.to}
              current={page.match(pathname)}
              className={`flex h-[30px] items-center gap-3 rounded-sm px-2 text-sm transition-colors duration-150 hover:bg-surface-hover [&_svg]:size-4 ${
                page.match(pathname) ? "font-semibold text-accent" : "text-strong"
              }`}
            >
              {page.icon}
              {page.label}
            </Link>
          ))}
        </nav>
        <div className="mt-auto grid gap-2 border-t border-hairline px-5 py-4">
          <span className="truncate text-xs text-subdued">{user.email}</span>
          <Button variant="ghost" className="-ml-2 w-fit" onClick={onLogout}>
            <LogOut aria-hidden="true" />
            Log out
          </Button>
        </div>
      </aside>
      {/* On narrow screens the pages sit in a bar on top. */}
      <header className="sticky top-0 z-20 flex h-14 items-center gap-1 overflow-x-auto border-b border-hairline bg-surface px-3 lg:hidden">
        <Brand />
        <nav className="ml-2 flex gap-1" aria-label="Pages">
          {PAGES.map((page) => (
            <Link
              key={page.to}
              to={page.to}
              current={page.match(pathname)}
              className={`rounded-sm px-2 py-1 text-sm whitespace-nowrap hover:bg-surface-hover ${page.match(pathname) ? "font-semibold text-accent" : "text-strong"}`}
            >
              {page.label}
            </Link>
          ))}
        </nav>
        <Button variant="ghost" icon className="ml-auto" aria-label="Log out" onClick={onLogout}>
          <LogOut aria-hidden="true" />
        </Button>
      </header>
      <div className="lg:pl-sidebar">
        <main ref={main} id="main" tabIndex={-1} className="mx-auto grid max-w-[1280px] gap-10 px-4 pt-6 pb-16 outline-none sm:px-6 lg:px-10 lg:pt-8">
          {children}
        </main>
      </div>
      <UploadList />
      <DropCover />
    </>
  );
}

function Brand() {
  return (
    <Link to="/" className="flex items-center gap-2 rounded-xs py-1">
      <img src="/logo.svg" alt="" className="size-6" />
      <span className="text-sm font-semibold text-heading">deneb</span>
    </Link>
  );
}

/** The uploads in progress, with their progress and a way to cancel or retry them. */
function UploadList() {
  const { items, cancel, retry } = useUploads();
  if (items.length === 0) return null;
  const settled = items.filter((item) => item.state === "done" || item.state === "failed");
  return (
    <section
      aria-label="Uploads"
      className="fixed right-4 bottom-4 z-30 grid max-h-[50vh] w-[min(26rem,calc(100%-2rem))] gap-2 overflow-y-auto rounded-lg bg-surface p-3 shadow-popover"
    >
      {items.map((item) => (
        <UploadRow key={item.key} item={item} onCancel={() => cancel(item.key)} onRetry={() => retry(item.key)} />
      ))}
      <p className="sr-only" aria-live="polite">
        {settled.map((item) => `${item.name}: ${item.state === "done" ? "uploaded" : "failed"}.`).join(" ")}
      </p>
    </section>
  );
}

const UPLOAD_STATES: Record<UploadItem["state"], string> = {
  queued: "Queued",
  creating: "Starting",
  uploading: "",
  done: "Uploaded",
  failed: "Failed",
};

function UploadRow({ item, onCancel, onRetry }: { item: UploadItem; onCancel: () => void; onRetry: () => void }) {
  const percent = item.size === 0 ? 100 : Math.floor((item.sent / item.size) * 100);
  return (
    <div className="grid gap-1.5">
      <div className="flex min-h-7 items-center gap-2">
        <p className="flex min-w-0 flex-1 items-baseline gap-2 text-sm">
          <span className="min-w-0 truncate font-semibold text-strong">{item.name}</span>
          <span className="numeric shrink-0 text-subdued">
            {item.state === "uploading" ? `${percent}% of ${formatBytes(item.size)}` : UPLOAD_STATES[item.state]}
          </span>
        </p>
        {item.state === "failed" && (
          <Button aria-label={`Retry ${item.name}`} onClick={onRetry}>
            Retry
          </Button>
        )}
        {item.state === "failed" ? (
          <Button variant="ghost" icon aria-label={`Remove ${item.name}`} onClick={onCancel}>
            <X aria-hidden="true" />
          </Button>
        ) : (
          item.state !== "done" && (
            <Button aria-label={`Cancel ${item.name}`} onClick={onCancel}>
              Cancel
            </Button>
          )
        )}
      </div>
      {item.state === "failed" ? (
        <p className="text-xs font-semibold break-words text-critical-ink">{item.error}</p>
      ) : (
        <div
          role="progressbar"
          aria-label={item.name}
          aria-valuemin={0}
          aria-valuemax={100}
          aria-valuenow={percent}
          className="h-1 overflow-hidden rounded-full bg-surface-subdued"
        >
          <div className="h-full bg-accent transition-[width]" style={{ width: `${percent}%` }} />
        </div>
      )}
    </div>
  );
}

/** Covers the window while files are dragged over it, and uploads the videos dropped on it. */
function DropCover() {
  const { start } = useUploads();
  const [dragging, setDragging] = useState(false);
  useEffect(() => {
    let depth = 0;
    const hasFiles = (event: DragEvent) => event.dataTransfer?.types.includes("Files") ?? false;
    const enter = (event: DragEvent) => {
      if (!hasFiles(event)) return;
      depth += 1;
      setDragging(true);
    };
    const leave = (event: DragEvent) => {
      if (!hasFiles(event)) return;
      depth = Math.max(0, depth - 1);
      if (depth === 0) setDragging(false);
    };
    const over = (event: DragEvent) => {
      if (hasFiles(event)) event.preventDefault();
    };
    const drop = (event: DragEvent) => {
      if (!hasFiles(event)) return;
      event.preventDefault();
      depth = 0;
      setDragging(false);
      const files = videoFiles(event.dataTransfer?.files ?? []);
      if (files.length > 0) start(files);
    };
    window.addEventListener("dragenter", enter);
    window.addEventListener("dragleave", leave);
    window.addEventListener("dragover", over);
    window.addEventListener("drop", drop);
    return () => {
      window.removeEventListener("dragenter", enter);
      window.removeEventListener("dragleave", leave);
      window.removeEventListener("dragover", over);
      window.removeEventListener("drop", drop);
    };
  }, [start]);
  if (!dragging) return null;
  return (
    <div className="pointer-events-none fixed inset-3 z-50 grid place-items-center rounded-lg border-2 border-dashed border-accent bg-surface">
      <p className="flex items-center gap-2 text-figure font-bold text-strong">
        <Upload aria-hidden="true" />
        Drop to upload
      </p>
    </div>
  );
}
