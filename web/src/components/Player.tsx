// The player of deneb: Video.js 10 with its neutral skin, playing the HLS stream,
// with the storyboard for previews on the seek bar and the captions when they exist.

import "@videojs/react/video/neutral-skin.css";
import { HlsVideo } from "@videojs/react/media/hls-video";
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
        <HlsVideo src={sources.hls} playsInline crossOrigin="anonymous" autoPlay={autoPlay} preload="metadata">
          <track kind="metadata" label="thumbnails" src={sources.storyboard} default />
          {sources.captions !== undefined && (
            <track kind="subtitles" src={sources.captions} srcLang={sources.captionsLanguage || undefined} label={languageName(sources.captionsLanguage)} />
          )}
        </HlsVideo>
      </NeutralVideoSkin>
    </VideoPlayer>
  );
}
