// Copyright 2018 Blues Inc.  All rights reserved.
// Use of this source code is governed by licenses granted by the
// copyright holder including that found in the LICENSE file.

package jsonata_test

import (
	"fmt"
	"log"
	"strings"

	jsonata "github.com/darius-lesch/jsonata-go/v2"
)

//
// This example demonstrates how to extend JSONata with
// custom functions.
//

// titlecase converts each word in a string to title case.
func titlecase(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + strings.ToLower(w[1:])
		}
	}
	return strings.Join(words, " ")
}

// exts defines a function named "titlecase" which maps to
// a custom Go function. Any function, from the standard library
// or otherwise, can be used to extend JSONata, as long as it
// returns either one or two arguments (the second argument must
// be an error).
var exts = map[string]jsonata.Extension{
	"titlecase": {
		Func: titlecase,
	},
}

func ExampleExpr_RegisterExts() {

	// Create an expression that uses the titlecase function.
	e := jsonata.MustCompile(`$titlecase("beneath the underdog")`)

	// Register the titlecase function.
	err := e.RegisterExts(exts)
	if err != nil {
		log.Fatal(err)
	}

	// Evaluate.
	res, err := e.Eval(nil)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(res)
	// Output: Beneath The Underdog
}
