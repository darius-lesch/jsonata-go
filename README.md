# JSONata in Go (`jsonata-go`)

**This repository is an updated fork of the original `blues/jsonata-go` library, upgraded to support JSONata 2.2.2 specifications and WebAssembly**

Package `jsonata` is a query and transformation language for JSON data implemented in pure Go. It is a high-performance port of the reference JavaScript implementation, [JSONata](https://jsonata.org/).

This repository maintains **100% feature compliance with `jsonata-js` v2.2.2**, passing **all 1,700+ upstream test cases** with zero skips or failures.

---

## Features

* **Full JSONata 2.2.2 Parity:** Complete support for path navigation, wildcards, descendants, predicates, sorting, grouping, lambdas, closures, transforms, regex, and the 50+ standard library functions.
* **Pure Go:** Zero external runtime dependencies.
* **WebAssembly (WASM) Support:** Ready-to-build WASM entry point for in-browser evaluation and UI playgrounds with panic recovery and AST caching.
* **Included Utilities:**
* **`jsonata-server`**: A locally hosted JSONata Exerciser web application.
* **`jsonata-test`**: CLI runner to evaluate the engine directly against the official `jsonata-js` test suite.

---

## Installation

Requires **Go 1.22+**.

```bash
go get github.com/darius-lesch/jsonata-go/v2

```
---

## Quick Start

```go
package main

import (
	jsonv1 "encoding/json"
	jsonv2 "encoding/json/v2"
	"fmt"
	"log"

	jsonata "github.com/darius-lesch/jsonata-go/v2"
)

const jsonString = `
{
    "orders": [
        {"price": 10, "quantity": 3},
        {"price": 0.5, "quantity": 10},
        {"price": 100, "quantity": 1}
    ]
}
`

func main() {
	var data interface{}

	// Decode JSON
	if err := jsonv2.Unmarshal([]byte(jsonString), &data, jsonv1.DefaultOptionsV1()); err != nil {
		log.Fatal(err)
	}

	// Compile JSONata expression
	expr, err := jsonata.Compile("$sum(orders.(price * quantity))")
	if err != nil {
		log.Fatal(err)
	}

	// Evaluate against data payload
	res, err := expr.Eval(data)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(res)
	// Output: 135
}

```

---

## WebAssembly (WASM) Support

`jsonata-go` includes a WebAssembly entry point inside `./wasm/` designed for client-side browser evaluation.

### Building the WASM Binary

```bash
GOOS=js GOARCH=wasm go build -ldflags="-s -w" -trimpath -o jsonata.wasm ./wasm/

```

### Browser Usage

The compiled WASM module exposes the `_jsonataGoEval` global function on `window`:

```javascript
// _jsonataGoEval(expressionString, jsonDataString) -> string | Error
try {
  const resultJson = window._jsonataGoEval(
    "$sum(orders.(price * quantity))",
    JSON.stringify(payload)
  );
  console.log(JSON.parse(resultJson)); // 135
} catch (err) {
  console.error("Evaluation Error:", err.message);
}

```

---

## Included Tools & Utilities

### 1. JSONata Server (`jsonata-server`)

A locally hosted version of the [JSONata Exerciser](http://try.jsonata.org/) web UI for interactively testing expressions against live JSON payloads.

#### Installation & Running

```bash
go install github.com/darius-lesch/jsonata-go/v2/jsonata-server@latest
jsonata-server -port=8080

```

Navigate to `http://localhost:8080` in your browser.

---

### 2. Upstream Test Suite Runner (`jsonata-test`)

A CLI utility used to run `jsonata-go` directly against the official [`jsonata-js` test-suite](https://www.google.com/search?q=https://github.com/jsonata-js/jsonata/tree/master/test/test-suite)[cite: 16].

#### Installation & Execution

1. **Install the CLI runner:**
```bash
go install github.com/darius-lesch/jsonata-go/v2/jsonata-test@latest

```

2. **Clone the official `jsonata-js` repository:**
```bash
git clone https://github.com/jsonata-js/jsonata.git
cd jsonata
git checkout v2.2.2

```

3. **Execute the test runner against the suite:**
```bash
jsonata-test ./test/test-suite

```

---

## Contributing

Contributions, bug reports, and pull requests are welcome! Prior to submitting a PR, please ensure all unit tests pass and code is formatted cleanly:

```bash
go test ./...
go fmt ./...

```
