import { forwardRef, useEffect, useRef, useState } from "react";
import type { MouseEvent as ReactMouseEvent } from "react";
import { Icon } from "./Icon";

export const STAGE_SINGLE_CLICK_DELAY_MS = 240;
const LIKE_BURST_DURATION_MS = 680;

interface StageGestureLayerProps {
  active: boolean;
  videoID: number;
  liked: boolean;
  canLike: boolean;
  onLike: () => void;
  onTogglePlayback: () => void;
}

interface LikeBurst {
  id: number;
  x: number;
  y: number;
}

export const StageGestureLayer = forwardRef<HTMLDivElement, StageGestureLayerProps>(
  function StageGestureLayer(
    { active, videoID, liked, canLike, onLike, onTogglePlayback },
    ref
  ) {
    const clickTimer = useRef(0);
    const burstTimer = useRef(0);
    const burstID = useRef(0);
    const [burst, setBurst] = useState<LikeBurst | null>(null);

    useEffect(() => () => {
      window.clearTimeout(clickTimer.current);
      window.clearTimeout(burstTimer.current);
    }, []);

    useEffect(() => {
      window.clearTimeout(clickTimer.current);
      window.clearTimeout(burstTimer.current);
      setBurst(null);
    }, [active, videoID]);

    function handleClick(event: ReactMouseEvent<HTMLDivElement>) {
      if (!active || event.detail !== 1) return;
      window.clearTimeout(clickTimer.current);
      clickTimer.current = window.setTimeout(onTogglePlayback, STAGE_SINGLE_CLICK_DELAY_MS);
    }

    function handleDoubleClick(event: ReactMouseEvent<HTMLDivElement>) {
      if (!active) return;
      event.preventDefault();
      window.clearTimeout(clickTimer.current);
      if (!canLike) return;
      const bounds = event.currentTarget.getBoundingClientRect();
      const nextBurst = {
        id: ++burstID.current,
        x: event.clientX - bounds.left,
        y: event.clientY - bounds.top
      };
      setBurst(nextBurst);
      window.clearTimeout(burstTimer.current);
      burstTimer.current = window.setTimeout(() => {
        setBurst((current) => current?.id === nextBurst.id ? null : current);
      }, LIKE_BURST_DURATION_MS);
      if (!liked) onLike();
    }

    return (
      <>
        <div
          ref={ref}
          className="stage-media-host"
          data-ui="stage-gesture-layer"
          onClick={handleClick}
          onContextMenu={(event) => event.preventDefault()}
          onDoubleClick={handleDoubleClick}
        />
        {burst && (
          <span
            aria-hidden="true"
            className="stage-like-burst"
            key={burst.id}
            style={{ left: burst.x, top: burst.y }}
          >
            <Icon name="heart" size={88} filled />
          </span>
        )}
      </>
    );
  }
);
