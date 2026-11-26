# Go → Gob conversion notes

Every `.go` file under `src/` and `packages/` (59 files, 8862 lines) now has a
`.gob` sibling (10056 lines). The `.go` files are left in place as the expected
transpiler output, the way `gob`'s own `test/cases` pair `name.gob` with
`name.go`. `refs/` is C++ reference material and was not touched.

## Conventions used

Taken from `gob`'s house style, the validation cops and `lib/gob/transpilation/runner.rb`.

| Go | Gob |
| --- | --- |
| `package journal` | `module Journal` |
| `import "x"` / `import y "x"` | `require "x"` / `require "x", y` |
| plain data struct | `var Name = Struct.new(Field: Type, ...)` |
| struct with methods | `class Name` + `attr_accessor :field, Type` |
| unexported struct with methods | `var name = Struct.new(...) do ... end` (Ruby class names must be constants) |
| `type X uint8` | `class X < uint8` |
| `type X interface { ... }` | `var X = Interface.new do def M # sig end end` |
| `func (r *T) M()` / `func (r T) M()` | `def self.M` / `def M`, receiver spelled `self` |
| unexported `const x = v` | `X_SCREAMING_SNAKE = v` (the runner camel-cases it) |
| exported or already-SCREAMING const | `const :Name, v` |
| package `var` | `var name = v` or `var :Name, Type, v` |
| `*T` in type position | `@@T` |
| `&x` / `*x` | `@x` / `@@x` |
| `[]T`, `map[K]V`, `chan T`, `[N]T`, `func()` | `Array[T]`, `Hash[K, V]`, `Chan[T]`, `Array[T, N]`, `Proc` |
| `x[a:b]`, `x[:b]`, `x[a:]`, `x[:]` | `x[a...b]`, `x[...b]`, `x[a...]`, `x[]` |
| `f(args...)` | `f(*args)` |
| `for cond` / `for {}` / `for i := range n` | `while cond` / `loop do` / `n.times do |i|` |
| `for k, v := range c` | `c.each do |k, v|` |
| C-style `for i := 0; i < n; i++` | `i = 0` + `while` + explicit `i = i + 1` |
| `if err != nil` / `if err == nil` | `if err` / `if err == nil` |
| `if a, err := f(); err != nil` | `if (a, err = f())` |
| `switch tag { case ... }` | `case tag ... when ... end` |
| tagless `switch { case cond: }` | `if` / `elsif` / `else` |
| `switch x := v.(type)` | `case x = v.(type) ... when @@T` |
| `select { case ... }` | `case` (no value) + `when ~ch` / `when ch << v` |
| `go f(x)` | `go lambda { f(x) }.()` |
| `x += y`, `x++` | `x = x + y`, `x = x + 1` |
| `{ ... }` bare block | `begin ... end` |

Go types inside signature comments are written as literal Go (`*Journal`,
`[]byte`, `...any`). When a parameter type contains parentheses the outer parens
are dropped — `# string, func(Event) -> *Syncer` — because the signature regex
cannot see past a nested `)`; a void function in that form ends with `-> ()`.

## Rewrites forced by the language

- **Methods are declared inside their type's block.** Go spreads `*Journal`
  methods over ten files; each file reopens `class Journal`, which is correct
  Ruby but makes the runner emit a duplicate empty `type Journal struct{}` per
  file until it learns to merge reopened declarations.
- **Immediately-invoked closures** are rejected by the `InvokedLambda` cop, so
  `syncer.go`'s `serverModified := func() bool {...}()` became the method
  `computeServerModified`.
- **Anonymous structs** (`struct{ name, ddl string }`, the json/msgpack payload
  structs, `struct{ Scan(...) }`) have no spelling, so each became a named
  package-level type: `columnPatch`, `registrationData`, `placeHolderData`,
  `ignoreAppsConfig`, `sendArguments`, `sendMessage`, `hydrateResult`.
- **Function-local `type` declarations** were hoisted to package scope.
- **`struct{}` as a set value / channel element** became `bool`
  (`watcher.watched`, `forbiddenDeletes`, `done`, `timerDone`, `aborted`).
- **Embedded fields** (`fs.Inode`, `*fs.LoopbackFile`, `DownloadInfo`,
  `*HydrationJob`) became named fields; field promotion is lost.
- **Struct tags only attach inside `Struct.new(...)`**, so tagged structs use
  that form and commented structs use `class` (a comment before a field in
  `Struct.new` is read as the tag).
- **Ruby keywords** forced two renames: `end` → `end_`/`finish`, `in` → `in_`.
- `_ uint32` padding appears twice in one struct; the second is `__` because a
  Ruby hash cannot repeat a key.
- **Not ported**, carried as source in comments: the `armFits` compile-time
  union guards in `cfapi_union.gob` and `var _ Vfs = (*VfsCfApi)(nil)` in
  `vfs_cfapi.gob` — both need a type alias / blank package-level assignment.

## Gaps worth fixing in gob

Ordered by how much output they break.

1. **`<<` is always a channel send.** `Statements` maps every `Binary[:<<]` to
   `SendStmt`, so a left shift silently becomes a send. Literal shifts were
   folded (as the `ArrayLength` cop prescribes: `1 << 30 - 2` → `1073741822`,
   the iota/bitflag constants → their values), but dynamic shifts cannot be
   (`1 << uint(i)`, `uint64(k[i]) << 8`, `uint64(hi) << 32`).
2. **Parenthesised sub-expressions are dropped.** There is no
   `Paren → ParenExpr` rule, so `x & (A | B) != 0` and `c + (x << y)` lose the
   grouping the precedence difference between Ruby and Go needs.
3. **No conversion to a pointer type.** `(*byte)(unsafe.Pointer(p))` and
   `(*cfCallbackParametersFetchData)(...)` have no spelling; those call sites
   pass the raw pointer with a comment. Eight occurrences, all in the
   Windows/cgo-adjacent code plus `statAttr`.
4. **No `package` other than `main`.** `Package(name: "main", ...)` is
   hardcoded and only `module Main` is hoisted, so `module Journal` etc. are
   currently dropped entirely.
5. **No interface, type-alias or named-basic-type declarations.**
   `Interface.new do ... end` and `class X < uint8` are what this port writes;
   both need runner rules (today they emit an empty struct).
6. **Typed constants.** `const :X, v` emits an untyped `const X = v`. That
   happens to be compatible for every enum here, but `const STATUS_SUCCESS int32 = 0`
   loses its type.
7. **Func types carry no signature.** `Proc` is `func()` only, so every
   function-typed struct field (the whole `Platform` table, `Scanner.processFile`,
   `Vfs` callbacks) lost its parameters; the Go signature is kept in a comment
   above the field.
8. **Unary `^` collides with `~`.** Go's bitwise NOT is Ruby's `~`, which the
   runner reads as a channel receive (`RemotePermissions.UnsetPermission`).
9. **Escaped and single-character runes.** `'\n'` is two characters in Ruby, and
   `StringLiteral` always emits double quotes, so `' '`, `'\\'`, `':'` and
   `'\n'` transpile to Go strings instead of runes. `CharLiteral` exists in the
   runner but is unused.
10. **Minor:** directional channels (`chan<- error`) lose their direction;
    `//go:build` is written `#go:build` and transpiles to the right text but
    lands after the `package` clause, where Go ignores it; generics appear only
    as `atomic.Pointer[VFSFuseContext]`, which the `IndexExpr` rule already
    handles.
