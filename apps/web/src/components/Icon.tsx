import type { SVGProps } from "react";

export type IconName =
  | "alert"
  | "bell"
  | "bookmark"
  | "camera"
  | "check"
  | "check-all"
  | "chevron-down"
  | "chevron-up"
  | "close"
  | "comment"
  | "discover"
  | "film"
  | "flame"
  | "fullscreen"
  | "heart"
  | "home"
  | "hourglass"
  | "image"
  | "lock"
  | "login"
  | "logout"
  | "megaphone"
  | "message"
  | "more"
  | "pause"
  | "play"
  | "plus"
  | "publish"
  | "refresh"
  | "reply"
  | "save"
  | "search"
  | "send"
  | "share"
  | "sparkles"
  | "tune"
  | "upload"
  | "user"
  | "user-edit"
  | "user-plus"
  | "users"
  | "video"
  | "volume"
  | "volume-off";

interface IconProps extends Omit<SVGProps<SVGSVGElement>, "name"> {
  name: IconName;
  size?: number;
  filled?: boolean;
}

export function Icon({ name, size = 22, filled = false, className = "", ...props }: IconProps) {
  return (
    <svg
      aria-hidden="true"
      className={`ui-icon ${filled ? "filled" : ""} ${className}`.trim()}
      fill="none"
      height={size}
      viewBox="0 0 24 24"
      width={size}
      {...props}
    >
      {renderIcon(name, filled)}
    </svg>
  );
}

