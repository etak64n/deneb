// One video: the player once it is ready, its facts, its renditions, and the URLs
// to play or embed it.

import { lazy, Suspense, useCallback, useEffect, useState, type FormEvent } from "react";
import { api, messageOf, type CaptionsStatus, type Video } from "../api.ts";
import { Button, CodeBox, ConfirmDialog, DetailList, Dialog, ErrorBox, PageHeader, Section, Skeleton, StatusBadge, table } from "../components/ui.tsx";
import { formatBytes, formatDateTime, formatDuration } from "../format.ts";
import { Link, navigate } from "../router.tsx";

const POLL_MS = 3000;

// The player is the largest part of the dashboard; it loads with the first video page.
const Player = lazy(() => import("../components/Player.tsx").then((module) => ({ default: module.Player })));

const CAPTIONS: Record<CaptionsStatus, string> = {
  none: "None",
  queued: "Queued",
  processing: "Transcribing",
  ready: "Ready",
  failed: "Failed",
};

const LANGUAGE_NAMES = new Intl.DisplayNames(["en"], { type: "language" });

function languageName(code: string): string {
  try {
    return LANGUAGE_NAMES.of(code) ?? code;
  } catch {
    return code;
  }
}

function settled(video: Video): boolean {
  const encoded = video.status === "ready" || video.status === "failed";
  return encoded && video.captions.status !== "queued" && video.captions.status !== "processing";
}

