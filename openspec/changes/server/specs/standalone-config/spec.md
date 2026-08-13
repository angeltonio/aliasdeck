# Delta for Standalone Config

## MODIFIED Requirements

### Requirement: config.yaml Strict Schema and Device Identity

The system MUST parse `config.yaml` (`version`, `device`, `source`, `backend`) strictly, and MUST derive a stable device identity from `device.name` (or a generated fallback if omitted). When `source.type: git`, the system MUST require `source.git.url` and MUST reject unknown fields under `source.git`, identical in strictness to `source.type: file`. `source.git.ref` MAY be omitted. When `source.type: server`, the system MUST require `source.url` and MUST reject unknown fields under the server source block, identical in strictness to `source.type: file` and `source.type: git`. The device token MUST NOT be read from `config.yaml`; it is resolved from a separate token file.
(Previously: this requirement covered `file` and `git` strictness only; `server` now carries the identical strictness rule, with its token excluded from this file by design.)

#### Scenario: Valid config.yaml parses
- GIVEN a well-formed `config.yaml` matching §7.3
- WHEN parsed
- THEN it yields a `Device` with name, profiles, source, and backend populated

#### Scenario: Unknown backend value rejected at parse time
- GIVEN `backend: invalid-value`
- WHEN parsed
- THEN parsing fails; only `native` and `chezmoi` are accepted values

#### Scenario: Git source without a ref parses
- GIVEN `source: {type: git, url: <repo>}` with no `ref`
- WHEN parsed
- THEN parsing succeeds and ref resolution defers to the remote's default branch

#### Scenario: Git source missing url rejected
- GIVEN `source: {type: git}` with no `url`
- WHEN parsed
- THEN parsing fails naming the missing required field

#### Scenario: Server source without a url rejected
- GIVEN `source: {type: server}` with no `url`
- WHEN parsed
- THEN parsing fails naming the missing required field

#### Scenario: Server source token absent from config.yaml
- GIVEN `source: {type: server, url: https://aliases.example.com}`
- WHEN parsed
- THEN parsing succeeds and no token field is read from or required in this file
