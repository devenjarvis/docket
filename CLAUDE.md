# Docket

Pull request review built around the reviewer's attention. Go 1.26 backend
(`internal/`), a plain-JS web UI (`internal/web/static`), and a Wails 3 desktop
shell. The README describes the product; `mise.toml` lists the tasks.

- Run the tests: `go test ./...`; vet the desktop build: `go vet -tags desktop ./...`.
- Format Go with `gofmt -w`.

## Brine

Rules the app follows live in `specs/**/*.feature` (valid Gherkin, read
strictly by Brine), each with examples that tests check. **To change behavior,
use `/brine:spec`**: it edits the spec with the person first, then the
`brine:tester` agent writes tests from the spec alone and the `brine:coder`
agent implements. Copy, visual and refactor work needs no spec.

- Mode: solo
- Generate: `mise run spec:gen` (`brine fmt && brine lint && brine gen go`; commit
  `specs/*.gen.go` with the spec, never edit it by hand)
- Spec tests: `mise run spec:test` (`brine run -- go test ./...`: every test, through `brine run`)
- Gate: `mise run spec:gate` (`brine gate`)
- Repo checks: `mise run verify` (fmt/lint/gen checks, the spec tests, the gate, both vets)
- Format test files: `gofmt -w <files>`
- CI gates with the base branch's manifest and config: no (no CI yet)
- Protected (only the tester touches tests; nobody else edits these): `specs/**`,
  `specs/*.gen.go`, `brine.toml`, `brine.seals.json`, sealed test files
  (`internal/**/spec_*_test.go`, every file listed in `brine.seals.json`)

The generated specs are package `specs`, imported as
`github.com/devenjarvis/docket/specs` (`specs.INBOX_001`, ...). Tests use the Go
SDK `github.com/devenjarvis/brine/packages/brine` (`brine.Scenario`,
`brine.ScenarioRows`).

### Layers

- `api`: `internal/server/spec_<feature>_test.go` (package `server_test`), through
  `server.New(...)`'s HTTP handler with `httptest`, the way `server_test.go` builds one.
- `unit`: `internal/<pkg>/spec_<feature>_test.go` (package `<pkg>_test`) in the package
  whose exported API the example exercises, calling it directly.

One file per spec and package: e.g. `internal/review/spec_inbox_test.go` holds every
`@unit` INBOX example.

### The tester may read

Besides the spec, generated modules and other spec tests:

- Contracts: the exported API of `internal/...` packages as `go doc -all ./internal/<pkg>`
  prints it (types, function signatures and their doc comments), and the domain types in
  `internal/model/*.go`.
- Test helpers and fixtures: the existing `*_test.go` files of an external test package
  (`package <pkg>_test`, e.g. the fakes in `internal/review/engine_test.go`,
  `internal/server/server_test.go`), `internal/acp/acptest`, `internal/gh/testdata`,
  `internal/demo/fixtures`.
