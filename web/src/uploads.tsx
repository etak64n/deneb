// The uploads of the dashboard. They run above the pages, so that they go on while
// the administrator moves around. A few run at once and the rest wait their turn; a
// failed one resumes where it stopped, on Retry or when the connection comes back.

import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { Upload as TusUpload } from "tus-js-client";
import { api, messageOf } from "./api.ts";
import { titleFromFileName } from "./format.ts";

export interface UploadItem {
  key: string;
  name: string;
  size: number;
  sent: number;
  state: "queued" | "creating" | "uploading" | "done" | "failed";
  error?: string;
}

interface Uploads {
  items: UploadItem[];
  /** The videos whose files arrived from this tab, which deneb starts to encode soon after. */
  uploaded: ReadonlySet<string>;
  /** Changes when a video is created or its file arrives, so that lists of videos load again. */
  version: number;
  start: (files: File[]) => void;
  /** Stops an upload, or removes a failed one, and deletes its video. */
  cancel: (key: string) => void;
  retry: (key: string) => void;
}

/** How many files upload at once. More would take every connection of the browser to deneb. */
const PARALLEL = 3;
/** How long a finished upload stays in the list. */
const DONE_MS = 4000;

/** What the provider keeps of an upload besides what it shows. */
interface Entry {
  file: File;
  videoId?: string;
  tus?: TusUpload;
  running: boolean;
  cancelled: boolean;
  done: boolean;
}

const UploadsContext = createContext<Uploads | null>(null);

