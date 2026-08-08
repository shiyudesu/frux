import { useEffect, useState } from "react";
import { fetchProtectedAssetAccess } from "../api/upload";
import { image } from "../constants";
import type { Video } from "../types";

interface ProtectedVideoCoverProps {
  video: Video;
  token?: string;
}

interface ResolvedCover {
  assetID: number;
  token: string;
  url: string;
  unavailable: boolean;
}

export function ProtectedVideoCover({ video, token = "" }: ProtectedVideoCoverProps) {
  const [resolved, setResolved] = useState<ResolvedCover | null>(null);
  const assetID = video.cover_asset_id || 0;
  const needsProtectedAccess = Boolean(token && !video.cover_url && assetID);

  useEffect(() => {
    if (!needsProtectedAccess) return undefined;
    let active = true;
    let refreshTimer = 0;
    let retryDelay = 5_000;

    const load = async () => {
      try {
        const access = await fetchProtectedAssetAccess(token, assetID);
        if (!active) return;
        setResolved({ assetID, token, url: access.url, unavailable: false });
        retryDelay = 5_000;
        const expiry = new Date(access.expires_at).getTime();
        if (Number.isFinite(expiry)) {
          refreshTimer = window.setTimeout(
            () => void load(),
            Math.min(2_147_000_000, Math.max(1_000, expiry - Date.now() - 30_000))
          );
        }
      } catch {
        if (!active) return;
        setResolved((current) => ({
          assetID,
          token,
          url: current?.assetID === assetID && current.token === token ? current.url : "",
          unavailable: true
        }));
        const delay = retryDelay;
        retryDelay = Math.min(60_000, retryDelay * 2);
        refreshTimer = window.setTimeout(() => void load(), delay);
      }
    };

    void load();
    return () => {
      active = false;
      window.clearTimeout(refreshTimer);
    };
  }, [assetID, needsProtectedAccess, token]);

  const protectedCover = needsProtectedAccess
    && resolved?.assetID === assetID
    && resolved.token === token
    ? resolved
    : null;
  return (
    <img
      src={video.cover_url || protectedCover?.url || image.stage}
      alt=""
      title={protectedCover?.unavailable ? "封面暂时不可用" : undefined}
    />
  );
}
