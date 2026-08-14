## 1. Processing Configuration

- [x] 1.1 Add validated media maximum-duration, command-timeout, and ffmpeg-preset configuration fields with safe defaults
- [x] 1.2 Set explicit local, Docker, and Prod processing values and update configuration tests
- [x] 1.3 Wire the validated processing policy into Worker processor construction

## 2. FFmpeg Reliability

- [x] 2.1 Apply configured duration, command timeout, and allowlisted x264 preset in the ffmpeg processor
- [x] 2.2 Distinguish command deadline expiry, parent cancellation, and ordinary ffmpeg exit failures
- [x] 2.3 Retain the actionable tail of bounded processing error messages
- [x] 2.4 Reproduce and fix multi-rendition DASH packaging with optional audio
- [x] 2.5 Add unit and integration coverage for duration policy, long command timeout, error tails, and DASH output

## 3. Durable Recovery

- [x] 3.1 Persist a safe `lease_expired` reason when reconciliation returns an expired processing attempt to the retry queue
- [x] 3.2 Add repository and worker coverage for expired-lease diagnostics and subsequent reclaim

## 4. Prod Deployment and Documentation

- [x] 4.1 Verify Prod Worker remains a mandatory Compose service across deploy, health gate, and rollback paths
- [x] 4.2 Add or update deployment regression tests for first-time Worker startup
- [x] 4.3 Update media, deployment, engineering, and operations documentation for runtime limits and failure diagnostics

## 5. Validation

- [x] 5.1 Run targeted media, configuration, persistence, and deployment tests
- [x] 5.2 Build both Go entry points and run strict OpenSpec validation

## 6. Extended Duration Policy

- [x] 6.1 Raise local, Docker, and Prod maximum media duration to 180 minutes and command timeout to 360 minutes
- [x] 6.2 Update configuration assertions and media/deployment/operations documentation for the extended duration policy
- [x] 6.3 Run targeted tests, build both Go entry points, and validate OpenSpec strictly
