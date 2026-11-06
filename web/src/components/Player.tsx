// The player of deneb: Video.js 10 with its neutral skin, playing the HLS stream,
// with the storyboard for previews on the seek bar and the captions when they exist.
// hls.js plays the stream: the lighter engine of Video.js cannot play MPEG-TS segments,
// which deneb writes because every HLS player can play them.

import "@videojs/react/video/neutral-skin.css";
import { HlsJsVideo } from "@videojs/react/media/hlsjs-video";
import { NeutralVideoSkin, VideoPlayer } from "@videojs/react/video";

export interface PlayerSources {
  hls: string;
  poster: string;
  storyboard: string;
  captions?: string;
  captionsLanguage?: string;
}

const LANGUAGE_NAMES = new Intl.DisplayNames(["en"], { type: "language" });

function languageName(code: string | undefined): string {
  if (!code) return "Captions";
  try {
    return LANGUAGE_NAMES.of(code) ?? code;
  } catch {
    return code;
  }
}

export function Player({ sources, autoPlay = false }: { sources: PlayerSources; autoPlay?: boolean }) {
  return (
    <VideoPlayer poster={sources.poster}>
      <NeutralVideoSkin className="deneb-player">
        <HlsJsVideo src={sources.hls} playsInline crossOrigin="anonymous" autoPlay={autoPlay} preload="metadata">
          <track kind="metadata" label="thumbnails" src={sources.storyboard} default />
          {sources.captions !== undefined && (
            <track kind="subtitles" src={sources.captions} srcLang={sources.captionsLanguage || undefined} label={languageName(sources.captionsLanguage)} />
          )}
        </HlsJsVideo>
      </NeutralVideoSkin>
    </VideoPlayer>
  );
}
