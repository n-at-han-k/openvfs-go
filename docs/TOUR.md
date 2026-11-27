# Language Tour

Gob is Ruby-like syntax that transpiles to Go. You write `.gob` files; Gob emits Go source you can format, build, and run with the Go toolchain through the `gob` CLI.

For runnable, tested examples, see `test/cases/` in the repository.

## Hello, Gob

```ruby
require "fmt"

module Main
  def main
    fmt.Println("hello world")
  end
end
```

`require` becomes a Go import. `require "pkg", name` aliases it, and `require "pkg", _` is a blank import. `module Main` produces `package main`, and `main` is treated specially as Go's entry point.

## Variables

```ruby
module Main
  def main
    var a = "initial"
    var :b, int, 1
    var :c, int, 2
    var d = true
    var :e, int

    f = "apple"
  end
end
```

Use `var name = value` when Go can infer the type, `var :name, Type` for an explicit zero value, and `var :name, Type, value` for an explicit typed value. `var name Type` works too. Short assignment `name = value` becomes `:=` in function bodies. Constants use `const :name, value`.

At module scope, `NAME = value` becomes a Go `const` renamed to `lowerCamel`, and `name = value` becomes a package-level `var`.

## Control flow

```ruby
if err != nil
  fmt.Println(err)
elsif other
  fmt.Println(other)
else
  fmt.Println("ok")
end

while i < 10
  i = i + 1
end

until done
  i = i + 1
end

loop do
  break
end
```

`if`, `elsif`, `else`, `while`, `loop do`, `break`, `next`, and `defer` map to their Go equivalents. `unless cond` becomes `if !cond`, and `until cond` becomes `for !cond`. `case/when` maps to Go `switch`, type switch, or `select` depending on form.

## Functions and multiple returns

```ruby
module Main
  def plus(a, b) # (int, int) -> int
    return a + b
  end

  def vals # -> (int, int)
    return 3, 7
  end

  def main
    a, b = vals()
    _, c = vals()
  end
end
```

Inline signature comments carry Go parameter and result types. Multiple return values are written and assigned in Go style.

## Collections and iteration

```ruby
require "fmt"

module Main
  def main
    nums = Array[int].new(2, 3, 4)
    m = Hash[string, string].new("a" => "apple", "b" => "banana")

    nums.each do |_, num|
      fmt.Println(num)
    end

    m.each do |k, v|
      fmt.Printf("%s -> %s\n", k, v)
    end

    3.times do |i|
      fmt.Println(i)
    end
  end
end
```

`Array[Type].new(...)` and `Hash[Key, Value].new(...)` create Go slices and maps. `each` over arrays, maps, and strings maps to Go `range`. `count.times do |i|` gives a zero-based counting loop.

## Structs, pointers, and methods

```ruby
module Main
  var person = Struct.new(name: string, age: int) do
    def Birthday # ()
      self.age = self.age + 1
    end

    protected def Label # () -> string
      return self.name
    end
  end

  def main
    p = person.new(name: "Ann", age: 40)
    sp = @p
    sp.Birthday()
    fmt.Println(sp.Label())
  end
end
```

`Struct.new(...)` declares a Go struct type. `class` with `attr_accessor` is another struct form used in golden cases. `Class.new(Type)` declares a named type over an underlying type, such as `type KSUID [byteLength]byte`. Struct field tags can be written as comments beside or before fields.

`@value` takes an address; `@@value` dereferences.

Every receiver is named `self`. A plain `def` inside a struct or class block gets a pointer receiver, because most methods need to modify the receiver. `protected def` gets a value receiver instead, so the method works on a copy.

## Errors

```ruby
require "errors"
require "fmt"

module Main
  def f(arg) # (int) -> (int, error)
    if arg == 42
      return -1, errors.New("can't work with 42")
    end

    return arg + 3, nil
  end

  def main
    if (r, e = f(7))
      fmt.Println("failed:", e)
    else
      fmt.Println("worked:", r)
    end
  end
end
```

Gob uses Go's error-as-value style. Errors are returned, checked, wrapped, and tested with the standard Go patterns.

## Closures

```ruby
module Main
  def intSeq # -> func() int
    i = 0
    return lambda { || # () -> int
      i = i + 1
      return i
    }
  end
end
```

Lambdas become Go function literals. Lambda parameter and result types are carried by inline comments.

## Concurrency

```ruby
require "fmt"
require "time"

module Main
  def main
    messages = make(Chan[string])

    go lambda {
      messages.<< "ping"
    }.()

    msg = messages.<<
    fmt.Println(msg)
  end
end
```

`go` starts a goroutine. `Chan[T]`/`make(Chan[T])` create channels; `make(Chan[T], n)` creates a buffered channel. `ch.<< value` or `ch.<<(value)` sends (`ch << value` also works), `ch.<<` receives, `value, ok = ch.<<` tests open status, and `close(ch)` closes a channel. `case/when` over channel operations maps to Go `select`.

## Next steps

- Read `README.md` and `DEVELOPMENT.md` for setup and workflow.
- Browse `test/cases/` for the golden Gob/Go pairs that define supported behavior.
- Use `gob fmt`, `gob vet`, `gob transpile`, `gob build`, and `gob run`.
