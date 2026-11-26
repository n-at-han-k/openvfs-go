# Gob and Go examples

These pairs show canonical Gob source and the Go shape it produces. Formatting may differ, but the AST and behavior should match.

## Hello

```ruby
require "fmt"

module Main
  def main
    fmt.Println("Hello, gopher!")
  end
end
```

```go
package main

import "fmt"

func main() {
	fmt.Println("Hello, gopher!")
}
```

## Constants, variables, and zero values

```ruby
module Main
  STATUS_ACTIVE = "active"
  packageLevel = "global"

  def main
    var :count, int
    var :name, string, "Nathan"
    const :ready, true

    fmt.Println(count, name, ready, STATUS_ACTIVE, packageLevel)
  end
end
```

```go
package main

import "fmt"

const statusActive = "active"

var packageLevel = "global"

func main() {
	var count int
	var name string = "Nathan"
	const ready = true

	fmt.Println(count, name, ready, statusActive, packageLevel)
}
```

## Structs, tags, and pointer receivers

```ruby
module Main
  var user = Struct.new(
    # `json:"name"`
    Name: string,
    Age: int
  ) do
    def self.UnmarshalText(text) # ([]byte) -> error
      return nil
    end
  end

  def run
    value = user.new(Name: "Nathan", Age: 30)
    fmt.Println(value.Name)
  end
end
```

```go
package main

import "fmt"

type user struct {
	Name string `json:"name"`
	Age  int
}

func (self *user) UnmarshalText(text []byte) error {
	return nil
}

func run() {
	value := user{Name: "Nathan", Age: 30}
	fmt.Println(value.Name)
}
```

## Collections and counting loops

```ruby
module Main
  def run
    names = Array[string].new("Go", "Gob")
    scores = Hash[string, int].new("Go" => 1, "Gob" => 2)

    3.times do |i|
      fmt.Println(i, names[i], scores["Go"])
    end
  end
end
```

```go
package main

import "fmt"

func run() {
	names := []string{"Go", "Gob"}
	scores := map[string]int{"Go": 1, "Gob": 2}

	for i := range 3 {
		fmt.Println(i, names[i], scores["Go"])
	}
}
```

## Functions and typed lambdas

```ruby
module Main
  def add(left, right) # (int, int) -> int
    return left + right
  end

  def run
    double = lambda { |value| # (int) -> int
      return value * 2
    }

    fmt.Println(add(2, 3), double(4))
  end
end
```

```go
package main

import "fmt"

func add(left int, right int) int {
	return left + right
}

func run() {
	double := func(value int) int {
		return value * 2
	}

	fmt.Println(add(2, 3), double(4))
}
```

## Pointers and error checks

```ruby
require "bytes"

module Main
  def run
    var :buf, bytes.Buffer

    if (_, err = buf.Write(Array[byte].new("hello")))
      fmt.Println("write failed:", err)
    end

    fmt.Println(string(buf.Bytes()))
  end
end
```

```go
package main

import (
	"bytes"
	"fmt"
)

func run() {
	var buf bytes.Buffer

	if _, err := buf.Write([]byte("hello")); err != nil {
		fmt.Println("write failed:", err)
	}

	fmt.Println(string(buf.Bytes()))
}
```

## Goroutines and channels

```ruby
require "fmt"

module Main
  def run
    messages = make(Chan[string])

    go lambda {
      messages << "ready"
    }.()

    message = ~messages
    fmt.Println(message)
  end
end
```

```go
package main

import "fmt"

func run() {
	messages := make(chan string)

	go func() {
		messages <- "ready"
	}()

	message := <-messages
	fmt.Println(message)
}
```

## Select with `case` / `when`

```ruby
require "fmt"
require "time"

module Main
  def run
    fast = make(Chan[string])
    slow = make(Chan[string])

    go lambda { fast << "fast" }.()
    go lambda { slow << "slow" }.()

    case
    when message = ~fast
      fmt.Println(message)
    when message = ~slow
      fmt.Println(message)
    when ~time.After(50 * time.Millisecond)
      fmt.Println("timeout")
    end
  end
end
```

```go
package main

import (
	"fmt"
	"time"
)

func run() {
	fast := make(chan string)
	slow := make(chan string)

	go func() { fast <- "fast" }()
	go func() { slow <- "slow" }()

	select {
	case message := <-fast:
		fmt.Println(message)
	case message := <-slow:
		fmt.Println(message)
	case <-time.After(50 * time.Millisecond):
		fmt.Println("timeout")
	}
}
```
