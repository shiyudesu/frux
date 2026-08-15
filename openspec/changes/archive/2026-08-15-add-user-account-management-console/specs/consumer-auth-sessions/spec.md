## ADDED Requirements

### Requirement: Privileged Ordinary Account Session Revocation
Privileged freeze, unfreeze, and force-sign-out operations SHALL increment the ordinary account authentication version. Freeze and force-sign-out SHALL revoke every active durable refresh session in the same transaction; unfreeze SHALL NOT restore or replace any previously revoked session.

#### Scenario: Frozen account attempts refresh
- **WHEN** a refresh credential belongs to an account frozen by a committed privileged operation
- **THEN** refresh is rejected and no replacement access or refresh credential is issued

#### Scenario: Forced-out account attempts refresh
- **WHEN** a refresh credential predates a committed force-sign-out operation
- **THEN** its revoked state or stale authentication version prevents refresh

#### Scenario: Account is unfrozen
- **WHEN** a frozen account returns to normal status
- **THEN** the user must complete a new password login because all prior durable sessions remain revoked

#### Scenario: Previously issued access token is used
- **WHEN** a consumer access token was issued before freeze or force-sign-out and has not reached its short expiration
- **THEN** it may remain cryptographically valid only until that existing expiration, can read already-delivered account messages through the normal message API, and cannot be refreshed afterward

### Requirement: Password-Proven Frozen Login Result
Consumer password login SHALL return a dedicated frozen-account result only after the submitted password has authenticated against the current account credential. The result SHALL issue no access token, refresh credential, refresh-session row, asset cookie, or replacement session.

#### Scenario: Frozen account submits the correct password
- **WHEN** a frozen ordinary account submits its current correct password
- **THEN** Frux returns HTTP 423 with `AUTH_ACCOUNT_FROZEN` and creates no authentication credential

#### Scenario: Frozen account submits a wrong password
- **WHEN** a frozen account submits an incorrect password
- **THEN** Frux returns the same `AUTH_INVALID_CREDENTIALS` response as any other wrong password and does not reveal the frozen state

#### Scenario: Unknown account attempts login
- **WHEN** a syntactically valid unknown account submits a password
- **THEN** Frux performs the existing dummy bcrypt work and returns `AUTH_INVALID_CREDENTIALS`

#### Scenario: Cancelled account submits the correct password
- **WHEN** a cancelled account submits its previously valid password
- **THEN** Frux retains the generic invalid-credentials result and creates no session

#### Scenario: Frozen account is later unfrozen
- **WHEN** the account returns to normal status
- **THEN** a subsequent correct password login can create a new session while all credentials revoked by the earlier freeze remain unusable
