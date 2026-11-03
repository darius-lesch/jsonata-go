// Copyright 2018 Blues Inc.  All rights reserved.
// Use of this source code is governed by licenses granted by the
// copyright holder including that found in the LICENSE file.

package jsonata

import (
	"math"
	"reflect"
	"sort"

	"github.com/darius-lesch/jsonata-go/v2/jlib"
	"github.com/darius-lesch/jsonata-go/v2/jparse"
	"github.com/darius-lesch/jsonata-go/v2/jtypes"
)

type tuple map[string]reflect.Value

type tupleSequence struct {
	tuples         []tuple
	keepSingletons bool
}

func (ts *tupleSequence) asSequence() *sequence {
	seq := newSequence(len(ts.tuples))
	seq.keepSingletons = ts.keepSingletons
	for _, t := range ts.tuples {
		v := t["@"]
		if v.IsValid() && v.CanInterface() {
			seq.Append(v.Interface())
		} else {
			seq.Append(nil)
		}
	}
	return seq
}

func asTupleSequence(v reflect.Value) (*tupleSequence, bool) {
	if !v.IsValid() {
		return nil, false
	}
	if ts, ok := v.Interface().(*tupleSequence); ok {
		return ts, true
	}
	return nil, false
}

func createFrameFromTuple(env *environment, t tuple) *environment {
	frame := newEnvironment(env, len(t))
	for k, v := range t {
		frame.bind(k, v)
	}
	return frame
}

func evalParent(node *jparse.ParentNode, env *environment) (reflect.Value, error) {
	if node.Slot == nil {
		return undefined, nil
	}
	v := env.lookup(node.Slot.Label)
	if !v.IsValid() {
		return undefined, nil
	}
	return v, nil
}

func evalTupleStep(step *jparse.StepNode, inputSeq *sequence, tupleBindings []tuple, env *environment) ([]tuple, error) {
	if jparse.IsSortNode(step.Expr) {
		sortNode := step.Expr.(*jparse.SortNode)
		if tupleBindings != nil {
			sorted, err := sortTuples(tupleBindings, sortNode.Terms, env)
			if err != nil {
				return nil, err
			}
			tupleBindings = sorted
		} else {
			inVal := reflect.ValueOf(inputSeq.values)
			sortedVal, err := evalSort(sortNode, inVal, env)
			if err != nil {
				return nil, err
			}
			var sortedItems []reflect.Value
			if seq, ok := asSequence(sortedVal); ok {
				sortedItems = make([]reflect.Value, len(seq.values))
				for i, v := range seq.values {
					sortedItems[i] = reflect.ValueOf(v)
				}
			} else if jtypes.IsArray(sortedVal) {
				arr := arrayify(sortedVal)
				sortedItems = make([]reflect.Value, arr.Len())
				for i := 0; i < arr.Len(); i++ {
					sortedItems[i] = arr.Index(i)
				}
			} else if sortedVal.IsValid() {
				sortedItems = []reflect.Value{sortedVal}
			}
			tupleBindings = make([]tuple, len(sortedItems))
			for i := 0; i < len(sortedItems); i++ {
				t := tuple{"@": sortedItems[i]}
				if step.Index != "" {
					t[step.Index] = reflect.ValueOf(float64(i))
				}
				tupleBindings[i] = t
			}
		}
		if len(step.Stages) > 0 {
			var err error
			tupleBindings, err = evalStages(step.Stages, tupleBindings, env)
			if err != nil {
				return nil, err
			}
		}
		return tupleBindings, nil
	}

	if tupleBindings == nil {
		tupleBindings = make([]tuple, inputSeq.Len())
		for i := 0; i < inputSeq.Len(); i++ {
			tupleBindings[i] = tuple{"@": reflect.ValueOf(inputSeq.values[i])}
		}
	}

	var result []tuple
	for ee := 0; ee < len(tupleBindings); ee++ {
		stepEnv := createFrameFromTuple(env, tupleBindings[ee])
		res, err := eval(step.Expr, tupleBindings[ee]["@"], stepEnv)
		if err != nil {
			return nil, err
		}
		if res == undefined || !res.IsValid() {
			continue
		}

		if ts, ok := asTupleSequence(res); ok {
			for _, subT := range ts.tuples {
				merged := make(tuple, len(tupleBindings[ee])+len(subT))
				for k, v := range tupleBindings[ee] {
					merged[k] = v
				}
				for k, v := range subT {
					merged[k] = v
				}
				result = append(result, merged)
			}
			continue
		}

		var resItems []reflect.Value
		if seq, ok := asSequence(res); ok {
			resItems = make([]reflect.Value, len(seq.values))
			for i, val := range seq.values {
				resItems[i] = reflect.ValueOf(val)
			}
		} else if jtypes.IsArray(res) {
			res = arrayify(res)
			resItems = make([]reflect.Value, res.Len())
			for i := 0; i < res.Len(); i++ {
				resItems[i] = res.Index(i)
			}
		} else {
			resItems = []reflect.Value{res}
		}

		for bb := 0; bb < len(resItems); bb++ {
			t := make(tuple, len(tupleBindings[ee])+4)
			for k, v := range tupleBindings[ee] {
				t[k] = v
			}
			if step.Focus != "" {
				t[step.Focus] = resItems[bb]
				t["@"] = tupleBindings[ee]["@"]
			} else {
				t["@"] = resItems[bb]
			}
			if step.Index != "" {
				t[step.Index] = reflect.ValueOf(float64(bb))
			}
			if step.Ancestor != nil {
				t[step.Ancestor.Label] = tupleBindings[ee]["@"]
			}
			result = append(result, t)
		}
	}

	if len(step.Stages) > 0 {
		var err error
		result, err = evalStages(step.Stages, result, env)
		if err != nil {
			return nil, err
		}
	}

	return result, nil
}

