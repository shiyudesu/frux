## 1. Admin Credential Foundation

- [ ] 1.1 Extend JWT claims and validation with explicit consumer/admin token types and audiences while preserving existing consumer token compatibility.
- [ ] 1.2 Add configuration for bounded admin token lifetime with validation and a conservative default/hard maximum.
- [ ] 1.3 Implement an Admin Authentication Service that reuses account password verification, requires active admin eligibility, and returns generic failures.
- [ ] 1.4 Issue `admin_access` tokens without trusting embedded role claims as current permissions.
- [ ] 1.5 Add unit tests for correct purpose/audience, wrong-purpose rejection, expiry, malformed claims, inactive accounts, and non-admin accounts.

## 2. Admin Login and Route Protection

- [ ] 2.1 Add `POST /api/admin/auth/login` with strict bounded JSON parsing, stable errors, no registration behavior, and layered login rate limiting.
- [ ] 2.2 Add admin-purpose authentication middleware that resolves the account subject before existing current-role permission middleware.
- [ ] 2.3 Move `/api/admin/me` and every other protected admin route behind admin-purpose authentication while leaving only the login endpoint public.
- [ ] 2.4 Return a stable admin-authentication 401 for missing, expired, malformed, consumer-purpose, or wrong-audience credentials.
- [ ] 2.5 Preserve immediate demotion/disable behavior and stable 403/503 authorization responses after admin authentication succeeds.
- [ ] 2.6 Add API-flow tests proving ordinary consumer tokens cannot access admin APIs and handlers do not execute on authentication failure.

## 3. Isolated Admin Web Session

- [ ] 3.1 Extend the typed History API router with canonical `/admin/login` and validated admin return destinations.
- [ ] 3.2 Add typed admin-login API functions and validated persisted Admin session types.
- [ ] 3.3 Implement `AdminSessionProvider` using a versioned `sessionStorage` key independent from the consumer Session provider.
- [ ] 3.4 Implement the dedicated admin login page with generic safe errors, no registration affordance, and validated return navigation.
- [ ] 3.5 Refactor Admin Shell bootstrap and API calls to use only the admin token and `/api/admin/me`.
- [ ] 3.6 Clear admin credentials and cached privileged data on authoritative 401 without altering consumer session state.
- [ ] 3.7 Implement admin-only logout and verify consumer logout leaves Admin session state untouched.
- [ ] 3.8 Remove the legacy `adminPrincipal` coupling from the consumer session without changing ordinary login, upload, message, or profile flows.

## 4. Verification and Cutover

- [ ] 4.1 Add frontend tests for direct admin navigation, consumer-only login, malformed stored admin data, dual-session coexistence, independent logout, 401, 403, and 503 states.
- [ ] 4.2 Run targeted JWT/auth/admin API tests, all affected authorization flow tests, and the strict Web production build.
- [ ] 4.3 Update README and `docs/modules/account.md`/`admin.md` with the dedicated login URL, bootstrap requirements, token boundary, lifetime, and operator cutover.
- [ ] 4.4 Perform browser validation that user and admin sessions coexist and that `/admin/*` never redirects to the consumer registration/login experience.
