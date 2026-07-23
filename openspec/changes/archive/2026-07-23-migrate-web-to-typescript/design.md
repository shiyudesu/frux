# Design: migrate-web-to-typescript

## Context

`apps/web` 是 React 18 + Vite 5 前端，全部代码集中在 `src/App.jsx`（2810 行，约 20 个组件）+ `src/main.jsx`（10 行）。后端 Go API（`apps/api`）领域模型清晰（`internal/domain/*/entity.go`），但无 OpenAPI。已有基础设施：底部存在 `apiRequest` 统一封装（App.jsx:2637）和一组按域 fetch 函数；路由为手写 `normalizeRoute` + `popstate`（约 50 行）。主要痛点：props 全隐式类型、`session`/`onNavigate` 四层穿透、FeedPage 700+ 行且 30+ 个 useState、数据获取逻辑散落各组件。

硬约束（来自需求方）：**只迁移这一次**，不留任何需要后期返工的迁移债。

## Goals / Non-Goals

**Goals:**
- 前端源码 100% TypeScript（`.ts`/`.tsx`），无残留 `.jsx`
- `strict: true` 从第一天开满，配合 `noUnusedLocals`/`noUnusedParameters`/`verbatimModuleSyntax`；零 `@ts-nocheck`、零 `@ts-expect-error`、零裸 `any`
- 模块分层落地，FeedPage 拆解完成，prop drilling 消除
- 类型检查焊入构建：`pnpm run build` = `tsc --noEmit && vite build`，类型腐烂在构建期爆炸而非悄悄积累
- 行为与 UX 完全不变（纯重构）

**Non-Goals:**
- 不引入 react-router / react-query / zustand 等运行时库（项目重后端轻前端，手写路由已够用）
- 不引入前端测试框架（无既有基建，收益不成比例；验证靠 tsc + build + 手动冒烟）
- 不做 UX/视觉/交互改动，不做功能增删
- 不为后端补 OpenAPI 或做类型 codegen（独立话题，未来再说）
- 不用 ts-migrate 等 codemod 工具（见 Decisions）

## Decisions

### D1: 手工迁移，不用 ts-migrate

ts-migrate 为数百文件级代码库设计，其 React 类型推断重度依赖 PropTypes——本代码库无 PropTypes，跑完只会得到一个满屏 `@ts-expect-error`、props 全 `any` 的 2810 行 `.tsx`，类型收益≈0 且背上清理 suppression 的债，恰好违反"不留迁移债"的硬约束。对 2 个文件的规模，手工迁移反而更快且质量最高。

### D2: 边拆边迁，单步完成（非先迁后拆 / 先拆后迁）

按模块切分，切出的每个文件直接写成 `.ts`/`.tsx`：先 `types.ts` → `api/` → `session.tsx`/`router.tsx` → 逐页面剥离 → 最后拆 FeedPage → 删除 `App.jsx`。每一步结束 `tsc --noEmit && vite build` 都是绿的。类型的形状在拆分过程中自然浮现，避免整文件改名后对着满屏红线盲拆，也避免拆两遍迁两遍。

### D3: 目标模块结构

```
apps/web/src/
├── types.ts            # Video/User/Comment/CursorPage<T> 等，对照 Go entity 手写
├── api/
│   ├── client.ts       # apiRequest<T> 泛型化 + 401 处理 + uploadFile
│   ├── feed.ts         # fetchFeedPage / fetchPlaybackConfig / fetchPreloadVideos
│   ├── messages.ts     # fetchMessages / markMessagesRead
│   ├── social.ts       # 关注/点赞/收藏/评论相关
│   └── account.ts      # 登录/注册/资料
├── session.tsx         # SessionContext + useSession()，分发 token/user/login/logout
├── router.tsx          # Route union 类型 + normalizeRoute + navigate + useRoute
├── pages/              # Login/Feed/Messages/Profile/PublicProfile/Upload 各一文件
├── components/         # AppShell/TopNav/VideoStage/CommentPanel/ActionButton/
│                       # WorkViewer/VideoGrid/RelationList/RelationModal/各类 Message 组件
├── hooks/              # useFeed / useComments / useSwipe / useUnreadCount
├── App.tsx             # 只剩路由分发与 Provider 组装
└── main.tsx
```

