import { StrictMode, useCallback, useEffect, useState } from "react";
import { createRoot } from "react-dom/client";
import { api, messageOf, type AuthState } from "./api.ts";
import { Shell } from "./components/Shell.tsx";
import { ErrorBox } from "./components/ui.tsx";
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

  const load = useCallback(() => {
    api
      .authState()
      .then(setState)
      .catch((reason: unknown) => setError(messageOf(reason)));
  }, []);
  useEffect(load, [load]);

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
            load();
          });
        }}
      >
        <Routes captions={state.captions} />
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

const root = document.getElementById("root");
if (root === null) throw new Error("index.html has no #root.");
createRoot(root).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