func evalStages(stages []jparse.Stage, tuples []tuple, env *environment) ([]tuple, error) {
	result := tuples
	for _, stage := range stages {
		switch stage.Type {
		case jparse.StageFilter:
			var err error
			result, err = evalFilterTuples(stage.Expr, result, env)
			if err != nil {
				return nil, err
			}
		case jparse.StageIndex:
			for ee := 0; ee < len(result); ee++ {
				result[ee][stage.Var] = reflect.ValueOf(float64(ee))
			}
		}
	}
	return result, nil
}

func evalFilterTuples(predicate jparse.Node, tuples []tuple, env *environment) ([]tuple, error) {
	if num, ok := predicate.(*jparse.NumberNode); ok {
		index := int(math.Floor(num.Value))
		if index < 0 {
			index = len(tuples) + index
		}
		if index >= 0 && index < len(tuples) {
			return []tuple{tuples[index]}, nil
		}
		return nil, nil
	}

	var results []tuple
	nTuples := len(tuples)
	for i := 0; i < nTuples; i++ {
		t := tuples[i]
		context := t["@"]
		frame := createFrameFromTuple(env, t)

		res, err := eval(predicate, context, frame)
		if err != nil {
			return nil, err
		}
		if res == undefined || !res.IsValid() {
			continue
		}

		if jtypes.IsNumber(res) {
			res = arrayify(res)
		}

		switch {
		case jtypes.IsArrayOf(res, jtypes.IsNumber):
			for j := 0; j < res.Len(); j++ {
				n, _ := jtypes.AsNumber(res.Index(j))
				idx := int(math.Floor(n))
				if idx < 0 {
					idx = nTuples + idx
				}
				if idx == i {
					results = append(results, t)
				}
			}
		case jlib.Boolean(res):
			results = append(results, t)
		}
	}
	return results, nil
}

