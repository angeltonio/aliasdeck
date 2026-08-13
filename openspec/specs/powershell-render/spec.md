# PowerShell Render Specification

## Purpose

Defines the PowerShell renderer's function-wrapped, compile-at-call-time output form, its escaping and double-`@args` forwarding guarantees, and the real-`pwsh` containment proof that backs them (PROJECT.md §6.3, §6.4).

## Requirements

### Requirement: Function-Wrapped, Compile-at-Call-Time Rendering

The renderer MUST emit each PowerShell alias as a function that stores the command in a single-quoted string variable and compiles it with `[scriptblock]::Create` only inside the function body — never as raw code directly in the function block — so the generated file executes nothing when dot-sourced.

#### Scenario: Injection payload contained at source time
- GIVEN an alias command containing `}`, `;`, `$(...)`, or a single quote
- WHEN the rendered file is dot-sourced in real `pwsh`
- THEN nothing executes; only the function definition is loaded

#### Scenario: Missing pwsh fails loudly under the required-shells flag
- GIVEN `ALIASDECK_REQUIRE_SHELLS=1` and no `pwsh` binary available
- WHEN the containment integration test runs
- THEN it fails rather than skipping

### Requirement: Single-Quote Escaping

The renderer MUST wrap the command in a single-quoted string and double every embedded single quote. It MUST NOT use backslash escaping.

#### Scenario: Embedded single quote doubled
- GIVEN a command containing a single quote
- WHEN rendered
- THEN every embedded quote appears doubled inside the single-quoted string and no backslash escape is emitted

### Requirement: Arguments Forwarded via @args Twice

The renderer MUST splat `@args` both inside the compiled command string and at the scriptblock invocation. Splatting at only one of the two positions MUST be treated as a defect, not an equivalent form.

#### Scenario: Argument with a space arrives intact
- GIVEN an alias wrapping a command like `git checkout`
- WHEN the generated function is called with an argument containing a space
- THEN that argument arrives at the aliased command as one argument

#### Scenario: Single-@args form drops arguments
- GIVEN a candidate rendering that splats `@args` only at the invocation
- WHEN tested against the golden file and the `pwsh` integration test
- THEN both catch the dropped arguments as a regression

### Requirement: Deterministic, Cross-Platform Byte-Identical Output

Rendering the same `ResolvedConfig` MUST produce byte-identical output regardless of host OS, with LF line endings and no embedded timestamp.

#### Scenario: Same input, same bytes across platforms
- GIVEN the same resolved config rendered on Windows, macOS and Linux
- WHEN the outputs are compared
- THEN they are byte-identical
