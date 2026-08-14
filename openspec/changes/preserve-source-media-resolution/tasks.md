## 1. Processing Profile

- [x] 1.1 Register and activate source-resolution processing profile v2 in local, Docker, and Prod configuration
- [x] 1.2 Keep unfinished v1 jobs processable with the single-output recovery behavior

## 2. Single-Output Processor

- [x] 2.1 Replace rendition-loop and DASH generation with one source-resolution baseline MP4
- [x] 2.2 Add H.264/AAC stream-copy and H.264 video-copy/AAC-normalization fast paths
- [x] 2.3 Perform exactly one source-resolution H.264/AAC encode when video normalization is required
- [x] 2.4 Preserve even output dimensions, stable baseline metadata, checksums, and idempotent publication
- [x] 2.5 Hide the Web quality selector when fewer than two selectable sources exist

## 3. Coverage

- [x] 3.1 Update processor unit tests for source-resolution dimensions and profile selection
- [x] 3.2 Replace adaptive-output integration assertions with single MP4 and stream-copy coverage
- [x] 3.3 Verify legacy v1 and active v2 jobs both produce one ready baseline

## 4. Documentation

- [x] 4.1 Update media, deployment, engineering, optimization, and UI/UX documentation to remove selectable-quality and DASH claims for new videos

## 5. Validation

- [x] 5.1 Run targeted media/configuration tests and build API and Worker
- [x] 5.2 Run strict OpenSpec validation
