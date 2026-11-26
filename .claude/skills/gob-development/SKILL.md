---
name: gob-development
description: Use whenever changing Gob source, .gob examples, transpilation rules, validation cops, CLI behavior, or golden tests. Covers the Ruby-to-Go contract, house style, AST inspection, formatting, validation, build/run commands, and the required test loop.
---

# Gob development

## Start here

Read `AGENTS.md` and `DEVELOPMENT.md` before editing. The official compiler is `lib/gob/transpilation/runner.rb`; older implementations may still exist.

Gob is strictly **Ruby-to-Go**. Do not add or reason about Go-to-Ruby conversion.

## Core rules

- Model syntax with `SyntaxTree` nodes and Ruby pattern matching. Do not scrape source with regexes when structure is available.
- Keep transpilation rules declarative: extract captures and compose `SyntaxTree::Go` constructors. Put behavior in helpers, not nested rule spaghetti.
- Read `docs/official-go-spec-v1.27.md`, `refs/go/src/go/ast`, and `lib/syntax_tree/go` before inventing AST shapes.
- Preserve multiline `def` and lambda bodies; inline signature comments carry Go parameter and result types.
- Prefer fixing `.gob` style, validation cops, or goldens before changing `runner.rb`. Change the runner only when the requested behavior truly cannot be expressed by existing Gob syntax, and keep the change minimal.
- Never edit `refs/`.

## House style

Use explicit Gob types and the forms the runner understands:

```ruby
require "fmt"

module Main
  var person = Struct.new(Name: string, Age: int)

  def runPerson # ()
    var :buf, bytes.Buffer
    names = Array[string].new("Go", "Gob")

    names.each do |_, name|
      fmt.Println(name)
    end

    go lambda {
      fmt.Println("async")
    }.()
  end
end
```

Important conventions:

- Use `var :name, Type`, `Array[Type].new(...)`, and `Hash[Key, Value].new(...)`.
- Use `count.times do |i|` for zero-based counting loops.
- Use `@value` for address-of and `@@value` for dereference.
- Use `def self.Method` inside a struct/class block for a Go pointer receiver.
- Put struct tags **before** the field so SyntaxTree attaches them to the `Assoc`:

  ```ruby
  # `json:"name"`
  Name: string,
  ```

- Lambda types come from comments:

  ```ruby
  lambda { |value| # (int) -> bool
    value > 0
  }
  ```

## Validation cops

Gob cops live in `lib/gob/validation/rules/` and run through `gob fmt` and `gob vet`.

To add one:

1. Subclass `Gob::Validation::Rules::Cop`.
2. Implement `rule(node)` with `case/in` patterns.
3. Use `add_offense` and `AutoCorrector` for safe, idempotent fixes.
4. Register the file in `lib/gob/validation/rules.rb` and `rules/config.yml`.
5. Add coverage in `test/validation_rules_test.rb`.

Corrections must converge, preserve comments/signatures, and avoid overlapping rewrites.

## Inspect before changing

Compare the actual Ruby and Go ASTs:

```sh
bundle exec ruby bin/examine-source-and-target-trees path/to/example.gob
bundle exec ruby tools/print-examples.rb
```

`stree match FILE.gob` and `stree json FILE.gob` are also useful for isolated AST experiments.

## Build, format, validate, run

Use the project bundle or `nix develop`:

```sh
bundle exec ruby exe/gob fmt --write path/to/example.gob
bundle exec ruby exe/gob vet path/to/example.gob
bundle exec ruby exe/gob transpile path/to/example.gob -o /tmp/example.go
bundle exec ruby exe/gob run path/to/example.gob
bundle exec ruby exe/gob build path/to/example.gob -o /tmp/example
```

If the native formatter is missing, rebuild it:

```sh
cd ext/goformatter
CGO_ENABLED=1 go build -buildmode=c-shared -buildvcs=false -o libgoformatter.so .
```

Use `.dylib` on macOS. Go fixtures are formatted with `gofmt`, not Gob cops.

## Nix

`flake.nix` provides the supported development environment: Ruby 4.0, Go 1.27, bundled gems, `gopls`, `ast-grep`, and `go-task`.

```sh
nix develop
bin/test
gob vet path/to/example.gob
```

The shell configures `BUNDLE_GEMFILE`, `RUBYLIB`, and `PATH`, so `gob` resolves to this checkout. Prefer these pinned tools over system Ruby/Go. After dependency changes, update `Gemfile.lock` and run `bundix -l` to regenerate `gemset.nix`; the shell hook also does this when the file is writable. The Nix environment includes only the `default` and `development` groups; do not add tree-sitter to Gob's runtime dependencies.

## References

- [`references/nix.md`](references/nix.md) — flake setup, Bundix, native formatter, and troubleshooting.
- [`templates/flake.nix`](templates/flake.nix) — copyable Gob development-shell template with Ruby 4.0, Go 1.27, Bundix, and the FFI platform hashes.
- [`references/examples.md`](references/examples.md) — paired Gob and Go examples covering declarations, structs, collections, functions, pointers, errors, and concurrency.

## Golden tests

Each case has `name.gob`, expected `name.go`, and generated `name.gob.out`.

```sh
bin/test
bin/test test/cases/standard-lib-examples/003_standard_library
bundle exec ruby -Itest test/validation_rules_test.rb
```

`bin/transpilation-check` is the same golden-test entry point.

For fixture modules with `go.mod`, also run:

```sh
cd test/cases/standard-lib-examples/003_standard_library
go test ./...
```

Update a `.go` golden only after the emitted Go is correct and compiles. Do not change goldens merely to silence a real transpiler bug. Restore exploratory `.gob.out` changes when they are not part of the intended patch.

## Done means

- Targeted `bin/test` passes.
- Full `bin/test` passes.
- Validation tests pass and changed `.gob` files vet clean.
- Generated fixture Go passes `go test ./...` where applicable.
- `gofmt` and `git diff --check` are clean.
- Do not commit unless asked.
