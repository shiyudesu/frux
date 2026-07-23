# Tasks: migrate-web-to-typescript

## 1. 工具链与配置

- [x] 1.1 在 `apps/web` 添加 devDependencies：`typescript`、`@types/react`、`@types/react-dom`（React 类型定义与 React 18 运行时保持同主版本；pnpm 安装并更新 lockfile）
- [x] 1.2 创建 `apps/web/tsconfig.json`：`strict`/`noUnusedLocals`/`noUnusedParameters`/`verbatimModuleSyntax` 全开，`moduleResolution: "bundler"`，`jsx: "react-jsx"`，`noEmit: true`
- [x] 1.3 `vite.config.js` → `vite.config.ts`，`main.jsx` → `main.tsx`，更新 `index.html` 入口引用
- [x] 1.4 `package.json` 的 `build` 脚本改为 `tsc --noEmit && vite build`，验证 `pnpm run build` 通过

## 2. 类型与 API 层

- [x] 2.1 对照 `apps/api/internal/domain/*/entity.go` 与 HTTP 响应结构，编写 `src/types.ts`（`User`/`Video`/`Comment`/`Message`/分页响应等）
- [x] 2.2 为 `localStorage` 读取（user、publicProfiles）编写 type guard 窄化函数
- [x] 2.3 迁移 `apiRequest` 到 `src/api/client.ts` 并泛型化为 `apiRequest<T>`，含 401 处理与 `uploadFile`
- [x] 2.4 按域迁移 fetch 函数到 `src/api/`（feed/messages/social/account），返回值接上 `types.ts` 声明的类型

## 3. 会话与路由基础设施

- [x] 3.1 创建 `src/session.tsx`：`SessionContext` + `useSession()`，提供 token/user/login/logout/refreshUnreadCount
- [x] 3.2 创建 `src/router.tsx`：Route union 类型 + `normalizeRoute` + `navigate` + `useRoute()`/`useNavigate()`

## 4. 逐页剥离（每页完成后 build 必须绿）

- [x] 4.1 剥离共享组件到 `src/components/`（AppShell/TopNav/ActionButton/VideoGrid/RelationList/RelationModal/WorkViewer/各类 Message 组件），props 全部显式类型化
- [x] 4.2 剥离 `LoginPage` 到 `src/pages/`，改用 `useSession`/`useNavigate`，手动冒烟登录/注册流程
- [x] 4.3 剥离 `MessagesPage`，手动冒烟消息列表/已读/全部已读
- [x] 4.4 剥离 `ProfilePage` 与 `PublicProfilePage`，手动冒烟资料编辑/头像上传/关注列表/作品查看
- [x] 4.5 剥离 `UploadPage`，手动冒烟视频+封面上传

## 5. FeedPage 拆解

- [x] 5.1 抽取 `src/hooks/useFeed.ts`：items/index/cursor/hasMore/加载与翻页逻辑
- [x] 5.2 抽取 `src/hooks/useComments.ts` 与 `src/hooks/useSwipe.ts`
- [x] 5.3 抽取展示组件 `VideoStage`/`CommentPanel`/`FeedMessage` 到 `src/components/`
- [x] 5.4 重组 `src/pages/FeedPage.tsx` 为容器组件，手动冒烟四个 feed 场景 + 点赞/收藏/关注/评论/滑动切换/预加载

## 6. 收尾与验证

- [x] 6.1 重写 `src/App.tsx` 为纯 Provider 组装 + 路由分发，删除 `App.jsx` 与 `main.jsx`
- [x] 6.2 全仓检查：`apps/web/src` 无 `.js`/`.jsx`，无 `@ts-nocheck`/`@ts-expect-error`/`: any`
- [x] 6.3 `pnpm run build` 通过（tsc 门禁生效）；验证故意引入类型错误时构建失败
- [x] 6.4 按 design.md 冒烟清单走查全部 6 个页面，确认行为与迁移前一致
- [x] 6.5 Docker 镜像构建验证（`apps/web/Dockerfile` 无需改动，确认 `pnpm run build` 含 tsc 步骤即可）
- [x] 6.6 更新 `docs/engineering.md` 等文档中涉及前端技术栈/构建命令的描述
