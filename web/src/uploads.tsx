// The uploads of the dashboard. They run above the pages, so that they go on while
// the administrator moves around, and resume after a lost connection.

import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { Upload as TusUpload } from "tus-js-client";
import { api, messageOf } from "./api.ts";
import { titleFromFileName } from "./format.ts";

export interface UploadItem {
  key: string;
  name: string;
  size: number;
  /** The video that the upload fills, once deneb has created it. */
  videoId?: string;
  sent: number;
  state: "creating" | "uploading" | "done" | "failed";
  error?: string;
}

interface Uploads {
  items: UploadItem[];
  /** Counts the uploads that finished, so that lists of videos know to load again. */
  finished: number;
  start: (files: File[]) => void;
  cancel: (key: string) => void;
  dismiss: (key: string) => void;
}

const UploadsContext = createContext<Uploads | null>(null);

export function UploadsProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<UploadItem[]>([]);
  const [finished, setFinished] = useState(0);
  const running = useRef(new Map<string, TusUpload>());

  const update = useCallback((key: string, change: Partial<UploadItem>) => {
    setItems((current) => current.map((item) => (item.key === key ? { ...item, ...change } : item)));
  }, []);

  const start = useCallback(
    (files: File[]) => {
      for (const file of files) {
        const key = `${file.name}-${file.size}-${Math.random().toString(36).slice(2)}`;
        setItems((current) => [...current, { key, name: file.name, size: file.size, sent: 0, state: "creating" }]);
        api
          .createVideo(titleFromFileName(file.name))
          .then(({ video, upload }) => {
            update(key, { videoId: video.id });
            const tus = new TusUpload(file, {
              endpoint: upload.url,
              metadata: { ...upload.metadata, filename: file.name, filetype: file.type },
              chunkSize: 64 * 1024 * 1024,
              retryDelays: [0, 1000, 3000, 5000, 10000],
              onProgress: (sent) => update(key, { sent, state: "uploading" }),
              onSuccess: () => {
                running.current.delete(key);
                update(key, { sent: file.size, state: "done" });
                setFinished((count) => count + 1);
              },
              onError: (error) => {
                running.current.delete(key);
                update(key, { state: "failed", error: messageOf(error) });
              },
            });
            running.current.set(key, tus);
            tus.start();
          })
          .catch((error: unknown) => update(key, { state: "failed", error: messageOf(error) }));
      }
    },
    [update],
  );

  // Cancelling also removes the video, which would otherwise wait for its file.
  const cancel = useCallback(
    (key: string) => {
      void running.current.get(key)?.abort(true);
      running.current.delete(key);
      const videoId = items.find((item) => item.key === key)?.videoId;
      if (videoId !== undefined) void api.deleteVideo(videoId).catch(() => undefined);
      setItems((current) => current.filter((item) => item.key !== key));
    },
    [items],
  );

  const dismiss = useCallback((key: string) => setItems((current) => current.filter((item) => item.key !== key)), []);

  // Finished uploads leave the list after a moment; failed ones stay until dismissed.
  useEffect(() => {
    if (!items.some((item) => item.state === "done")) return;
    const timer = setTimeout(() => setItems((current) => current.filter((item) => item.state !== "done")), 4000);
    return () => clearTimeout(timer);
  }, [items]);

  // Closing the tab during an upload asks first.
  useEffect(() => {
    const active = items.some((item) => item.state === "creating" || item.state === "uploading");
    if (!active) return;
    const warn = (event: BeforeUnloadEvent) => event.preventDefault();
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [items]);

  const value = useMemo(() => ({ items, finished, start, cancel, dismiss }), [items, finished, start, cancel, dismiss]);
  return <UploadsContext.Provider value={value}>{children}</UploadsContext.Provider>;
}

export function useUploads(): Uploads {
  const uploads = useContext(UploadsContext);
  if (uploads === null) throw new Error("useUploads needs an UploadsProvider.");
  return uploads;
}
