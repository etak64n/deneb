// The embed page: /embed/<video>?token=<playback token>, or /embed/<video> for permanent
// URLs. Sites put it in an iframe. It plays the stream of the video through the /play
// URLs of the same token, or of "permanent" in its place.

import { StrictMode, useEffect, useState } from "react";
import { createRoot } from "react-dom/client";
import { Player, type PlayerSources } from "./components/Player.tsx";
import "./styles.css";

interface Info {
  title: string;
  captions: boolean;
  captions_language: string;
}

/** The answer of deneb that the video is not available to this URL: 403 or 404. */
class Unavailable extends Error {}

function Embed() {
  const id = decodeURIComponent(window.location.pathname.split("/")[2] ?? "");
  const token = new URLSearchParams(window.location.search).get("token") ?? "permanent";
  const root = `/play/${encodeURIComponent(id)}/${encodeURIComponent(token)}/`;
  const [info, setInfo] = useState<Info | null>(null);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    let stopped = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    // 403 and 404 mean that the video is not available. Any other failure may pass, as
    // while deneb restarts, so the page asks again a few times before it gives up.
    const load = (attempt: number) => {
      fetch(`${root}info.json`)
        .then((response) => {
          if (response.status === 403 || response.status === 404) throw new Unavailable();
          if (!response.ok) throw new Error(response.statusText);
          return response.json() as Promise<Info>;
        })
        .then((value) => {
          if (stopped) return;
          setInfo(value);
          if (value.title) document.title = value.title;
        })
        .catch((reason: unknown) => {
          if (stopped) return;
          if (reason instanceof Unavailable || attempt >= 4) {
            setFailed(true);
            return;
          }
          timer = setTimeout(() => load(attempt + 1), 1000 * 2 ** attempt);
        });
    };
    load(0);
    return () => {
      stopped = true;
      clearTimeout(timer);
    };
  }, [root]);

  if (failed) return <p className="grid h-screen place-items-center text-sm text-white">This video is not available.</p>;
  // The poster shows at once, while the page learns the title and the captions.
  if (info === null) return <img src={`${root}poster.jpg`} alt="" className="h-screen w-screen object-contain" />;
  const sources: PlayerSources = {
    hls: `${root}hls/master.m3u8`,
    poster: `${root}poster.jpg`,
    storyboard: `${root}storyboard.vtt`,
    captions: info.captions ? `${root}captions.vtt` : undefined,
    captionsLanguage: info.captions_language || undefined,
  };
  return <Player sources={sources} />;
}

const element = document.getElementById("root");
if (element === null) throw new Error("embed.html has no #root.");
createRoot(element).render(
  <StrictMode>
    <Embed />
  </StrictMode>,
);