func sortTuples(tuples []tuple, terms []jparse.SortTerm, env *environment) ([]tuple, error) {
	if len(tuples) <= 1 {
		return tuples, nil
	}

	type tupleSortInfo struct {
		tuple  tuple
		values []reflect.Value
	}

	info := make([]*tupleSortInfo, len(tuples))
	isNumberTerm := make([]bool, len(terms))
	isStringTerm := make([]bool, len(terms))

	for i, t := range tuples {
		context := t["@"]
		frame := createFrameFromTuple(env, t)
		vals := make([]reflect.Value, len(terms))

		for j, term := range terms {
			v, err := eval(term.Expr, context, frame)
			if err != nil {
				return nil, err
			}
			if v == undefined || !v.IsValid() {
				continue
			}
			switch {
			case jtypes.IsNumber(v):
				if isStringTerm[j] {
					return nil, newEvalError(ErrSortMismatch, term.Expr, nil)
				}
				vals[j] = v
				isNumberTerm[j] = true
			case jtypes.IsString(v):
				if isNumberTerm[j] {
					return nil, newEvalError(ErrSortMismatch, term.Expr, nil)
				}
				vals[j] = v
				isStringTerm[j] = true
			default:
				return nil, newEvalError(ErrNonSortable, term.Expr, nil)
			}
		}
		info[i] = &tupleSortInfo{tuple: t, values: vals}
	}

	sort.SliceStable(info, func(i, j int) bool {
		for t, term := range terms {
			vi := info[i].values[t]
			vj := info[j].values[t]

			switch {
			case (vi == undefined || !vi.IsValid()) && (vj == undefined || !vj.IsValid()):
				continue
			case vi == undefined || !vi.IsValid():
				return false
			case vj == undefined || !vj.IsValid():
				return true
			}

			if eq(vi, vj) {
				continue
			}

			if term.Dir == jparse.SortDescending {
				return lt(vj, vi)
			}
			return lt(vi, vj)
		}
		return false
	})

	result := make([]tuple, len(info))
	for i := range info {
		result[i] = info[i].tuple
	}
	return result, nil
}

func evalObjectTuples(node *jparse.ObjectNode, tuples []tuple, env *environment) (reflect.Value, error) {
	if len(tuples) == 0 {
		return reflect.ValueOf(map[string]any{}), nil
	}

	type groupEntry struct {
		pairIndex int
		tuples    []tuple
	}

	groups := make(map[string]*groupEntry)
	var groupOrder []string

	for _, t := range tuples {
		frame := createFrameFromTuple(env, t)
		for pairIdx, pair := range node.Pairs {
			kVal, err := eval(pair[0], t["@"], frame)
			if err != nil {
				return undefined, err
			}
			if kVal == undefined || !kVal.IsValid() {
				continue
			}
			key, ok := jtypes.AsString(kVal)
			if !ok {
				return undefined, newEvalError(ErrIllegalKey, pair[0], nil)
			}
			entry, exists := groups[key]
			if exists {
				if entry.pairIndex != pairIdx {
					return undefined, newEvalError(ErrDuplicateKey, pair[0], key)
				}
				entry.tuples = append(entry.tuples, t)
			} else {
				entry = &groupEntry{
					pairIndex: pairIdx,
					tuples:    []tuple{t},
				}
				groups[key] = entry
				groupOrder = append(groupOrder, key)
			}
		}
	}

	results := make(map[string]any, len(groups))
	for _, key := range groupOrder {
		entry := groups[key]
		reduced := reduceTupleStream(entry.tuples)
		context := reduced["@"]
		frame := createFrameFromTuple(env, reduced)

		val, err := eval(node.Pairs[entry.pairIndex][1], context, frame)
		if err != nil {
			return undefined, err
		}
		if val.IsValid() && val != undefined && val.CanInterface() {
			if ts, ok := asTupleSequence(val); ok {
				seq := newSequence(len(ts.tuples))
				for _, t := range ts.tuples {
					v := t["@"]
					if v.IsValid() && v.CanInterface() {
						seq.Append(v.Interface())
					}
				}
				val = reflect.ValueOf(seq)
			}
			results[key] = val.Interface()
		}
	}

	return reflect.ValueOf(results), nil
}

func reduceTupleStream(tuples []tuple) tuple {
	if len(tuples) == 0 {
		return tuple{}
	}
	if len(tuples) == 1 {
		t := make(tuple, len(tuples[0]))
		for k, v := range tuples[0] {
			t[k] = v
		}
		return t
	}
	result := make(tuple)
	for _, t := range tuples {
		for k, v := range t {
			if existing, ok := result[k]; !ok {
				result[k] = v
			} else {
				result[k] = appendTupleValues(existing, v)
			}
		}
	}
	return result
}

func appendTupleValues(a, b reflect.Value) reflect.Value {
	var seq *sequence
	if s, ok := asSequence(a); ok {
		seq = s
	} else {
		seq = newSequence(2)
		if a.IsValid() && a.CanInterface() {
			seq.Append(a.Interface())
		}
	}
	if b.IsValid() && b.CanInterface() {
		if sb, ok := asSequence(b); ok {
			for _, val := range sb.values {
				seq.Append(val)
			}
		} else {
			seq.Append(b.Interface())
		}
	}
	return reflect.ValueOf(seq)
}
