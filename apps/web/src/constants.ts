// 全局常量：localStorage 键、Feed 场景、默认播放配置、占位图片。
// 值与迁移前 LegacyApp.jsx 顶部常量完全一致。
import type { PlaybackConfig, SessionUser } from "./types";
import type { IconName } from "./components/Icon";

export const TOKEN_KEY = "gcfeed.accessToken";
export const USER_KEY = "gcfeed.user";
export const PUBLIC_PROFILE_KEY = "gcfeed.publicProfiles";
export const FEED_TRANSITION_MS = 320;

export const DEFAULT_PLAYBACK_CONFIG: PlaybackConfig = {
  platform: "Web",
  network_type: "DEFAULT",
  preload_count: 3,
  buffer_ms: 1200
};

export type FeedSceneKey = "timeline" | "recommend" | "following" | "hot";

/** Feed 场景对应的路由（Route union 的子集，在 constants 中声明以避免与 router 循环依赖） */
export type FeedSceneRoute = "/timeline" | "/recommend" | "/following" | "/hotfeed";

export interface FeedSceneMeta {
  key: FeedSceneKey;
  label: string;
  route: FeedSceneRoute;
  icon: IconName;
}

export const FEED_SCENES: FeedSceneMeta[] = [
  { key: "timeline", label: "最新", route: "/timeline", icon: "discover" },
  { key: "recommend", label: "推荐", route: "/recommend", icon: "sparkles" },
  { key: "following", label: "关注", route: "/following", icon: "users" },
  { key: "hot", label: "热门", route: "/hotfeed", icon: "flame" }
];

/** LegacyApp 中 `FEED_SCENES.find(...) || FEED_SCENES[0]` 的等价封装 */
export function getFeedSceneMeta(key: string): FeedSceneMeta {
  return FEED_SCENES.find((scene) => scene.key === key) || FEED_SCENES[0];
}

export const image = {
  currentUser:
    "https://lh3.googleusercontent.com/aida-public/AB6AXuAEH5ZNpPdQoO7Qiy3CGshEypK0dp1HFeVoZ1TAHDLhfcvYMg_js-k2rhBTIPpqMjs6GQpIgKMnUhIu0tY_QYUCTocPD70FDbGWYmHI25NPQ1Quod_7Ssaq0ICv7vvwNephDLNouriPhG7ubVy8GZKbFP9Qi-2yLe2mzST0t9Vfygo2jvgiHITh11LVRZ2ZTcE3nmySn6ZMnpqONtz0zbaKbQMLsDNfR-5smwYHCQLvdp6U5U2-OW_kZS1P6U9vR_PN9Ey84a1VDgRZ",
  stage:
    "https://lh3.googleusercontent.com/aida-public/AB6AXuBoRvSlHsGSK5JYfx8r7praM2C7qfaT8MA3oCiEBrp2qR1Ew_d_BBW1bayhxrA9QACs__BYjSfSKuyEvZcT0YtXO8fuXj8VQ2YLiuTimXER4hQXjdpWsSohnXC6O_Q_Ax3IYrf6kxn3pfnf3gbpdpHg6Z_gBGl-pwwh9QZ1MJMCDFNOgDIYu6YlIUcGa_f9muHACh25ulddKdk1mb9Ml2sMhagIzsTCt5xLaDwtQUM8HjhIkIrThVgRoRpajSVgMilICEgR6TB1uoLn",
  vertical:
    "https://lh3.googleusercontent.com/aida-public/AB6AXuAqJ-TzzKMaUW37h8FSVT6sySUyelD9iqAXQM8_V6Ynq8kYCFHFl-i5bCoymxVX4HRAhhWgD59axTWzgDp0cHyhyxghctwlas-jU5GyIstMv9SFzSLAx6tbBm85-yYz-578vmofewsYO3GeSOn7DOfZehI-h4AYI4TVeLPJp1t2qRfNFfYVTM6wFRmrN6KpTsUf-i1KDnFjGY0jsdTWvNSWT4ESDCXtOBQ9aWp9AzFdF4KNeN2DiNc5TqpFDECYEYYb8xODhOueWS1n",
  creator:
    "https://lh3.googleusercontent.com/aida-public/AB6AXuCIV1PKtZYRoVkb2NMSoMUWK2b4z4ud3anAMawRZcTvvO65A3fdP_VxZ-SKBjbFmKTXq8K4_5u7hDf1HFGvgAeGcRLain2KtgULNWhuvqWY6DarBw00-1b5W5FbUG65hymKyOYSaKWWhutXHzhRpe9P6PtNySTafG8eHDMWiY3Nd98DFbfRucptBxPPwEiuHqa25JIulogR7d149IxiPQBll9Cj5SbLwJJHMJwyYDdjLt6Xeb3SxzEo_wXQRoy_1ygtQV0BrhnwB-Gc",
  elena:
    "https://lh3.googleusercontent.com/aida-public/AB6AXuCfqKNkFePsMLBDZYqyGujFrw7aFgKuMbRmsfxl2RU6YwArmAMkd2WUVUnThLXS1IZ06GUZtBEwByaBo_gZorXeQZDI80CTTRv_UVwE8zOrSzQcYoPs610EFhylJndZzJdHfZ_baQVq05Jrr2nIs7XAaPX61Z2ztTTJW_J15FaeL8r-SltWdmFelv1138YY_ZzeCtPYBsTCVFSJ4jCOoKS346foSAxivJV0V-VKDfC87OD2aJvmJUGF28t-s9gS2MaTUjbEeLcHAmt",
  marcus:
    "https://lh3.googleusercontent.com/aida-public/AB6AXuB7uaOE97IUWoZ4UATBaoSTyBJZUmaVDIznnBD7dhe-84PxZmlZ1V8lAgzaub9vACzqMryk0r96bzhbWPWr4VFjb6IQKxipytjB0_hO7yATBoAmjoMitTHcKYf-KgpXjA0_I4DP8Kym-JYSOhbsDxtiwXkZk1KHPGMerpuvZm24J8JkfExcRH-_8MFsLcGC6tkPin9XxwLp41Hu7yaTCJ6G2--rRM8JW2W9wtB0kXH3sn6754xv94qNtJNdCnyg7cpmz7tbIePycKd5"
};

/**
 * 未登录时的占位用户。迁移前只有部分字段，其余字段读取时为 undefined；
 * 这里按 SessionUser 补齐零值字段，读取处行为等价（undefined ?? 0 与 0 相同）。
 */
export const emptyProfile: SessionUser = {
  id: 0,
  account: "",
  nickname: "",
  avatar_url: image.currentUser,
  bio: "",
  role: "",
  status: 0,
  following_count: 0,
  follower_count: 0,
  work_count: 0
};