export function UploadsProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<UploadItem[]>([]);
  const [uploaded, setUploaded] = useState<ReadonlySet<string>>(() => new Set());
  const [version, setVersion] = useState(0);
  const entries = useRef(new Map<string, Entry>());
  const queue = useRef<string[]>([]);
  const active = useRef(0);
  const timers = useRef(new Set<ReturnType<typeof setTimeout>>());
  const pumpRef = useRef<() => void>(() => undefined);

  const update = useCallback((key: string, change: Partial<UploadItem>) => {
    setItems((current) => current.map((item) => (item.key === key ? { ...item, ...change } : item)));
  }, []);

  const remove = useCallback((key: string) => {
    entries.current.delete(key);
    setItems((current) => current.filter((item) => item.key !== key));
  }, []);

  // An upload stopped running: the next one in line takes its place.
  const settle = useCallback((entry: Entry) => {
    if (!entry.running) return;
    entry.running = false;
    active.current -= 1;
    pumpRef.current();
  }, []);

  const fail = useCallback(
    (key: string, entry: Entry, error: unknown) => {
      settle(entry);
      if (!entry.cancelled) update(key, { state: "failed", error: messageOf(error) });
    },
    [settle, update],
  );

  const run = useCallback(
    (key: string, entry: Entry) => {
      entry.running = true;
      active.current += 1;
      if (entry.tus !== undefined) {
        // tus asks deneb how much of the file arrived, and sends the rest.
        update(key, { state: "uploading", error: undefined });
        entry.tus.start();
        return;
      }
      update(key, { state: "creating", error: undefined });
      api
        .createVideo(titleFromFileName(entry.file.name))
        .then(({ video, upload }) => {
          entry.videoId = video.id;
          setVersion((count) => count + 1);
          if (entry.cancelled) {
            void api.deleteVideo(video.id).catch(() => undefined);
            settle(entry);
            return;
          }
          entry.tus = new TusUpload(entry.file, {
            endpoint: upload.tus.endpoint,
            metadata: { ...upload.tus.metadata, filename: entry.file.name, filetype: entry.file.type },
            chunkSize: 64 * 1024 * 1024,
            retryDelays: [0, 1000, 3000, 5000, 10000],
            // An upload resumes within this tab; nothing looks for it after a reload.
            storeFingerprintForResuming: false,
            onProgress: (sent) => update(key, { sent, state: "uploading" }),
            onSuccess: () => {
              entry.done = true;
              settle(entry);
              update(key, { sent: entry.file.size, state: "done" });
              setUploaded((current) => new Set(current).add(video.id));
              setVersion((count) => count + 1);
              const timer = setTimeout(() => {
                timers.current.delete(timer);
                remove(key);
              }, DONE_MS);
              timers.current.add(timer);
            },
            onError: (error) => fail(key, entry, error),
          });
          entry.tus.start();
        })
        .catch((error: unknown) => fail(key, entry, error));
    },
    [update, settle, fail, remove],
  );

  const pump = useCallback(() => {
    while (active.current < PARALLEL) {
      const key = queue.current.shift();
      if (key === undefined) return;
      const entry = entries.current.get(key);
      if (entry !== undefined && !entry.cancelled && !entry.running) run(key, entry);
    }
  }, [run]);
  useEffect(() => {
    pumpRef.current = pump;
  }, [pump]);

  const start = useCallback(
    (files: File[]) => {
      const added = files.map((file): UploadItem => {
        const key = `${file.name}-${file.size}-${Math.random().toString(36).slice(2)}`;
        entries.current.set(key, { file, running: false, cancelled: false, done: false });
        queue.current.push(key);
        return { key, name: file.name, size: file.size, sent: 0, state: "queued" };
      });
      setItems((current) => [...current, ...added]);
      pump();
    },
    [pump],
  );

  const cancel = useCallback(
    (key: string) => {
      const entry = entries.current.get(key);
      if (entry === undefined || entry.done) return;
      entry.cancelled = true;
      queue.current = queue.current.filter((queued) => queued !== key);
      if (entry.tus !== undefined) {
        // Terminating it lets deneb drop what arrived at once.
        entry.tus.abort(true).catch(() => undefined);
        settle(entry);
      }
      // A video still being created is deleted when deneb answers, in run.
      if (entry.videoId !== undefined) {
        void api
          .deleteVideo(entry.videoId)
          .catch(() => undefined)
          .finally(() => setVersion((count) => count + 1));
      }
      remove(key);
    },
    [settle, remove],
  );

  const retry = useCallback(
    (key: string) => {
      const entry = entries.current.get(key);
      if (entry === undefined || entry.running || entry.done || entry.cancelled || queue.current.includes(key)) return;
      update(key, { state: "queued", error: undefined });
      queue.current.push(key);
      pump();
    },
    [update, pump],
  );

  // Failed uploads go on when the browser is back online.
  useEffect(() => {
    const resume = () => {
      for (const [key, entry] of entries.current) if (entry.tus !== undefined && !entry.running) retry(key);
    };
    window.addEventListener("online", resume);
    return () => window.removeEventListener("online", resume);
  }, [retry]);

  // Closing the tab during an upload asks first.
  const busy = items.some((item) => item.state === "queued" || item.state === "creating" || item.state === "uploading");
  useEffect(() => {
    if (!busy) return;
    const warn = (event: BeforeUnloadEvent) => event.preventDefault();
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [busy]);

  // Logging out stops the uploads of the session that ends.
  useEffect(() => {
    const all = entries.current;
    const pending = timers.current;
    return () => {
      for (const timer of pending) clearTimeout(timer);
      for (const entry of all.values()) {
        entry.cancelled = true;
        entry.tus?.abort().catch(() => undefined);
      }
    };
  }, []);

  const value = useMemo(() => ({ items, uploaded, version, start, cancel, retry }), [items, uploaded, version, start, cancel, retry]);
  return <UploadsContext.Provider value={value}>{children}</UploadsContext.Provider>;
}

export function useUploads(): Uploads {
  const uploads = useContext(UploadsContext);
  if (uploads === null) throw new Error("useUploads needs an UploadsProvider.");
  return uploads;
}

/** The files that look like videos, by their type or, when the system gives none, their name. */
export function videoFiles(files: Iterable<File>): File[] {
  return Array.from(files).filter((file) => file.type.startsWith("video/") || VIDEO_EXTENSION.test(file.name));
}

const VIDEO_EXTENSION = /\.(3gp|avi|flv|m2ts|m4v|mkv|mov|mp4|mpe?g|mts|mxf|ogv|ts|webm|wmv)$/i;

/** The value of the accept attribute of file inputs for videos. */
export const VIDEO_ACCEPT = "video/*,.3gp,.avi,.flv,.m2ts,.m4v,.mkv,.mov,.mp4,.mpeg,.mpg,.mts,.mxf,.ogv,.ts,.webm,.wmv";