### D4: API 边界类型手写，对照 Go entity

后端无 OpenAPI，codegen 无从谈起。从 `apps/api/internal/domain/*/entity.go` 和 `interfaces/http` 的响应结构对照手写 interface。这是本次迁移类型价值的核心来源：fetch 返回值不再是 `any`，字段拼写错误在编译期暴露。类型漂移风险存在但可接受——后端 API 稳定，且 `tsc` 门禁保证前端内部一致。

### D5: 保留手写路由，类型化为 union

`type Route = "/timeline" | "/recommend" | "/following" | "/hotfeed" | "/login" | "/profile" | "/upload" | "/messages" | { user: number }` 之类的判别联合。50 行手写路由已满足需求，换 react-router 是替别人写代码；但 union 类型能让 `onNavigate("/proflie")` 这类笔误编译期爆炸——这是 TS 白送的红利。

### D6: SessionContext 消除 prop drilling

`session`/`onNavigate` 目前从 App 穿透 AppShell→TopNav→各页面（4 层）。机械地换为 `SessionContext`（token、user、login、logout、refreshUnreadCount）+ `useSession()` hook；路由跳转提供 `useNavigate()`。纯机械改动，风险≈0。

### D7: FeedPage 拆解为 hooks + 展示组件

700+ 行、30+ useState 是"只迁一次"要求下最大的残留债务候选。拆分：
- `useFeed(scene)`：items/index/cursor/hasMore/loadingMore/feedState + 加载与翻页
- `useComments(videoID)`：评论面板状态与提交
- `useSwipe()`：手势/滑动切换逻辑
- 展示层：`VideoStage`、`CommentPanel`、`ActionButton`、`FeedMessage`（已存在，直接搬出）

### D8: TypeScript 工具链与 tsconfig 配置

`@types/react` 与 `@types/react-dom` 保持和 React 18 运行时相同的主版本，避免编译器接受运行时不存在的 React 19 API。`strict: true`、`noUnusedLocals`、`noUnusedParameters`、`verbatimModuleSyntax`（强制 `import type`，避免 Vite 下类型导入误入运行时）、`moduleResolution: "bundler"`、`jsx: "react-jsx"`、`noEmit: true`。`build` 脚本改为 `tsc --noEmit && vite build`——这是"不返工"的保险丝。

### D9: 不加测试框架

前端为项目配角、零既有测试基建，DOM/hooks 测试需引入 vitest+jsdom 等一套设施，与项目定位不成比例。回归保障 = `tsc --noEmit`（strict）+ `vite build` + 逐页面手动冒烟清单（登录/四场景 Feed/评论/消息/资料/上传/作品查看）。

## Risks / Trade-offs

- [拆分过程中行为回归（state 提升/降层时机、effect 依赖变化）] → 每页面剥离后立即 build + 该页面手动冒烟；diff 以"搬运"为主，禁止顺手改逻辑
- [手写类型与 Go 响应漂移] → 接受风险；tsc 保证前端内部一致；未来若 API 演进频繁再考虑 OpenAPI/codegen
- [strict 全开导致个别位置类型表达困难（如手势事件、video 元素 ref）] → 用精确类型（`React.TouchEvent`、`HTMLVideoElement`）解决；确实无解处用最小范围的类型断言并注释原因，仍禁止 `any`/`ts-ignore`
- [无测试兜底，回归靠人工] → 冒烟清单覆盖全部 6 个页面 + 核心交互；迁移后第一轮使用即观察期
- [localStorage 读出的 user/publicProfile 是不可信 JSON] → 在 `types.ts` 写窄化函数（type guard），读取处校验而非断言

## Migration Plan

单分支一次性完成，按 D2 顺序推进，每步保持构建绿。无需灰度/回滚策略——静态站点，构建产物行为等价；若发现回归，git revert 即可。

## Open Questions

- 无。（库选型、FeedPage 拆分、测试策略、strict 策略均已在探索阶段与需求方确认）
