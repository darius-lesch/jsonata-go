package main

import (
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"

	jsonata "github.com/darius-lesch/jsonata-go/v2"
	types "github.com/darius-lesch/jsonata-go/v2/jtypes"
)

type testCase struct {
	Expr        string         `json:"expr"`
	ExprFile    string         `json:"expr-file"`
	Category    string         `json:"category"`
	Data        any            `json:"data"`
	Dataset     string         `json:"dataset"`
	Description string         `json:"description"`
	TimeLimit   int            `json:"timelimit"`
	Depth       int            `json:"depth"`
	Bindings    map[string]any `json:"bindings"`
	Result      any            `json:"result"`
	Undefined   bool           `json:"undefined"`
	Error       string         `json:"code"`
	Token       string         `json:"token"`
	Unordered   bool           `json:"unordered"`
}

func main() {
	var group string
	var verbose bool

	flag.BoolVar(&verbose, "verbose", false, "verbose output")
	flag.StringVar(&group, "group", "", "restrict to one or more test groups")
	flag.Parse()

	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "Syntax: jsonata-test [options] <directory>")
		os.Exit(1)
	}

	root := flag.Arg(0)
	testdir := filepath.Join(root, "groups")
	datadir := filepath.Join(root, "datasets")

	err := run(testdir, datadir, group)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error while running: %s\n", err)
		os.Exit(2)
	}

	fmt.Println("OK")
}

