# Gob Specification

This document describes the Gob syntax supported by the current implementation. The authoritative behavior is the golden cases in `test/cases/` and the official transpiler in `lib/gob/transpilation/runner.rb`.

## Files and packages

- Gob source files use the `.gob` extension.
- `require "pkg"` imports a Go package.
- `require "pkg", name` imports it under an alias.
- `require "pkg", _` is a blank import.
- `module Main` declares `package main`; other module names declare that package name (for example `module Ksuid` -> `package ksuid`).
- Top-level `def main` is treated as Go `func main()`.

```ruby
require "fmt"

module Main
  def main
    fmt.Println("hello world")
  end
end
```

## Variables and constants

```ruby
var a = "initial"          # inferred var
var :b, int, 1             # explicit typed var with value
var :c, int                # explicit typed zero value
var buf Array[byte, 16]    # explicit typed zero value, bare name
f = "apple"                # short assignment in function body
```

`var` forms declare Go variables. Plain assignment in a function body maps to Go short declaration where appropriate. Compound assignments such as `+=` are supported where golden cases use them.

Constants use `const :name, value`:

```ruby
const :numJobs, 5
const :s, "hello"
```

At module scope:

- `NAME = value` becomes a Go `const`. `SCREAMING_SNAKE` names are rewritten to `lowerCamel`.
- `name = value` becomes a package-level `var`.
- Adjacent `const` or `var` declarations are grouped into one parenthesised block.

## Functions

```ruby
def plus(a, b) # (int, int) -> int
  return a + b
end

def vals # -> (int, int)
  return 3, 7
end
```

- Function bodies use `def ... end`.
- Go parameter/result types are supplied in inline signature comments.
- Multiple return values are supported.

## Lambdas

```ruby
double = lambda { |value| # (int) -> int
  return value * 2
}
```

Lambdas transpile to Go function literals. Type comments carry parameter and result types. Lambdas may be invoked immediately, for example `go lambda { ... }.()`.

## Control flow and loops

Gob uses Ruby-style blocks with `end`.

```ruby
if err != nil
  fmt.Println(err)
elsif other
  fmt.Println(other)
else
  fmt.Println("ok")
end
```

`if err`, `if err != nil`, and `if (name = expr)` follow Go-style condition/error-check patterns as shown in golden cases. A bare `if err` is read as `if err != nil`.

Loops and control statements:

```ruby
while i < len(s)
  i = i + 1
end

until done
  i = i + 1
end

loop do
  break
end

next
```

- `while` maps to a Go `for` condition loop.
- `until cond` maps to `for !cond`.
- `loop do` maps to an infinite Go `for {}` loop.
- `break` and `next` map to Go `break` and `continue`.
- `unless cond` maps to `if !cond`. `unless err != nil` maps to `if err == nil`.

Iteration forms in golden cases include:

```ruby
3.times do |i| ... end              # counting loop
nums.each do |_, num| ... end       # range over slice/array
kvs.each do |k, v| ... end          # range over map
"go".each do |i, c| ... end         # range over string
```

`defer` schedules cleanup:

```ruby
defer file.Close()
```

## Case, switch, and select

`case` maps to Go `switch` or `select` depending on form.

```ruby
case len(b)                # value switch
when 0
  return nil
else
  return err
end

case v = src.(type)        # type switch
when nil
  return self.scan(nil)
when Array[byte]
  return self.scan(v)
else
  return fmt.Errorf("type %T", v)
end
```

A `case` with no expression and channel `when` clauses becomes a Go `select` (see Concurrency).

## Literals

```ruby
s = "hello"
r = '\n'
b = '['
```

- Double-quoted strings transpile to Go interpreted string literals.
- Single-quoted literals transpile to Go rune/char literals: `'a'` becomes `'a'`, `'\n'` becomes `'\n'`.
- Single quotes are for runes. `gob fmt`/validation reports multi-character single-quoted text and suggests double quotes.

## Collections

```ruby
nums = Array[int].new(2, 3, 4)
m = Hash[string, int].new("foo" => 1, "bar" => 2)
s = make(Array[string], 3)
c = make(Hash[string, int])
```

- `Array[T].new(...)` creates a Go slice literal.
- `Hash[K, V].new(...)` creates a Go map literal.
- `Array[T, N].new(...)` creates a Go fixed-size array literal.
- `make(Array[T], n)` and `make(Hash[K, V])` map to Go `make`.
- `Proc` is the Go type `func()`. `Proc[T, U]` takes params; a final array literal gives return types, e.g. `Proc[int, [bool]]` -> `func(int) bool`, `Proc[int, [bool, error]]` -> `func(int) (bool, error)`.
- `x[]` slices the whole value: `x[:]`. `x[...n]` becomes `x[:n]`.
- `Type.(x)` casts/converts when the receiver is a type-form (`Array[T]`, `Hash[K, V]`, `@@T`, `@@pkg.Type`), e.g. `Array[byte].(x)` -> `([]byte)(x)`, `@@byte.(p)` -> `(*byte)(p)`.
- `x.(T)` is a type assertion when the receiver is a value.
- Common Go builtins/helpers used in cases include `len`, `cap`, `append`, `copy`, `delete`, and `clear`.

