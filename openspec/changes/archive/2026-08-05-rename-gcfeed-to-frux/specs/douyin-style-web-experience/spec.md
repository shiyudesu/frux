## RENAMED Requirements

- FROM: `### Requirement: Original GCFeed brand and icon system`
- TO: `### Requirement: Original Frux brand and icon system`

## MODIFIED Requirements

### Requirement: Unified user application shell
The user frontend SHALL render a consistent dark Frux shell across Feed, messages, own profile, public profile, and upload routes. At wide desktop widths the shell SHALL use a 160px navigation rail and a 56px top header, and SHALL retain the existing typed route destinations and authentication redirects.

#### Scenario: Desktop shell renders
- **WHEN** a user opens any user route at a viewport width of at least 1280px
- **THEN** the page renders the Frux brand, fixed 160px side navigation, 56px header, route-aware active navigation, and main content without horizontal overflow

#### Scenario: Existing destinations remain valid
- **WHEN** a user activates Feed, message, upload, profile, login, or public-profile navigation
- **THEN** the application navigates through the existing typed route union and preserves the existing authentication rules

### Requirement: Original Frux brand and icon system
The redesigned frontend SHALL use an original FRUX wordmark, F compact brand mark, and locally owned typed SVG icon registry. It MUST NOT embed Douyin logos, trademarks, proprietary SVG paths, or source-site artwork.

#### Scenario: Brand assets are Frux-owned
- **WHEN** a developer inspects the redesigned navigation and authentication surfaces
- **THEN** all brand and interface icons come from Frux source files or existing user/video content rather than copied Douyin assets

#### Scenario: Icon names are type checked
- **WHEN** a component requests an icon
- **THEN** the icon name is constrained by a TypeScript union and an invalid icon name fails the frontend build

### Requirement: Douyin-style authentication presentation
The login/register route SHALL use a dimmed short-video backdrop and a centered light authentication dialog while preserving Frux account, password, nickname, login, and registration behavior. The interface MUST NOT present nonfunctional QR, phone, or third-party authentication methods.

#### Scenario: User opens authentication
- **WHEN** an unauthenticated user navigates to the authentication route
- **THEN** the centered dialog shows the existing login/register modes and fields against the redesigned backdrop

#### Scenario: Authentication methods remain truthful
- **WHEN** the authentication dialog is rendered
- **THEN** every displayed method is supported by the existing Frux API and no fake QR or phone login control is shown
