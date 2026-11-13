# Repository instructions

These instructions apply to the entire repository.

## Validation

- Run only unit tests automatically. Select specific packages or test names so
  the command does not also execute integration or end-to-end tests.
- Example allowed commands, when relevant to the change:
  - `go test ./settings`
  - `go test ./gui/endpoints`
  - `go test ./settings ./gui/endpoints`
- Compilation performed by `go test` is allowed. Standalone builds such as
  `go build` require an explicit user request.
- Do not automatically run builds, build checks, packaging, integration tests,
  end-to-end tests, native GUI tests, or launch the app for manual verification.
  This includes `go build`, GoReleaser, `TestNativeUI`, `WG_GUI_TEST=1`,
  `WG_NET_TEST=1`, and network namespace tests.
- Run those commands only when the user explicitly requests that validation.
  A request to implement a change does not authorize them.
- If verification requires anything beyond unit tests, leave it unrun and
  mention that briefly in the final response. Do not ask for permission merely
  to complete routine validation.

## Installation

- Do not automatically install, reinstall, update, or deploy the app or its
  dependencies, or copy build outputs into system locations.
- Run installation commands only when the user explicitly requests installation.
  A request to change code, or to explain how to install it, does not authorize
  installation.
- This includes package manager commands, `go install`, and copying binaries,
  desktop launchers, icons, or PolicyKit policies into system directories.