## Structs and classes

```ruby
var person = Struct.new(name: string, age: int) do
  def Rename(name) # (string)
    self.name = name
  end

  protected def Display # -> string
    return self.name
  end
end
```

- `Struct.new(...)` declares a Go struct type.
- Struct field tags are written as a comment on the field, either beside it or
  on the line above. Either way the tag stays with the field it was written for,
  including the first and the last field:

  ```ruby
  var article = Struct.new(
    Title: string, # `json:"title"`
    # `json:"views"`
    Views: int,
  )
  ```

  A class field carries its tag the same way:

  ```ruby
  class Article
    attr_accessor :Title, string # `json:"title"`
  end
  ```

- Plain `def` inside the block declares a pointer receiver, because most methods need to modify the receiver.
- `protected def` declares a value receiver. The method gets a copy, so the instance is protected from modification.
- Every receiver is named `self`.
- Struct literals use `person.new(name: "Ann", age: 40)` or positional fields as shown in cases.

`class` declares a named struct type with `attr_accessor` fields:

```ruby
class Args
  attr_accessor :A, int
  attr_accessor :B, int
end
```

Class methods follow the same receiver rule. Instantiate with `ClassName.new(...)`, like struct literals.

`Class.new(Type)` declares a named Go type over an underlying type:

```ruby
var CompressedSet = Class.new(Array[byte])

var KSUID = Class.new(Array[byte, BYTE_LENGTH]) do
  protected def String # () -> string
    return ""
  end
end
```

These become `type CompressedSet []byte` and `type KSUID [byteLength]byte`. Methods in the block attach to the named type.

## Pointers

```ruby
p = @value   # address-of
v = @@p      # dereference
```

`@` maps to Go `&`. `@@` maps to Go `*` for dereference.

- `@pkg.Type.new(Field: v)` becomes `&pkg.Type{Field: v}`.
- `@@pkg.Type` in a type position is the pointer type `*pkg.Type`.
- `@@T.(x)` casts to a pointer type: `(*T)(x)`.

## Errors

Gob follows Go error conventions. Functions return `error` values, callers check them, and helpers such as `errors.New`, `fmt.Errorf`, and `errors.Is` are used as in Go.

```ruby
if (r, e = f(i))
  fmt.Println("failed:", e)
else
  fmt.Println("worked:", r)
end
```

## Concurrency

```ruby
messages = make(Chan[string])
jobs = make(Chan[int], 5)

go lambda { messages.<< "ping" }.()

msg = messages.<<
j, more = jobs.<<
close(jobs)
```

- `go` starts a goroutine.
- `Chan[T]` is the Gob channel type form; direction types use Go-style forms in signatures, such as `<-chan int` and `chan<- int`.
- `make(Chan[T])` or `make(Chan[T], n)` creates an unbuffered or buffered channel.
- `ch.<< value`, `ch.<<(value)`, or `ch ← value` sends (`ch << value` is also supported).
- `ch.<<` or `←ch` receives; `value, ok = ch.<<` checks whether the channel is still open.
- `close(ch)` closes a channel.

`case/when` with channel operations maps to Go `select`:

```ruby
case
when msg = c1.<<
  fmt.Println(msg)
when ch.<<(msg)
  fmt.Println("sent")
when time.After(time.Second).<<
  fmt.Println("timeout")
end
```

## Bitwise operators

```ruby
x = a & b    # AND
x = a | b    # OR
x = a ^ b    # XOR
x = a << 1   # left shift
x = a >> 1   # right shift
x = ~a       # bitwise NOT (Go ^a)
x = a &! b   # bit clear (Go a &^ b)
```

- `~x` is unary bitwise NOT and emits Go `^x`.
- `a &! b` is bit clear and emits Go `a &^ b`.

## Style and validation

- Prefer explicit Gob types and supported forms.
- Run `gob fmt` and `gob vet` on `.gob` files. `gob fmt` formats in
  place, and every command takes a directory as well as a file, so
  `gob vet src packages` checks a whole tree in one pass.
- `gob transpile src -o ./build` mirrors a source tree into `build/`.
- Use `test/cases/` as the reference for supported syntax and expected Go output.