function renderIcon(name: IconName, filled: boolean) {
  const fill = filled ? "currentColor" : "none";
  switch (name) {
    case "alert":
      return (
        <>
          <path d="M12 3 2.8 19h18.4L12 3Z" stroke="currentColor" strokeLinejoin="round" strokeWidth="1.8" />
          <path d="M12 8.5v4.8M12 16.8v.2" stroke="currentColor" strokeLinecap="round" strokeWidth="1.8" />
        </>
      );
    case "bell":
      return (
        <>
          <path d="M6.7 9.5a5.3 5.3 0 0 1 10.6 0c0 5 2.1 5.3 2.1 6.6H4.6c0-1.3 2.1-1.6 2.1-6.6Z" fill={fill} stroke="currentColor" strokeLinejoin="round" strokeWidth="1.7" />
          <path d="M9.7 19a2.7 2.7 0 0 0 4.6 0" stroke="currentColor" strokeLinecap="round" strokeWidth="1.7" />
        </>
      );
    case "bookmark":
      return <path d="M6.5 4.5h11v15l-5.5-3.4-5.5 3.4v-15Z" fill={fill} stroke="currentColor" strokeLinejoin="round" strokeWidth="1.8" />;
    case "camera":
      return (
        <>
          <path d="M7.5 7 9 4.8h6L16.5 7H20v11H4V7h3.5Z" fill={fill} stroke="currentColor" strokeLinejoin="round" strokeWidth="1.7" />
          <circle cx="12" cy="12.5" r="3.2" stroke="currentColor" strokeWidth="1.7" />
        </>
      );
    case "check":
      return <path d="m5 12.5 4.2 4.2L19 7" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" />;
    case "check-all":
      return (
        <>
          <path d="m3.5 12 3 3 5-6" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round" strokeWidth="1.8" />
          <path d="m10.5 14 2 2 8-9" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round" strokeWidth="1.8" />
        </>
      );
    case "chevron-down":
      return <path d="m6 9 6 6 6-6" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" />;
    case "chevron-up":
      return <path d="m6 15 6-6 6 6" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" />;
    case "close":
      return <path d="m6 6 12 12M18 6 6 18" stroke="currentColor" strokeLinecap="round" strokeWidth="1.9" />;
    case "comment":
      return <path d="M4 5.5h16v11H9l-5 3v-14Z" fill={fill} stroke="currentColor" strokeLinejoin="round" strokeWidth="1.8" />;
    case "discover":
      return (
        <>
          <circle cx="12" cy="12" r="8.5" stroke="currentColor" strokeWidth="1.7" />
          <path d="m15.5 8.5-2.1 4.9-4.9 2.1 2.1-4.9 4.9-2.1Z" fill={fill} stroke="currentColor" strokeLinejoin="round" strokeWidth="1.5" />
        </>
      );
    case "film":
    case "video":
      return (
        <>
          <rect fill={fill} height="13" rx="2" stroke="currentColor" strokeWidth="1.7" width="16" x="4" y="5.5" />
          <path d="m10 9 5 3-5 3V9Z" fill={filled ? "var(--frux-bg)" : "currentColor"} />
        </>
      );
    case "flame":
      return <path d="M13.2 3.5c.6 3.7-2 4.3-1.1 7.2.8-1.1 1.7-1.6 2.8-2.3 2.4 2 3.2 4.1 2.4 6.8-.8 2.8-3 4.3-5.5 4.3-3.2 0-5.6-2.3-5.6-5.5 0-3.8 2.7-6.7 7-10.5Z" fill={fill} stroke="currentColor" strokeLinejoin="round" strokeWidth="1.7" />;
    case "fullscreen":
      return <path d="M8.5 4.5h-4v4M15.5 4.5h4v4M8.5 19.5h-4v-4M15.5 19.5h4v-4" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round" strokeWidth="1.8" />;
    case "heart":
      return <path d="M12 20s-7.5-4.4-7.5-10a4.3 4.3 0 0 1 7.5-2.9A4.3 4.3 0 0 1 19.5 10c0 5.6-7.5 10-7.5 10Z" fill={fill} stroke="currentColor" strokeLinejoin="round" strokeWidth="1.8" />;
    case "home":
      return (
        <>
          <path d="m3.5 11 8.5-7 8.5 7" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round" strokeWidth="1.8" />
          <path d="M6 10v9h12v-9M10 19v-5h4v5" fill={fill} stroke="currentColor" strokeLinejoin="round" strokeWidth="1.8" />
        </>
      );
    case "hourglass":
      return <path d="M7 4h10M7 20h10M8 4c0 3.6.9 5.2 4 8-3.1 2.8-4 4.4-4 8M16 4c0 3.6-.9 5.2-4 8 3.1 2.8 4 4.4 4 8" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round" strokeWidth="1.7" />;
    case "image":
      return (
        <>
          <rect fill={fill} height="15" rx="2" stroke="currentColor" strokeWidth="1.7" width="17" x="3.5" y="4.5" />
          <circle cx="9" cy="9" r="1.5" fill="currentColor" />
          <path d="m5.5 17 4.2-4.2 2.8 2.7 2.2-2.1 3.8 3.6" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round" strokeWidth="1.6" />
        </>
      );
    case "lock":
      return (
        <>
          <rect fill={fill} height="10" rx="2" stroke="currentColor" strokeWidth="1.7" width="14" x="5" y="10" />
          <path d="M8 10V7.5a4 4 0 0 1 8 0V10" stroke="currentColor" strokeLinecap="round" strokeWidth="1.7" />
        </>
      );
    case "login":
      return (
        <>
          <path d="M13.5 5H19v14h-5.5M4 12h11M11 8l4 4-4 4" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round" strokeWidth="1.8" />
        </>
      );
    case "logout":
      return (
        <>
          <path d="M10.5 5H5v14h5.5M20 12H9M13 8l-4 4 4 4" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round" strokeWidth="1.8" />
        </>
      );
    case "megaphone":
      return (
        <>
          <path d="M4 10v4h4l8 4V6l-8 4H4Z" fill={fill} stroke="currentColor" strokeLinejoin="round" strokeWidth="1.7" />
          <path d="m8 14 1.5 5h3" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round" strokeWidth="1.7" />
        </>
      );
    case "message":
      return <path d="M4 5.5h16v11H9l-5 3v-14Z" fill={fill} stroke="currentColor" strokeLinejoin="round" strokeWidth="1.8" />;
    case "more":
      return (
        <>
          <circle cx="5" cy="12" r="1.5" fill="currentColor" />
          <circle cx="12" cy="12" r="1.5" fill="currentColor" />
          <circle cx="19" cy="12" r="1.5" fill="currentColor" />
        </>
      );
    case "pause":
      return (
        <>
          <rect fill="currentColor" height="14" rx="1" width="3.5" x="6.5" y="5" />
          <rect fill="currentColor" height="14" rx="1" width="3.5" x="14" y="5" />
        </>
      );
    case "plus":
      return <path d="M12 5v14M5 12h14" stroke="currentColor" strokeLinecap="round" strokeWidth="1.9" />;
    case "play":
      return <path d="m8 5 11 7-11 7V5Z" fill="currentColor" stroke="currentColor" strokeLinejoin="round" strokeWidth="1.2" />;
    case "publish":
    case "upload":
      return (
        <>
          <path d="M12 16V4M7.5 8.5 12 4l4.5 4.5" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round" strokeWidth="1.8" />
          <path d="M5 14v5h14v-5" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round" strokeWidth="1.8" />
        </>
      );
    case "refresh":
      return <path d="M19 7v5h-5M5.4 17a8 8 0 0 0 13.2-3M5 17v-5h5M18.6 7A8 8 0 0 0 5.4 10" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round" strokeWidth="1.8" />;
    case "reply":
      return <path d="m10 7-6 5 6 5v-3c4.8 0 7.8 1.4 10 4-1-5.8-4.2-9-10-9V7Z" fill={fill} stroke="currentColor" strokeLinejoin="round" strokeWidth="1.7" />;
    case "save":
      return (
        <>
          <path d="M5 4h12l2 2v14H5V4Z" fill={fill} stroke="currentColor" strokeLinejoin="round" strokeWidth="1.7" />
          <path d="M8 4v5h7V4M8 20v-6h8v6" stroke="currentColor" strokeLinejoin="round" strokeWidth="1.7" />
        </>
      );
    case "search":
      return (
        <>
          <circle cx="10.5" cy="10.5" r="6" stroke="currentColor" strokeWidth="1.8" />
          <path d="m15 15 5 5" stroke="currentColor" strokeLinecap="round" strokeWidth="1.8" />
        </>
      );
    case "send":
      return <path d="m3.5 11.5 17-7-5.8 15-3.1-6.2-8.1-1.8Zm8.1 1.8 4.2-4.2" fill={fill} stroke="currentColor" strokeLinecap="round" strokeLinejoin="round" strokeWidth="1.7" />;
    case "share":
      return <path d="M14 5 20 12l-6 7v-4c-5.5 0-8 1.5-10 4 1-6 4-9 10-9V5Z" fill={fill} stroke="currentColor" strokeLinejoin="round" strokeWidth="1.7" />;
    case "sparkles":
      return (
        <>
          <path d="M12 3c.7 3 2.3 4.6 5.3 5.3-3 .7-4.6 2.3-5.3 5.3-.7-3-2.3-4.6-5.3-5.3C9.7 7.6 11.3 6 12 3Z" fill={fill} stroke="currentColor" strokeLinejoin="round" strokeWidth="1.4" />
          <path d="M18 14c.4 1.8 1.4 2.8 3.2 3.2-1.8.4-2.8 1.4-3.2 3.2-.4-1.8-1.4-2.8-3.2-3.2 1.8-.4 2.8-1.4 3.2-3.2ZM5 14c.3 1.2.9 1.8 2.1 2.1C5.9 16.4 5.3 17 5 18.2c-.3-1.2-.9-1.8-2.1-2.1C4.1 15.8 4.7 15.2 5 14Z" fill="currentColor" />
        </>
      );
    case "tune":
      return (
        <>
          <path d="M4 7h16M4 17h16" stroke="currentColor" strokeLinecap="round" strokeWidth="1.7" />
          <circle cx="9" cy="7" r="2" fill="var(--frux-surface)" stroke="currentColor" strokeWidth="1.7" />
          <circle cx="15" cy="17" r="2" fill="var(--frux-surface)" stroke="currentColor" strokeWidth="1.7" />
        </>
      );
    case "user":
      return (
        <>
          <circle cx="12" cy="8" r="4" fill={fill} stroke="currentColor" strokeWidth="1.7" />
          <path d="M4.5 20a7.5 7.5 0 0 1 15 0" fill={fill} stroke="currentColor" strokeLinecap="round" strokeWidth="1.7" />
        </>
      );
    case "user-edit":
      return (
        <>
          <circle cx="10" cy="7.5" r="3.5" stroke="currentColor" strokeWidth="1.7" />
          <path d="M3.8 18a6.2 6.2 0 0 1 10.8-4.2M15 19l4.8-4.8 1.5 1.5-4.8 4.8-2 .5.5-2Z" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round" strokeWidth="1.7" />
        </>
      );
    case "user-plus":
      return (
        <>
          <circle cx="9" cy="8" r="3.5" fill={fill} stroke="currentColor" strokeWidth="1.7" />
          <path d="M3.5 19a5.5 5.5 0 0 1 11 0M18 8v6M15 11h6" stroke="currentColor" strokeLinecap="round" strokeWidth="1.7" />
        </>
      );
    case "users":
      return (
        <>
          <circle cx="9" cy="8" r="3.5" fill={fill} stroke="currentColor" strokeWidth="1.7" />
          <path d="M3.5 19a5.5 5.5 0 0 1 11 0" fill={fill} stroke="currentColor" strokeLinecap="round" strokeWidth="1.7" />
          <path d="M15 5.2a3.2 3.2 0 0 1 0 6.2M16.2 14a4.8 4.8 0 0 1 4.3 5" stroke="currentColor" strokeLinecap="round" strokeWidth="1.7" />
        </>
      );
    case "volume":
      return (
        <>
          <path d="M4 10v4h4l5 4V6l-5 4H4Z" fill={fill} stroke="currentColor" strokeLinejoin="round" strokeWidth="1.7" />
          <path d="M16 9a4 4 0 0 1 0 6M18.5 6.5a7.5 7.5 0 0 1 0 11" stroke="currentColor" strokeLinecap="round" strokeWidth="1.7" />
        </>
      );
    case "volume-off":
      return (
        <>
          <path d="M4 10v4h4l5 4V6l-5 4H4Z" stroke="currentColor" strokeLinejoin="round" strokeWidth="1.7" />
          <path d="m16 9 5 6M21 9l-5 6" stroke="currentColor" strokeLinecap="round" strokeWidth="1.7" />
        </>
      );
  }
}
