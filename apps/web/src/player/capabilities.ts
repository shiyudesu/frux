import type {
  PlaybackClientCapabilities,
  PlaybackSource,
  PlaybackSourceCapability,
  PlayerMediaElement
} from "./types";

interface NetworkInformationLike {
  effectiveType?: string;
  downlink?: number;
  rtt?: number;
  saveData?: boolean;
}

interface NavigatorCapabilitiesLike {
  onLine?: boolean;
  connection?: NetworkInformationLike;
  mozConnection?: NetworkInformationLike;
  webkitConnection?: NetworkInformationLike;
  mediaCapabilities?: MediaCapabilitiesLike;
}

interface MediaCapabilitiesLike {
  decodingInfo(configuration: MediaDecodingConfiguration): Promise<MediaCapabilitiesDecodingInfo>;
}

interface MediaSourceConstructorLike {
  isTypeSupported(contentType: string): boolean;
}

interface CodecProbe {
  canPlayType(contentType: string): string;
}

export interface CapabilityDetectionOptions {
  navigator?: NavigatorCapabilitiesLike;
  mediaSource?: MediaSourceConstructorLike;
  mediaElement?: Pick<PlayerMediaElement, never> & CodecProbe;
  viewport?: {
    width: number;
    height: number;
    devicePixelRatio: number;
  };
}

export async function detectPlaybackCapabilities(
  sources: readonly PlaybackSource[],
  options: CapabilityDetectionOptions = {}
): Promise<PlaybackClientCapabilities> {
  const nav = options.navigator ?? browserNavigator();
  const connection = nav?.connection ?? nav?.mozConnection ?? nav?.webkitConnection;
  const mediaSource = options.mediaSource ?? browserMediaSource();
  const mediaElement = options.mediaElement ?? browserCodecProbe();
  const viewport = options.viewport ?? browserViewport() ?? { width: 1, height: 1, devicePixelRatio: 1 };
  const sourceCapabilities = await Promise.all(
    sources.map((source) => detectSourceCapability(source, mediaElement, mediaSource, nav?.mediaCapabilities))
  );

  return {
    online: nav?.onLine !== false,
    mediaSource: Boolean(mediaSource),
    mediaCapabilities: Boolean(nav?.mediaCapabilities),
    saveData: Boolean(connection?.saveData),
    effectiveType: normalizeSignal(connection?.effectiveType),
    downlinkMbps: positive(connection?.downlink),
    rttMs: positive(connection?.rtt),
    viewportWidth: positive(viewport.width) ?? 1,
    viewportHeight: positive(viewport.height) ?? 1,
    devicePixelRatio: positive(viewport.devicePixelRatio) ?? 1,
    sources: sourceCapabilities
  };
}

async function detectSourceCapability(
  source: PlaybackSource,
  mediaElement: CodecProbe | undefined,
  mediaSource: MediaSourceConstructorLike | undefined,
  mediaCapabilities: MediaCapabilitiesLike | undefined
): Promise<PlaybackSourceCapability> {
  const contentTypes = codecProbeContentTypes(source);
  if (source.type === "dash" && (!mediaSource || !contentTypes.every((type) => mediaSource.isTypeSupported(type)))) {
    return unsupported(source, "media_source");
  }
  const canPlayResults = contentTypes.map((type) => mediaElement?.canPlayType(type) ?? "");
  if (canPlayResults.some((result) => result === "")) return unsupported(source, "codec");
  if (!mediaCapabilities) return supported(source, true, true);

  try {
    const result = await mediaCapabilities.decodingInfo(decodingConfiguration(source));
    if (!result.supported) return unsupported(source, "decoding");
    return supported(source, result.smooth, result.powerEfficient);
  } catch {
    return supported(source, canPlayResults.every((result) => result === "probably"), true);
  }
}

function decodingConfiguration(source: PlaybackSource): MediaDecodingConfiguration {
  const videoCodec = source.codecs.find((codec) => !isAudioCodec(codec));
  const audioCodec = source.codecs.find(isAudioCodec);
  const configuration: MediaDecodingConfiguration = {
    type: source.type === "dash" ? "media-source" : "file",
    video: {
      contentType: codecType("video/mp4", videoCodec),
      width: source.width ?? 1280,
      height: source.height ?? 720,
      bitrate: source.bitrate ?? 2_000_000,
      framerate: 30
    }
  };
  if (audioCodec) {
    configuration.audio = {
      contentType: codecType("audio/mp4", audioCodec),
      channels: "2",
      bitrate: 128_000,
      samplerate: 48_000
    };
  }
  return configuration;
}

export function codecContentType(source: PlaybackSource): string {
  const mimeType = source.type === "dash" ? "video/mp4" : source.mimeType;
  return source.codecs.length ? `${mimeType}; codecs="${source.codecs.join(",")}"` : mimeType;
}

function codecProbeContentTypes(source: PlaybackSource): readonly string[] {
  const videoCodec = source.codecs.find((codec) => !isAudioCodec(codec));
  const audioCodec = source.codecs.find(isAudioCodec);
  const values = [codecType("video/mp4", videoCodec)];
  if (audioCodec) values.push(codecType("audio/mp4", audioCodec));
  return values;
}

function codecType(mimeType: string, codec: string | undefined): string {
  return codec ? `${mimeType}; codecs="${codec}"` : mimeType;
}

function isAudioCodec(codec: string): boolean {
  return /^(mp4a|opus|vorbis|ac-3|ec-3)/i.test(codec);
}

export function isConstrainedNetwork(capabilities: PlaybackClientCapabilities): boolean {
  if (!capabilities.online || capabilities.saveData) return true;
  if (/^(slow-2g|2g|3g)$/.test(capabilities.effectiveType)) return true;
  if (capabilities.downlinkMbps !== undefined && capabilities.downlinkMbps < 1.5) return true;
  return capabilities.rttMs !== undefined && capabilities.rttMs >= 500;
}

function supported(source: PlaybackSource, smooth: boolean, powerEfficient: boolean): PlaybackSourceCapability {
  return { sourceId: source.id, playable: true, smooth, powerEfficient };
}

function unsupported(
  source: PlaybackSource,
  reason: NonNullable<PlaybackSourceCapability["reason"]>
): PlaybackSourceCapability {
  return { sourceId: source.id, playable: false, smooth: false, powerEfficient: false, reason };
}

function browserNavigator(): NavigatorCapabilitiesLike | undefined {
  return typeof navigator === "undefined" ? undefined : navigator as NavigatorCapabilitiesLike;
}

function browserMediaSource(): MediaSourceConstructorLike | undefined {
  return typeof MediaSource === "undefined" ? undefined : MediaSource;
}

function browserCodecProbe(): CodecProbe | undefined {
  return typeof document === "undefined" ? undefined : document.createElement("video");
}

function browserViewport(): CapabilityDetectionOptions["viewport"] {
  if (typeof window === "undefined") return { width: 1, height: 1, devicePixelRatio: 1 };
  return {
    width: window.innerWidth,
    height: window.innerHeight,
    devicePixelRatio: window.devicePixelRatio
  };
}

function normalizeSignal(value: string | undefined): string {
  return value?.trim().toLowerCase() ?? "";
}

function positive(value: number | undefined): number | undefined {
  return value !== undefined && Number.isFinite(value) && value > 0 ? value : undefined;
}
