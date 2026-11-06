import { Component, StrictMode, useCallback, useEffect, useState, type ReactNode } from "react";
import { createRoot } from "react-dom/client";
import { api, messageOf, onSignedOut, type AuthState } from "./api.ts";
import { Shell } from "./components/Shell.tsx";
import { Button, ErrorBox } from "./components/ui.tsx";
import { ApiKeysPage } from "./pages/ApiKeysPage.tsx";
import { AuthPage } from "./pages/AuthPage.tsx";
import { NotFoundPage } from "./pages/NotFoundPage.tsx";
import { VideoPage } from "./pages/VideoPage.tsx";
import { VideosPage } from "./pages/VideosPage.tsx";
import { WebhookPage } from "./pages/WebhookPage.tsx";
import { WebhooksPage } from "./pages/WebhooksPage.tsx";
import { navigate, usePathname } from "./router.tsx";
import "./styles.css";
import { UploadsProvider } from "./uploads.tsx";

function App() {
  const [state, setState] = useState<AuthState | null>(null);
  const [error, setError] = useState("");

  // Resolves to whether someone is signed in.
  const load = useCallback(
    () =>
      api
        .authState()
        .then((next) => {
          setState(next);
          return next.user !== null;
        })
        .catch((reason: unknown) => {
          setError(messageOf(reason));
          return false;
        }),
    [],
  );
  useEffect(() => {
    void load();
    // When the session ends, as after 30 days, the sign-in page comes back.
    onSignedOut(() => void load());
  }, [load]);

  if (error) {
    return (
      <main className="mx-auto max-w-md p-6">
        <ErrorBox>{error}</ErrorBox>
      </main>
    );
  }
  if (state === null) return null;
  if (state.user === null) return <AuthPage setup={state.setup_required} onSignedIn={load} />;

  return (
    <UploadsProvider>
      <Shell
        user={state.user}
        onLogout={() => {
          void api.logout().finally(() => {
            navigate("/");
            void load();
          });
        }}
      >
        <PageBoundary>
          <Routes captions={state.captions} />
        </PageBoundary>
      </Shell>
    </UploadsProvider>
  );
}

function Routes({ captions }: { captions: boolean }) {
  const pathname = usePathname();
  if (pathname === "/") return <VideosPage />;
  const video = /^\/videos\/([^/]+)$/.exec(pathname);
  if (video?.[1]) return <VideoPage key={video[1]} id={decodeURIComponent(video[1])} captions={captions} />;
  if (pathname === "/api-keys") return <ApiKeysPage />;
  if (pathname === "/webhooks") return <WebhooksPage />;
  const webhook = /^\/webhooks\/([^/]+)$/.exec(pathname);
  if (webhook?.[1]) return <WebhookPage key={webhook[1]} id={decodeURIComponent(webhook[1])} />;
  return <NotFoundPage />;
}

/**
 * Keeps the dashboard on screen when a page fails to render, as when deneb was upgraded
 * while the tab was open and a part of the old version is gone.
 */
class PageBoundary extends Component<{ children: ReactNode }, { error: string }> {
  override state = { error: "" };

  static getDerivedStateFromError(error: unknown) {
    return { error: messageOf(error) };
  }

  override render() {
    if (this.state.error === "") return this.props.children;
    return (
      <div className="grid justify-items-start gap-4">
        <ErrorBox>{this.state.error}</ErrorBox>
        <Button onClick={() => window.location.reload()}>Reload</Button>
      </div>
    );
  }
}

const root = document.getElementById("root");
if (root === null) throw new Error("index.html has no #root.");
createRoot(root).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
