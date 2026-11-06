// The library: every video, newest first, with its state while it encodes.

import { Film, Upload } from "lucide-react";
import { useCallback, useEffect, useRef, useState, type MouseEvent } from "react";
import { api, messageOf, type Video } from "../api.ts";
import { Button, ErrorBox, PageHeader, Skeleton, StatusBadge, table } from "../components/ui.tsx";
import { formatDate, formatDuration } from "../format.ts";
import { Link, navigate, useTitle } from "../router.tsx";
import { useUploads, VIDEO_ACCEPT } from "../uploads.tsx";

/** How often the list looks again while a video encodes. */
const POLL_MS = 3000;
/** The videos of a page of the list. */
const PAGE = 50;

function inProgress(video: Video): boolean {
  return video.status === "queued" || video.status === "processing" || video.captions.status === "queued" || video.captions.status === "processing";
}

/** Whether a comes after b in the order of the list: newest first, then by ID. */
function isOlder(a: Video, b: Video): boolean {
  const at = Date.parse(a.created_at);
  const bt = Date.parse(b.created_at);
  return at < bt || (at === bt && a.id < b.id);
}

export function VideosPage() {
  useTitle("Videos");
  const [list, setList] = useState<{ videos: Video[]; next: string } | null>(null);
  const [error, setError] = useState("");
  const [loadingMore, setLoadingMore] = useState(false);
  const { start, uploaded, version } = useUploads();
  const fileInput = useRef<HTMLInputElement>(null);
  const loaded = useRef(0);
  useEffect(() => {
    loaded.current = list?.videos.length ?? 0;
  }, [list]);

  // Loads the videos shown again, up to the largest page, and keeps any after them.
  const refresh = useCallback(() => {
    api
      .listVideos(Math.min(100, Math.max(PAGE, loaded.current)))
      .then(({ videos: fresh, next_cursor }) => {
        setError("");
        setList((old) => {
          const last = fresh.at(-1);
          if (old === null || last === undefined || next_cursor === "") return { videos: fresh, next: next_cursor };
          const seen = new Set(fresh.map((video) => video.id));
          const rest = old.videos.filter((video) => !seen.has(video.id) && isOlder(video, last));
          return rest.length === 0 ? { videos: fresh, next: next_cursor } : { videos: [...fresh, ...rest], next: old.next };
        });
      })
      .catch((reason: unknown) => setError(messageOf(reason)));
  }, []);

  useEffect(refresh, [refresh, version]);

  const videos = list?.videos ?? null;
  const next = list?.next ?? "";
  // A video whose file just arrived waits a moment before deneb queues it.
  const busy = videos?.some((video) => inProgress(video) || (video.status === "waiting" && uploaded.has(video.id))) ?? false;
  useEffect(() => {
    if (!busy) return;
    const timer = setInterval(refresh, POLL_MS);
    return () => clearInterval(timer);
  }, [busy, refresh]);

  function loadMore() {
    setLoadingMore(true);
    api
      .listVideos(PAGE, next)
      .then(({ videos: more, next_cursor }) => {
        setList((old) => {
          const seen = new Set(old?.videos.map((video) => video.id));
          return { videos: [...(old?.videos ?? []), ...more.filter((video) => !seen.has(video.id))], next: next_cursor };
        });
      })
      .catch((reason: unknown) => setError(messageOf(reason)))
      .finally(() => setLoadingMore(false));
  }

  function chooseFiles() {
    fileInput.current?.click();
  }

  return (
    <div className="grid gap-6">
      <PageHeader
        title="Videos"
        actions={
          <Button variant="primary" onClick={chooseFiles}>
            <Upload aria-hidden="true" />
            Upload
          </Button>
        }
      />
      <input
        ref={fileInput}
        type="file"
        accept={VIDEO_ACCEPT}
        multiple
        hidden
        onChange={(event) => {
          const files = Array.from(event.target.files ?? []);
          if (files.length > 0) start(files);
          event.target.value = "";
        }}
      />
      {error && <ErrorBox>{error}</ErrorBox>}
      {videos === null ? (
        !error && (
          <div className="grid gap-3" aria-busy="true">
            {Array.from({ length: 5 }, (_, index) => (
              <Skeleton key={index} className="h-14" />
            ))}
          </div>
        )
      ) : videos.length === 0 ? (
        <button
          type="button"
          onClick={chooseFiles}
          className="grid h-56 cursor-pointer place-items-center rounded-lg border border-dashed border-border text-control transition-colors duration-150 hover:bg-surface-subdued"
        >
          <span className="flex items-center gap-2 text-sm font-semibold [&_svg]:size-4">
            <Upload aria-hidden="true" />
            Upload videos
          </span>
        </button>
      ) : (
        <>
          <VideoTable videos={videos} />
          {next !== "" && (
            <Button className="justify-self-center" disabled={loadingMore} onClick={loadMore}>
              Load more
            </Button>
          )}
        </>
      )}
    </div>
  );
}

function VideoTable({ videos }: { videos: Video[] }) {
  function openRow(event: MouseEvent<HTMLTableRowElement>, id: string) {
    // The link in the row opens the video itself.
    if ((event.target as HTMLElement).closest("a, button")) return;
    navigate(`/videos/${encodeURIComponent(id)}`);
  }
  return (
    <div className="overflow-x-auto">
      <table className="w-full table-fixed text-sm">
        <thead>
          <tr className={table.headRow}>
            <th className={`${table.head} hidden w-28 sm:table-cell`}>
              <span className="sr-only">Poster</span>
            </th>
            <th className={table.head}>Title</th>
            <th className={`${table.head} w-32 sm:w-40`}>Status</th>
            <th className={`${table.head} hidden w-24 text-right sm:table-cell`}>Duration</th>
            <th className={`${table.head} hidden w-28 md:table-cell`}>Created</th>
          </tr>
        </thead>
        <tbody>
          {videos.map((video) => (
            <tr key={video.id} onClick={(event) => openRow(event, video.id)} className={`${table.row} cursor-pointer transition-colors duration-150 hover:bg-surface-subdued`}>
              <td className="hidden py-2 pr-2 pl-2 sm:table-cell">
                <Poster video={video} />
              </td>
              <td className={`${table.cell} truncate`}>
                <Link to={`/videos/${encodeURIComponent(video.id)}`} className="font-semibold text-strong hover:text-accent">
                  {video.title || <span className="text-subdued">Untitled</span>}
                </Link>
              </td>
              <td className={table.cell}>
                <StatusBadge status={video.status} progress={video.progress} />
              </td>
              <td className={`${table.cell} numeric hidden text-right sm:table-cell`}>{video.duration > 0 ? formatDuration(video.duration) : ""}</td>
              <td className={`${table.cell} numeric hidden text-label md:table-cell`}>{formatDate(video.created_at)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function Poster({ video }: { video: Video }) {
  const poster = video.playback?.poster_url;
  return (
    <div className="grid aspect-video w-24 place-items-center overflow-hidden rounded-xs bg-surface-subdued text-placeholder [&_svg]:size-4">
      {poster ? <img src={poster} alt="" loading="lazy" className="size-full object-cover" /> : <Film aria-hidden="true" />}
    </div>
  );
}
