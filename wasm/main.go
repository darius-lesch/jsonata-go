//go:build js && wasm

package main

import (
	"encoding/json"
	"fmt"
	"sync"
	"syscall/js"

	"github.com/darius-lesch/jsonata-go/v2"
)

var exprCache sync.Map // string -> *jsonata.Expr

func main() {
	// Expose the evaluation function to the JavaScript global scope
	js.Global().Set("_jsonataGoEval", js.FuncOf(jsEval))

	// Prevent the Go WASM process from exiting
	select {}
}

func jsEval(_ js.Value, args []js.Value) any {
	if len(args) < 2 {
		return jsError("jsonataGoEval requires 2 arguments: expr, jsonData")
	}
	exprStr := args[0].String()
	jsonStr := args[1].String()

	result, err := doEval(exprStr, jsonStr)
	if err != nil {
		return jsError(err.Error())
	}
	return js.ValueOf(result)
}

func doEval(exprStr, jsonStr string) (result string, err error) {
	// Catch any internal jsonata-go panics to prevent WASM module crashes
	defer catchPanic(&err)

	var expr *jsonata.Expr
	if cached, ok := exprCache.Load(exprStr); ok {
		expr = cached.(*jsonata.Expr)
	} else {
		compiled, compileErr := jsonata.Compile(exprStr)
		if compileErr != nil {
			return "", compileErr
		}
		exprCache.Store(exprStr, compiled)
		expr = compiled
	}

	var data interface{}
	if jsonStr != "" {
		if unmarshalErr := json.Unmarshal([]byte(jsonStr), &data); unmarshalErr != nil {
			return "", fmt.Errorf("invalid JSON data: %w", unmarshalErr)
		}
	}

	res, evalErr := expr.Eval(data)
	if evalErr != nil {
		return "", evalErr
	}

	// Map Go nil to a JS empty string (which the frontend can treat as undefined)
	if res == nil {
		return "", nil
	}

	out, marshalErr := json.Marshal(res)
	if marshalErr != nil {
		return "", fmt.Errorf("cannot marshal result: %w", marshalErr)
	}
	return string(out), nil
}

func catchPanic(errp *error) {
	if r := recover(); r != nil {
		switch v := r.(type) {
		case error:
			*errp = fmt.Errorf("internal error: %w", v)
		default:
			*errp = fmt.Errorf("internal error: %v", r)
		}
	}
}

func jsError(msg string) js.Value {
	// Return a native JavaScript Error object
	return js.Global().Get("Error").New(msg)
}
