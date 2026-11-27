# AGENTS.md

OpenVFS is written in Gob — Ruby-shaped source in `src/**/*.gob`
that transpiles to Go. `rake build` transpiles `src` into `build/src` and then runs
`go build`.

The gob language reference lives in `docs/SPEC.md` and `docs/TOUR.md`. **Read them before
writing gob.** Most of this file exists because a handful of Go/Ruby habits are not gob,
and gob fails late and quietly on them.

## Before you commit: `rake build` must be green

`gob vet src` is **not** a sufficient check. It reports "no offenses detected" on source
that fails to transpile *and* on source that transpiles into Go that will not compile.
Only `rake build` tells you the truth.

```sh
rake build
```

## A gob syntax error does not name the file it is in

```
gob: transpile src/main.gob
gob: syntax error, unexpected ',', expecting 'then' or '\n' or ';'
```

The error is **not** necessarily in `src/main.gob`. gob parses a whole package at once,
so it reports the first file of the package and the error can be in any sibling — above,
`src/main.gob`, `src/service.gob`, `src/socket.gob` or `src/syncer.gob`.

Nor is there a line number. Copy each file into a directory of its own and transpile it
alone; only the file that actually contains the error will fail:

```sh
for f in $(find src -name '*.gob'); do
  d=$(mktemp -d)/$(echo "$f" | tr / _); mkdir -p "$d"; cp "$f" "$d/"
  printf '%-40s ' "$f"
  gob transpile "$d/$(basename "$f")" -o "$d/out" 2>&1 | grep 'syntax error' || echo OK
done
```

Then grep that one file for the constructs below.

## Recurring mistakes

These keep coming back. They all look like reasonable Go or Ruby, and none of them are gob.

### `if init; cond` does not exist

gob has no Go-style init statement in a condition. `docs/SPEC.md` documents exactly three
condition forms: `if err`, `if err != nil`, and `if (name = expr)`.

```ruby
if _, ok = handled[e.Path]; ok      # syntax error, unexpected ','
if _, err = os.Stat(src); err == nil # syntax error, unexpected ','
```

For comma-ok, use the parenthesized assignment — it emits the Go idiom:

```ruby
if (_, ok = handled[e.Path])        # -> if _, ok := handled[e.Path]; ok {
```

The binding is scoped to the `if`, so it shadows an outer `ok`. Give each one its own
name (`okOld`, `okNew`) rather than reusing one.

**The parenthesized form keys off the variable's name.** A second value named `err*`
becomes `err != nil`; anything else is used as the condition verbatim:

```ruby
if (_, err = os.Stat(p))            # -> if _, err := os.Stat(p); err != nil {
```

So it cannot express `err == nil`, and `unless (_, err = f())` emits `!err`, which is not
valid Go. When you want the nil case, split it:

```ruby
_, statErr = os.Stat(src)
if statErr == nil
```

A parenthesized condition with `;` inside — `if (_, perr = url.Parse(href); perr == nil)` —
is worse than a syntax error: it *parses*, and silently emits `if nil { ... }` with the
assignment dropped, so you get `undefined: u` from the Go compiler instead. Split those too.

### `if err` only expands for the literal name `err`

A bare identifier condition becomes `!= nil` **only** when it is spelled `err`. Every
other error variable needs the comparison written out:

```ruby
if relErr                 # -> if relErr { ... }  "non-boolean condition in if statement"
if relErr != nil          # correct
```

The same applies inside a compound condition, where even `err` is not expanded:

```ruby
if err && !os.IsNotExist(err)        # mismatched types error and bool
if err != nil && !os.IsNotExist(err) # correct
```

### `append` is a function, not a method

gob passes `.append` through verbatim, so it transpiles cleanly and then fails at
`go build` with `has no field or method append`:

```ruby
actions = actions.append(x)   # []SyncAction has no field or method append
actions = append(actions, x)  # correct
```

`docs/SPEC.md` lists `len`, `cap`, `append`, `copy`, `delete` and `clear` as Go builtins —
all of them are called, never dotted.

### Struct tags are comments

A tag passed as a third positional argument is **silently discarded along with the whole
field**. The struct comes out as `type Response struct{}` and every use of it fails:

```ruby
attr_accessor :Href, string, `xml:"DAV: href"`   # field dropped entirely
attr_accessor :Href, string # `xml:"DAV: href"`  # correct
```

### Do not name a local after an imported package

Ordinary Go shadowing, but easy to miss across a long method:

```ruby
url = self.urlFor(dir)   # shadows net/url; url.Parse later fails
target = self.urlFor(dir)
```

### Keep `require` in sync with what you use

Go rejects unused imports and gob does not prune them, so deleting the last caller of a
package breaks the build. When you remove code, check whether `strings`, `time`, `io/fs`
or a `src/...` package went with it.

## Debugging the generated Go

When a Go compile error makes no sense against the gob source, read what gob actually
emitted — `build/src/` mirrors `src/`, and the error's file/line refer to that tree:

```sh
gob transpile src -o ./build/src   # rake build wipes build/, so re-run this
sed -n '120,170p' build/src/syncer.go
```

An empty `struct{}`, an `if nil {`, or a dropped statement in the generated Go means gob
did not understand the gob source — not that your Go is wrong.

## Verifying a construct in isolation

Rather than guessing which spelling gob accepts, transpile a scratch file and read the Go:

```sh
d=$(mktemp -d); cat > "$d/t.gob" <<'GOB'
require "fmt"

module Main
  def main # ()
    m = make(Hash[string, bool])
    if (_, ok = m["a"])
      fmt.Println(ok)
    end
  end
end
GOB
gob transpile "$d/t.gob" -o "$d/out" && cat "$d/out"
```

`docs/SPEC.md` points at gob's own `test/cases/` golden pairs as the authoritative list of
supported syntax.
