## 1. Environment Preparation

- [x] 1.1 Validate the frontend build and local service configuration
- [x] 1.2 Start the complete stack and confirm web and API health
- [x] 1.3 Prepare isolated test credentials and valid upload fixtures

## 2. Browser Smoke Coverage

- [x] 2.1 Verify registration, login, authenticated navigation, logout, and repeat login
- [x] 2.2 Verify upload, own profile, profile editing, uploaded work, and work viewer
- [x] 2.3 Verify timeline, recommendation, following, and hot feed routes
- [x] 2.4 Verify available like, favorite, follow, swipe, and comment interactions
- [x] 2.5 Verify messages, read controls, public profile, and relationship views
- [x] 2.6 Review browser console errors and failed required network requests

## 3. Regression Resolution

- [x] 3.1 Reproduce and fix issues discovered by the smoke run; no TypeScript migration regression was found, while RabbitMQ readiness and nginx upstream re-resolution were corrected
- [x] 3.2 Repeat the complete browser suite after fixes

## 4. Completion

- [x] 4.1 Run the frontend production build and strict OpenSpec validation
- [x] 4.2 Mark `migrate-web-to-typescript` smoke task 6.4 complete
- [x] 4.3 Record final passing coverage in this change

## Verification

- Browser run: 20/20 scenarios passed with two isolated users and real uploaded video, cover, and avatar files.
- Feed coverage: timeline, recommendation, following, and hot scenes all rendered video content.
- Interaction coverage: wheel navigation, follow, like, favorite, comment, public profile, relationship lists, and work viewer.
- Message coverage: three generated messages loaded; individual and bulk read controls passed.
- Runtime diagnostics: zero console errors, page errors, failed required requests, or HTTP error responses.
- Evidence: session artifact `files/browser-smoke/result.json` and passing screenshots.
