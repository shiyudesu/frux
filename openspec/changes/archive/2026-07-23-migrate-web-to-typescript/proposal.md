# Proposal: migrate-web-to-typescript

## Why

`apps/web` 当前是 2810 行单文件 `App.jsx`（约 20 个组件、30+ 处隐式类型的状态），无任何类型保障，API 响应形状全靠约定，重构与新增功能时易出错且无法被工具链捕获。项目决定重构前端并**一次性**全量迁移到 TypeScript——strict 模式直接开满，不留 `@ts-nocheck`/`any` 等迁移债，避免后期二次迁移返工。

## What Changes

- 引入 TypeScript 工具链：`typescript` 依赖、与 React 18 运行时同主版本的类型定义、`tsconfig.json`（`strict: true` + `noUnusedLocals`/`noUnusedParameters` + `verbatimModuleSyntax`），`vite.config.js` → `vite.config.ts`，`index.html` 入口指向 `main.tsx`
- `package.json` 的 `build` 脚本改为 `tsc --noEmit && vite build`，类型错误在构建期直接失败
- 拆分 `src/App.jsx`（2810 行）为模块结构：`types.ts`、`api/`（client + 按域分组）、`session.tsx`（SessionContext，消除 prop drilling）、`router.tsx`（保留手写路由，路由收敛为 union 类型）、`pages/`、`components/`、`hooks/`
- 对照 `apps/api/internal/domain/*/entity.go` 手写 `Video`/`User`/`Comment` 等领域类型与 API 响应类型，`apiRequest` 泛型化
- 拆解 FeedPage（700+ 行）：数据逻辑收敛进 `useFeed`/`useComments`/`useSwipe` hooks，展示层拆为 `VideoStage`/`CommentPanel` 等组件
- 全程禁止 `@ts-nocheck`、`@ts-expect-error`、裸 `any`；行为保持完全一致（纯重构，无 UX 变更）
- 不引入 react-router、react-query、状态管理库等新运行时依赖；不引入前端测试框架
- 删除 `App.jsx` 与 `main.jsx`

## Capabilities

### New Capabilities

- `web-frontend`: `apps/web` 前端的应用架构约定——TypeScript strict 代码基线、模块分层（types/api/session/router/pages/components/hooks）、类型化 API 客户端、Context 会话分发、手写类型化路由、构建期类型检查门禁

### Modified Capabilities

- `web-package-management`: 构建要求变化——`pnpm run build` 现在先执行 `tsc --noEmit` 再 `vite build`，Docker 镜像构建同样经过类型检查门禁（依赖仍由 pnpm 管理，锁文件与 Corepack 约定不变）

## Impact

- **代码**：`apps/web/src/**` 全部重写为 `.ts`/`.tsx`；`apps/web/vite.config.js`、`apps/web/index.html`、`apps/web/package.json`
- **依赖**：新增 devDependency `typescript`、`@types/react`、`@types/react-dom`（均由 pnpm 管理，无新运行时依赖）
- **API**：无变化（后端 Go API 不动，前端类型对照现有响应手写）
- **Docker**：`apps/web/Dockerfile` 无需改动（仍走 `pnpm run build`），但构建现在包含类型检查，类型错误会导致镜像构建失败
- **风险**：纯行为保持型重构，主要风险是拆分过程中的行为回归，靠逐页面手动冒烟验证兜底