export function VideoPage({ id, captions }: { id: string; captions: boolean }) {
  const [video, setVideo] = useState<Video | null>(null);
  const [error, setError] = useState("");
  const [renaming, setRenaming] = useState(false);
  const [deleting, setDeleting] = useState(false);

  const load = useCallback(() => {
    api
      .getVideo(id)
      .then(({ video: fresh }) => {
        setError("");
        // A new signed URL each time would restart the player; keep the one it plays.
        setVideo((old) => (old?.playback && fresh.playback ? { ...fresh, playback: old.playback } : fresh));
      })
      .catch((reason: unknown) => setError(messageOf(reason)));
  }, [id]);
  useEffect(load, [load]);

  const polling = video !== null && !settled(video);
  useEffect(() => {
    if (!polling) return;
    const timer = setInterval(load, POLL_MS);
    return () => clearInterval(timer);
  }, [polling, load]);

  if (video === null) {
    return (
      <div className="grid gap-6">
        <Link to="/" className="w-fit text-sm font-semibold text-accent hover:text-accent-hover">
          Videos
        </Link>
        {error ? <ErrorBox>{error}</ErrorBox> : <Skeleton className="aspect-video w-full max-w-3xl" />}
      </div>
    );
  }

  const playback = video.playback;
  const embedCode = playback
    ? `<iframe src="${playback.embed_url}" style="width:100%;aspect-ratio:16/9;border:0" allow="autoplay; fullscreen; picture-in-picture" allowfullscreen></iframe>`
    : "";

  return (
    <div className="grid gap-8">
      <div className="grid gap-2">
        <Link to="/" className="w-fit text-sm font-semibold text-accent hover:text-accent-hover">
          Videos
        </Link>
        <PageHeader
          title={video.title || "Untitled"}
          actions={
            <>
              <Button onClick={() => setRenaming(true)}>Rename</Button>
              <Button className="text-critical" onClick={() => setDeleting(true)}>
                Delete
              </Button>
            </>
          }
        />
      </div>
      {error && <ErrorBox>{error}</ErrorBox>}

      <div className="grid items-start gap-8 lg:grid-cols-[minmax(0,1fr)_320px]">
        <div className="min-w-0">
          {playback ? (
            <div className="aspect-video overflow-hidden rounded-md bg-black">
              <Suspense>
                <Player
                  sources={{
                    hls: playback.hls_url,
                    poster: playback.poster_url,
                    storyboard: playback.storyboard_url,
                    captions: playback.captions_url,
                    captionsLanguage: video.captions.language,
                  }}
                />
              </Suspense>
            </div>
          ) : (
            <Progress video={video} />
          )}
        </div>
        <DetailList
          items={[
            { label: "Status", value: <StatusBadge status={video.status} progress={video.progress} /> },
            { label: "Video ID", value: <span className="font-mono text-xs text-strong">{video.id}</span> },
            ...(video.duration > 0 ? [{ label: "Duration", value: <span className="numeric">{formatDuration(video.duration)}</span> }] : []),
            ...(video.width > 0 ? [{ label: "Resolution", value: <span className="numeric">{`${video.width} × ${video.height}`}</span> }] : []),
            ...(video.source_size > 0 ? [{ label: "Uploaded file", value: <span className="numeric">{formatBytes(video.source_size)}</span> }] : []),
            ...(captions || video.captions.status !== "none"
              ? [
                  {
                    label: "Captions",
                    value: video.captions.language ? `${CAPTIONS[video.captions.status]} (${languageName(video.captions.language)})` : CAPTIONS[video.captions.status],
                  },
                ]
              : []),
            { label: "Created", value: <span className="numeric">{formatDateTime(video.created_at)}</span> },
            ...(video.ready_at ? [{ label: "Ready", value: <span className="numeric">{formatDateTime(video.ready_at)}</span> }] : []),
          ]}
        />
      </div>

      {playback && (
        <Section title="Playback">
          <div className="grid gap-4">
            <div className="grid gap-1.5">
              <h3 className="text-sm font-bold text-strong">HLS URL</h3>
              <CodeBox text={playback.hls_url} />
            </div>
            <div className="grid gap-1.5">
              <h3 className="text-sm font-bold text-strong">Embed</h3>
              <CodeBox text={embedCode} />
            </div>
            <p className="numeric text-xs text-subdued">Expires {formatDateTime(playback.expires_at)}</p>
          </div>
        </Section>
      )}

      {video.renditions.length > 0 && (
        <Section title="Renditions">
          <div className="overflow-x-auto">
            <table className="w-full max-w-2xl text-sm">
              <thead>
                <tr className={table.headRow}>
                  <th className={table.head}>Name</th>
                  <th className={`${table.head} text-right`}>Width</th>
                  <th className={`${table.head} text-right`}>Height</th>
                  <th className={`${table.head} text-right`}>Bitrate</th>
                </tr>
              </thead>
              <tbody>
                {video.renditions.map((rendition) => (
                  <tr key={rendition.name} className={table.row}>
                    <td className={`${table.cell} font-semibold`}>{rendition.name}</td>
                    <td className={`${table.cell} numeric text-right`}>{rendition.width}</td>
                    <td className={`${table.cell} numeric text-right`}>{rendition.height}</td>
                    <td className={`${table.cell} numeric text-right`}>{`${Math.round(rendition.bitrate / 1000).toLocaleString("en")} kb/s`}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Section>
      )}

      <RenameDialog
        open={renaming}
        video={video}
        onClose={() => setRenaming(false)}
        onRenamed={(renamed) => {
          setVideo((old) => (old ? { ...old, title: renamed.title } : renamed));
          setRenaming(false);
        }}
      />
      <ConfirmDialog
        open={deleting}
        title={`Delete ${video.title ? `“${video.title}”` : "this video"}?`}
        confirmLabel="Delete"
        onClose={() => setDeleting(false)}
        onConfirm={() => api.deleteVideo(video.id).then(() => navigate("/"))}
      />
    </div>
  );
}

/** The place of the player until the video is ready. */
function Progress({ video }: { video: Video }) {
  const percent = Math.floor(video.progress * 100);
  return (
    <div className="grid aspect-video place-items-center rounded-md bg-surface-subdued p-6">
      {video.status === "failed" ? (
        <ErrorBox>{video.error || "The encoding failed."}</ErrorBox>
      ) : (
        <div className="grid w-full max-w-xs gap-2">
          <div className="flex items-baseline justify-between text-sm">
            <span className="font-semibold text-strong">{video.status === "processing" ? "Encoding" : video.status === "queued" ? "Queued" : "Waiting for upload"}</span>
            {video.status === "processing" && <span className="numeric text-label">{percent}%</span>}
          </div>
          <div className="h-1 overflow-hidden rounded-full bg-hairline">
            <div className="h-full bg-accent transition-[width]" style={{ width: `${video.status === "processing" ? percent : 0}%` }} />
          </div>
        </div>
      )}
    </div>
  );
}

function RenameDialog({ open, video, onClose, onRenamed }: { open: boolean; video: Video; onClose: () => void; onRenamed: (video: Video) => void }) {
  const [title, setTitle] = useState(video.title);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    if (open) {
      setTitle(video.title);
      setError("");
    }
  }, [open, video.title]);

  function handleSubmit(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    api
      .renameVideo(video.id, title)
      .then(({ video: renamed }) => onRenamed(renamed))
      .catch((reason: unknown) => setError(messageOf(reason)))
      .finally(() => setBusy(false));
  }

  return (
    <Dialog open={open} onClose={onClose} title="Rename video">
      <form onSubmit={handleSubmit} className="grid gap-4">
        <label className="grid gap-1.5">
          <span className="text-sm font-semibold text-strong">Title</span>
          <input value={title} maxLength={300} autoFocus onChange={(event) => setTitle(event.target.value)} className="field" />
        </label>
        {error && <ErrorBox>{error}</ErrorBox>}
        <div className="flex justify-end gap-2">
          <Button onClick={onClose}>Cancel</Button>
          <Button type="submit" variant="primary" disabled={busy}>
            Save
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
