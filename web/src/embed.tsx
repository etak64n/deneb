// The embed page: /embed/<video>?token=<playback token>, or /embed/<video> for a public
// video. Sites put it in an iframe. It plays the stream of the video through the /play
// URLs of the same token, or of "public" in its place.

import { StrictMode, useEffect, useState } from "react";
import { createRoot } from "react-dom/client";
import { Player, type PlayerSources } from "./components/Player.tsx";
import "./styles.css";

interface Info {
  title: string;
  captions: boolean;
  captions_language: string;
}

function Embed() {
  const id = decodeURIComponent(window.location.pathname.split("/")[2] ?? "");
  const token = new URLSearchParams(window.location.search).get("token") ?? "public";
  const root = `/play/${encodeURIComponent(id)}/${encodeURIComponent(token)}/`;
  const [info, setInfo] = useState<Info | null>(null);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    fetch(`${root}info.json`)
      .then((response) => {
        if (!response.ok) throw new Error(response.statusText);
        return response.json() as Promise<Info>;
      })
      .then((value) => {
        setInfo(value);
        if (value.title) document.title = value.title;
      })
      .catch(() => setFailed(true));
  }, [root]);

  if (failed) return <p className="grid h-screen place-items-center text-sm text-white">This video is not available.</p>;
  if (info === null) return null;
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