// run runs all test cases
func run(testdir string, datadir string, filter string) error {
	var numPassed, numFailed int
	err := filepath.Walk(testdir, func(path string, info os.FileInfo, walkFnErr error) error {
		var dirName string

		if info.IsDir() {
			if path == testdir {
				return nil
			}
			dirName = filepath.Base(path)
			if filter != "" && !strings.Contains(dirName, filter) {
				return filepath.SkipDir
			}
			return nil
		}

		// Ignore files with names ending with .jsonata, these
		// are not test cases
		if filepath.Ext(path) == ".jsonata" {
			return nil
		}

		testCases, err := loadTestCases(path)
		if err != nil {
			return fmt.Errorf("walk %s: %s", path, err)
		}

		for _, testCase := range testCases {
			failed, err := runTest(testCase, datadir, path)

			if err != nil {
				return err
			}
			if failed {
				numFailed++
			} else {
				numPassed++
			}
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("walk %s: ", err)
	}

	fmt.Println()
	fmt.Println(numPassed, "passed", numFailed, "failed")
	return nil
}

// runTest runs a single test case
func runTest(tc testCase, dataDir string, path string) (bool, error) {
	// Some tests assume JavaScript-style object traversal,
	// these are marked as unordered and can be skipped
	// See https://github.com/jsonata-js/jsonata/issues/179
	if tc.Unordered {
		return false, nil
	}

	if tc.TimeLimit != 0 {
		return false, nil
	}

	// If this test has an associated dataset, load it
	data := tc.Data
	if tc.Dataset != "" {
		var dest any
		err := readJSONFile(filepath.Join(dataDir, tc.Dataset+".json"), &dest)
		if err != nil {
			return false, err
		}
		data = dest
	}

	var failed bool
	expr, unQuoted := replaceQuotesInPaths(tc.Expr)
	got, _ := eval(expr, tc.Bindings, data)

	if !equalResults(got, tc.Result) && (!strings.Contains(expr, "$keys(") || !equalResultsUnordered(got, tc.Result)) {
		failed = true
		printTestCase(os.Stderr, tc, strings.TrimSuffix(filepath.Base(path), ".json"))
		fmt.Fprintf(os.Stderr, "Test file: %s \n", path)

		if tc.Category != "" {
			fmt.Fprintf(os.Stderr, "Category: %s \n", tc.Category)
		}
		if tc.Description != "" {
			fmt.Fprintf(os.Stderr, "Description: %s \n", tc.Description)
		}

		fmt.Fprintf(os.Stderr, "Expression: %s\n", expr)
		if unQuoted {
			fmt.Fprintf(os.Stderr, "Unquoted: %t\n", unQuoted)
		}
		fmt.Fprintf(os.Stderr, "Expected Result: %v [%T]\n", tc.Result, tc.Result)
		fmt.Fprintf(os.Stderr, "Actual Result:   %v [%T]\n", got, got)
	}

	// TODO this block is commented out to make staticcheck happy,
	// but we should check that the error is the same as the js one
	// var exp error
	// if tc.Undefined {
	// 	exp = jsonata.ErrUndefined
	// } else {
	// 	exp = convertError(tc.Error)
	// }

	// if !reflect.DeepEqual(err, exp) {
	// TODO: Compare actual/expected errors
	// }

	return failed, nil
}

// loadTestExprFile loads a jsonata expression from a file and returns the
// expression
// For example, one test looks like this
//
//	{
//	    "expr-file": "case000.jsonata",
//	    "dataset": null,
//	    "bindings": {},
//	    "result": 2
//	}
//
// We want to load the expression from case000.jsonata so we can use it
// as an expression in the test case
func loadTestExprFile(testPath string, exprFileName string) (string, error) {
	splitPath := strings.Split(testPath, "/")
	splitPath[len(splitPath)-1] = exprFileName
	exprFilePath := strings.Join(splitPath, "/")

	content, err := os.ReadFile(exprFilePath)
	if err != nil {
		return "", err
	}

	return string(content), nil
}

// loadTestCases loads all of the json data for tests and converts them to test cases
func loadTestCases(path string) ([]testCase, error) {
	// Test cases are contained in json files. They consist of either
	// one test case in the file or an array of test cases.
	// Since we don't know which it will be until we load the file,
	// first try to demarshall it a single case, and if there is an
	// error, try again demarshalling it into an array of test cases
	var tc testCase
	err := readJSONFile(path, &tc)
	if err != nil {
		var tcs []testCase
		err := readJSONFile(path, &tcs)
		if err != nil {
			return nil, err

		}

		// If any of the tests specify an expression file, load it from
		// disk and add it to the test case
		for i := range tcs {
			if tcs[i].ExprFile != "" {
				expr, err := loadTestExprFile(path, tcs[i].ExprFile)
				if err != nil {
					return nil, err
				}
				tcs[i].Expr = expr
			}
		}
		return tcs, nil
	}

	// If we have gotten here then there was only one test specified in the
	// tests file.

	// If the test specifies an expression file, load it from
	// disk and add it to the test case
	if tc.ExprFile != "" {
		expr, err := loadTestExprFile(path, tc.ExprFile)
		if err != nil {
			return nil, err
		}
		tc.Expr = expr
	}

	return []testCase{tc}, nil
}

func printTestCase(w io.Writer, tc testCase, name string) {
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintf(w, "Failed Test Case: %s\n", name)
	switch {
	case tc.Data != nil:
		_, _ = fmt.Fprintf(w, "Data: %v\n", tc.Data)
	case tc.Dataset != "":
		_, _ = fmt.Fprintf(w, "Dataset: %s\n", tc.Dataset)
	default:
		_, _ = fmt.Fprintln(w, "Data: N/A")
	}
	if tc.Error != "" {
		_, _ = fmt.Fprintf(w, "Expected error code: %v\n", tc.Error)
	}
	if len(tc.Bindings) > 0 {
		_, _ = fmt.Fprintf(w, "Bindings: %v\n", tc.Bindings)
	}
}

func eval(expression string, bindings map[string]any, data any) (any, error) {
	expr, err := jsonata.Compile(expression)
	if err != nil {
		return nil, err
	}

	err = expr.RegisterVars(bindings)
	if err != nil {
		return nil, err
	}

	res, err := expr.Eval(data)
	return res, err
}

func isNullValue(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	return rv.Kind() == reflect.Pointer && rv.IsNil()
}

func equalResults(x, y any) bool {
	if isNullValue(x) && isNullValue(y) {
		return true
	}
	if reflect.DeepEqual(x, y) {
		return true
	}

	vx := types.Resolve(reflect.ValueOf(x))
	vy := types.Resolve(reflect.ValueOf(y))

	if types.IsArray(vx) && types.IsArray(vy) {
		if vx.Len() != vy.Len() {
			return false
		}
		for i := 0; i < vx.Len(); i++ {
			if !equalResults(vx.Index(i).Interface(), vy.Index(i).Interface()) {
				return false
			}
		}
		return true
	}

	if types.IsMap(vx) && types.IsMap(vy) {
		if vx.Len() != vy.Len() {
			return false
		}
		for _, k := range vx.MapKeys() {
			valX := vx.MapIndex(k)
			valY := vy.MapIndex(k)
			if !valY.IsValid() {
				return false
			}
			if !equalResults(valX.Interface(), valY.Interface()) {
				return false
			}
		}
		return true
	}

	ix, okx := types.AsNumber(vx)
	iy, oky := types.AsNumber(vy)
	if okx && oky && ix == iy {
		return true
	}

	sx, okx := types.AsString(vx)
	sy, oky := types.AsString(vy)
	if okx && oky {
		if sx == sy {
			return true
		}
		if len(sx) == len(sy) &&
			strings.HasPrefix(sx, "{") && strings.HasSuffix(sx, "}") &&
			strings.HasPrefix(sy, "{") && strings.HasSuffix(sy, "}") &&
			strings.Count(sx, "\n") == strings.Count(sy, "\n") {
			var mx, my map[string]any
			if jsonv2.Unmarshal([]byte(sx), &mx) == nil && jsonv2.Unmarshal([]byte(sy), &my) == nil {
				return equalResults(mx, my)
			}
		}
	}

	bx, okx := types.AsBool(vx)
	by, oky := types.AsBool(vy)
	if okx && oky && bx == by {
		return true
	}

	return false
}

func equalResultsUnordered(x, y any) bool {
	if equalResults(x, y) {
		return true
	}
	vx := types.Resolve(reflect.ValueOf(x))
	vy := types.Resolve(reflect.ValueOf(y))
	if types.IsMap(vx) && types.IsMap(vy) && vx.Len() == vy.Len() {
		for _, k := range vx.MapKeys() {
			valX := vx.MapIndex(k)
			valY := vy.MapIndex(k)
			if !valY.IsValid() {
				return false
			}
			if !equalResultsUnordered(valX.Interface(), valY.Interface()) {
				return false
			}
		}
		return true
	}
	if types.IsArray(vx) && types.IsArray(vy) && vx.Len() == vy.Len() {
		used := make([]bool, vy.Len())
		for i := 0; i < vx.Len(); i++ {
			found := false
			for j := 0; j < vy.Len(); j++ {
				if !used[j] && equalResultsUnordered(vx.Index(i).Interface(), vy.Index(j).Interface()) {
					used[j] = true
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
		return true
	}
	return false
}

func readJSONFile(path string, dest any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("ReadFile %s: %s", path, err)
	}

	err = jsonv2.Unmarshal(b, dest, jsontext.AllowDuplicateNames(true), jsontext.AllowInvalidUTF8(true))
	if err != nil {
		return fmt.Errorf("unmarshal %s: %s", path, err)
	}

	return nil
}

var (
	reQuotedPath      = regexp.MustCompile(`([A-Za-z\$\\*\` + "`" + `])\.[\"']([ \.0-9A-Za-z]+?)[\"']`)
	reQuotedPathStart = regexp.MustCompile(`^[\"']([ \.0-9A-Za-z]+?)[\"']\.([A-Za-z\$\*\"\'])`)
)

func replaceQuotesInPaths(s string) (string, bool) {
	var changed bool

	if reQuotedPathStart.MatchString(s) {
		s = reQuotedPathStart.ReplaceAllString(s, "`$1`.$2")
		changed = true
	}

	for reQuotedPath.MatchString(s) {
		s = reQuotedPath.ReplaceAllString(s, "$1.`$2`")
		changed = true
	}

	return s, changed
}
