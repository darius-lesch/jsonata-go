// Copyright 2018 Blues Inc.  All rights reserved.
// Use of this source code is governed by licenses granted by the
// copyright holder including that found in the LICENSE file.

package jsonata_test

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

func ExampleExpr_Eval() {

	var data interface{}

	// Decode JSON.
	err := jsonv2.Unmarshal([]byte(jsonString), &data, jsonv1.DefaultOptionsV1())
	if err != nil {
		log.Fatal(err)
	}

	// Create expression.
	e := jsonata.MustCompile("$sum(orders.(price*quantity))")

	// Evaluate.
	res, err := e.Eval(data)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(res)
	// Output: 135
}
