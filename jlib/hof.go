// Copyright 2018 Blues Inc.  All rights reserved.
// Use of this source code is governed by licenses granted by the
// copyright holder including that found in the LICENSE file.

package jlib

import (
	"fmt"
	"reflect"

	"github.com/darius-lesch/jsonata-go/v2/jtypes"
)

// Map (golint)
func Map(v reflect.Value, f jtypes.Callable) (any, error) {

	v = forceArray(jtypes.Resolve(v))

	var results []any

	argc := clamp(f.ParamCount(), 1, 3)

	for i := 0; i < arrayLen(v); i++ {

		argv := []reflect.Value{v.Index(i), reflect.ValueOf(i), v}

		res, err := f.Call(argv[:argc])
		if err != nil {
			return nil, err
		}
		if res.IsValid() && res.CanInterface() {
			results = append(results, res.Interface())
		}
	}

	switch len(results) {
	case 0:
		return nil, jtypes.ErrUndefined
	case 1:
		return results[0], nil
	default:
		return results, nil
	}
}

// Filter (golint)
func Filter(v reflect.Value, f jtypes.Callable) (any, error) {

	v = forceArray(jtypes.Resolve(v))

	var results []any

	argc := clamp(f.ParamCount(), 1, 3)

	for i := 0; i < arrayLen(v); i++ {

		item := v.Index(i)
		argv := []reflect.Value{item, reflect.ValueOf(i), v}

		res, err := f.Call(argv[:argc])
		if err != nil {
			return nil, err
		}
		if Boolean(res) && item.IsValid() && item.CanInterface() {
			results = append(results, item.Interface())
		}
	}

	switch len(results) {
	case 0:
		return nil, jtypes.ErrUndefined
	case 1:
		return results[0], nil
	default:
		return results, nil
	}
}

// Reduce (golint)
func Reduce(v reflect.Value, f jtypes.Callable, init jtypes.OptionalValue) (any, error) {

	v = forceArray(jtypes.Resolve(v))

	var res reflect.Value

	if f.ParamCount() < 2 {
		return nil, fmt.Errorf("second argument of function \"reduce\" must be a function that takes two arguments")
	}

	argc := clamp(f.ParamCount(), 2, 4)

	i := 0
	switch {
	case init.IsSet():
		res = jtypes.Resolve(init.Value)
	case arrayLen(v) > 0:
		res = v.Index(0)
		i = 1
	}

	var err error
	for ; i < arrayLen(v); i++ {
		argv := []reflect.Value{res, v.Index(i), reflect.ValueOf(i), v}
		res, err = f.Call(argv[:argc])
		if err != nil {
			return nil, err
		}
	}

	if !res.IsValid() || !res.CanInterface() {
		return nil, jtypes.ErrUndefined
	}

	return res.Interface(), nil
}

// Single returns the one and only one value in the array parameter that satisfy
// the function predicate (i.e. function returns Boolean true when passed the
// value). Returns an error if the number of matching values is not exactly
// one.
// https://docs.jsonata.org/higher-order-functions#single
func Single(v reflect.Value, f jtypes.OptionalCallable) (any, error) {
	v = forceArray(jtypes.Resolve(v))

	if !f.IsSet() {
		if arrayLen(v) != 1 {
			return nil, fmt.Errorf("number of matching values returned by single() must be 1, got: %d", arrayLen(v))
		}
		item := v.Index(0)
		if !item.IsValid() || !item.CanInterface() {
			return nil, jtypes.ErrUndefined
		}
		return item.Interface(), nil
	}

	argc := clamp(f.Callable.ParamCount(), 1, 3)
	var results []any
	for i := 0; i < arrayLen(v); i++ {
		item := v.Index(i)
		argv := []reflect.Value{item, reflect.ValueOf(i), v}
		res, err := f.Callable.Call(argv[:argc])
		if err != nil {
			return nil, err
		}
		if Boolean(res) && item.IsValid() && item.CanInterface() {
			results = append(results, item.Interface())
		}
	}

	if len(results) != 1 {
		return nil, fmt.Errorf("number of matching values returned by single() must be 1, got: %d", len(results))
	}

	return results[0], nil
}

func clamp(n, minVal, maxVal int) int {
	return min(max(n, minVal), maxVal)
}
