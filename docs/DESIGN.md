# Gob Design

## Purpose

Gob is a Ruby-to-Go transpiler. You write Ruby-shaped `.gob` source; Gob emits Go source. The project is strictly one-way: no Go-to-Ruby conversion.

## Pipeline

Gob source moves through a small pipeline:

1. **Parse** - Ruby source is parsed with `SyntaxTree`.
2. **Validate** - Gob cops check source conventions and safe rewrites.
3. **Transpile** - pattern-matching rules map Ruby/SyntaxTree forms to Go AST nodes.
4. **Emit/format** - Go AST is rendered and formatted as Go source.

The official transpilation implementation is `lib/gob/transpilation/runner.rb`.

## Design goals

- **Go output first.** Gob syntax exists to produce clear, idiomatic Go.
- **Minimal surface.** Prefer small, explicit constructs over many Ruby idioms.
- **Structural matching.** Use SyntaxTree nodes and Ruby pattern matching, not text scraping.
- **Declarative rules.** Transpilation rules should extract captures and compose Go constructors; behavior belongs in helpers.
- **Golden tests as spec.** `test/cases/` contains `.gob` inputs with expected `.go` output.

## Ruby-shaped, Go-semantic

Gob borrows Ruby syntax where it maps cleanly to Go:

- `require "pkg"` maps to Go imports.
- `module` maps to a Go package declaration.
- `def` declares Go functions or methods; `class`/`Struct.new` declare Go struct types; `Class.new` declares a named type over an underlying type.
- Inside a struct or class block, a plain `def` gets a pointer receiver and `protected def` gets a value receiver. Every receiver is named `self`.
- `Array[T].new(...)` and `Hash[K, V].new(...)` map to Go slices/maps; fixed arrays use `Array[T, N].new(...)`.
- `while`, `until`, `unless`, `loop do`, `break`, `next`, and `defer` map to Go control-flow constructs.
- `case/when` maps to Go `switch`, type switch, or `select` depending on form.
- `lambda { ... }` maps to Go function literals.
- `go`, channels, `@` address-of, and `@@` dereference express Go runtime/concurrency/pointer concepts.

Type information that Ruby does not carry natively is supplied by explicit Gob forms and inline signature comments, for example `var :buf, bytes.Buffer` or `def add(a, b) # (int, int) -> int`.

## Validation

`gob fmt` and `gob vet` run Gob validation cops. Cops live in `lib/gob/validation/rules/` and should provide safe, idempotent auto-corrections where possible. Formatting and validation are preferred over adding ad-hoc transpiler behavior.

## Testing

Golden cases live under `test/cases/`. A case typically has:

- `name.gob` - Gob input
- `name.go` - expected Go output
- `name.gob.out` - generated output

Run the goldens with `bin/transpilation-check` (optionally scoped to a path or file).

The test loop is: make the emitted Go correct, compile/verify it, then update goldens only when the output is right.