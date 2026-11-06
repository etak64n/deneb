// A small router on the History API.

import { useSyncExternalStore, type MouseEvent, type ReactNode } from "react";

export function navigate(to: string): void {
  window.history.pushState(null, "", to);
  window.dispatchEvent(new PopStateEvent("popstate"));
  window.scrollTo(0, 0);
}

function subscribe(onChange: () => void): () => void {
  window.addEventListener("popstate", onChange);
  return () => window.removeEventListener("popstate", onChange);
}

/** The path of the page that is open, such as /videos/vid_abc. */
export function usePathname(): string {
  return useSyncExternalStore(subscribe, () => window.location.pathname);
}

export function Link({ to, className, current, children }: { to: string; className?: string; current?: boolean; children: ReactNode }) {
  function handleClick(event: MouseEvent<HTMLAnchorElement>) {
    // Let the browser open new tabs and windows as usual.
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    navigate(to);
  }
  return (
    <a href={to} onClick={handleClick} className={className} aria-current={current ? "page" : undefined}>
      {children}
    </a>
  );
}
