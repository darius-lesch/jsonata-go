// Copyright 2018 Blues Inc.  All rights reserved.
// Use of this source code is governed by licenses granted by the
// copyright holder including that found in the LICENSE file.

package jparse

import (
	"fmt"
	"regexp"
	"regexp/syntax"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// Node represents an individual node in a syntax tree.
type Node interface {
	String() string
	optimize() (Node, error)
}

// A StringNode represents a string literal.
type StringNode struct {
	Value string
}

func parseString(p *parser, t token) (Node, error) {

	s, ok := unescape(t.Value)
	if !ok {
		typ := ErrIllegalEscape
		if len(s) > 0 && s[0] == 'u' {
			typ = ErrIllegalEscapeHex
		}

		return nil, newErrorHint(typ, t, s)
	}

	return &StringNode{
		Value: s,
	}, nil
}

func (n *StringNode) optimize() (Node, error) {
	return n, nil
}

func (n StringNode) String() string {
	return fmt.Sprintf("%q", n.Value)
}

// A NumberNode represents a number literal.
type NumberNode struct {
	Value float64
}

func parseNumber(p *parser, t token) (Node, error) {

	// Number literals are promoted to type float64.
	n, err := strconv.ParseFloat(t.Value, 64)
	if err != nil {
		typ := ErrInvalidNumber
		if e, ok := err.(*strconv.NumError); ok && e.Err == strconv.ErrRange {
			typ = ErrNumberRange
		}
		return nil, newError(typ, t)
	}

	return &NumberNode{
		Value: n,
	}, nil
}

func (n *NumberNode) optimize() (Node, error) {
	return n, nil
}

func (n NumberNode) String() string {
	return fmt.Sprintf("%g", n.Value)
}

// A BooleanNode represents the boolean constant true or false.
type BooleanNode struct {
	Value bool
}

func parseBoolean(p *parser, t token) (Node, error) {

	var b bool

	switch t.Value {
	case "true":
		b = true
	case "false":
		b = false
	default: // should be unreachable
		panicf("parseBoolean: unexpected value %q", t.Value)
	}

	return &BooleanNode{
		Value: b,
	}, nil
}

func (n *BooleanNode) optimize() (Node, error) {
	return n, nil
}

func (n BooleanNode) String() string {
	return fmt.Sprintf("%t", n.Value)
}

// A NullNode represents the JSON null value.
type NullNode struct{}

func parseNull(p *parser, t token) (Node, error) {
	return &NullNode{}, nil
}

func (n *NullNode) optimize() (Node, error) {
	return n, nil
}

func (NullNode) String() string {
	return "null"
}

// A RegexNode represents a regular expression.
type RegexNode struct {
	Value *regexp.Regexp
}

func parseRegex(p *parser, t token) (Node, error) {

	if t.Value == "" {
		return nil, newError(ErrEmptyRegex, t)
	}

	re, err := regexp.Compile(t.Value)
	if err != nil {
		hint := "unknown error"
		if e, ok := err.(*syntax.Error); ok {
			hint = string(e.Code)
		}

		return nil, newErrorHint(ErrInvalidRegex, t, hint)
	}

	return &RegexNode{
		Value: re,
	}, nil
}

func (n *RegexNode) optimize() (Node, error) {
	return n, nil
}

func (n RegexNode) String() string {
	var expr string
	if n.Value != nil {
		expr = n.Value.String()
	}
	return fmt.Sprintf("/%s/", expr)
}

// A VariableNode represents a JSONata variable.
type VariableNode struct {
	Name string
}

func parseVariable(p *parser, t token) (Node, error) {
	return &VariableNode{
		Name: t.Value,
	}, nil
}

func (n *VariableNode) optimize() (Node, error) {
	return n, nil
}

func (n VariableNode) String() string {
	return "$" + n.Name
}

// A NameNode represents a JSON field name.
type NameNode struct {
	Value   string
	escaped bool
}

func parseName(p *parser, t token) (Node, error) {
	return &NameNode{
		Value: t.Value,
	}, nil
}

func parseEscapedName(p *parser, t token) (Node, error) {
	return &NameNode{
		Value:   t.Value,
		escaped: true,
	}, nil
}

func (n *NameNode) optimize() (Node, error) {
	return &PathNode{
		Steps: []Node{n},
	}, nil
}

func (n NameNode) String() string {
	if n.escaped {
		return fmt.Sprintf("`%s`", n.Value)
	}
	return n.Value
}

// Escaped returns true for names enclosed in backticks (e.g.
// `Product Name`), and false otherwise. This doesn't affect
// evaluation but may be useful when recreating a JSONata
// expression from its AST.
func (n NameNode) Escaped() bool {
	return n.escaped
}

// A PathNode represents a JSON object path. It consists of one
// or more 'steps' or Nodes (most commonly NameNode objects).
type PathNode struct {
	Steps         []Node
	KeepArrays    bool
	Tuple         bool
	SeekingParent []*AncestorSlot
}

func (n *PathNode) optimize() (Node, error) {
	return n, nil
}

func (n PathNode) String() string {
	var b strings.Builder
	for i, step := range n.Steps {
		if i > 0 && !IsSortNode(step) {
			b.WriteString(".")
		}
		b.WriteString(step.String())
	}
	if n.KeepArrays {
		b.WriteString("[]")
	}
	return b.String()
}

// A NegationNode represents a numeric negation operation.
type NegationNode struct {
	RHS Node
}

func parseNegation(p *parser, t token) (Node, error) {
	return &NegationNode{
		RHS: p.parseExpression(p.bp(t.Type)),
	}, nil
}

func (n *NegationNode) optimize() (Node, error) {

	var err error

	n.RHS, err = n.RHS.optimize()
	if err != nil {
		return nil, err
	}

	// If the operand is a number literal, negate it now
	// instead of waiting for evaluation.
	if number, ok := n.RHS.(*NumberNode); ok {
		return &NumberNode{
			Value: -number.Value,
		}, nil
	}

	return n, nil
}

func (n NegationNode) String() string {
	return fmt.Sprintf("-%s", n.RHS)
}

// A RangeNode represents the range operator.
type RangeNode struct {
	LHS Node
	RHS Node
}

func (n *RangeNode) optimize() (Node, error) {

	var err error

	n.LHS, err = n.LHS.optimize()
	if err != nil {
		return nil, err
	}

	n.RHS, err = n.RHS.optimize()
	if err != nil {
		return nil, err
	}

	return n, nil
}

func (n RangeNode) String() string {
	return fmt.Sprintf("%s..%s", n.LHS, n.RHS)
}

// An ArrayNode represents an array of items.
type ArrayNode struct {
	Items []Node
}

func parseArray(p *parser, t token) (Node, error) {

	var items []Node

	for hasItems := p.token.Type != typeBracketClose; hasItems; { // disallow trailing commas

		item := p.parseExpression(0)

		if p.token.Type == typeRange {

			p.consume(typeRange, true)

			item = &RangeNode{
				LHS: item,
				RHS: p.parseExpression(0),
			}
		}

		items = append(items, item)

		if p.token.Type != typeComma {
			break
		}
		p.consume(typeComma, true)
	}

	p.consume(typeBracketClose, false)

	return &ArrayNode{
		Items: items,
	}, nil
}

func (n *ArrayNode) optimize() (Node, error) {

	var err error

	for i := range n.Items {
		n.Items[i], err = n.Items[i].optimize()
		if err != nil {
			return nil, err
		}
	}

	return n, nil
}

func (n ArrayNode) String() string {
	return fmt.Sprintf("[%s]", joinNodes(n.Items, ", "))
}

// An ObjectNode represents an object, an unordered list of
// key-value pairs.
type ObjectNode struct {
	Pairs [][2]Node
}

func parseObject(p *parser, t token) (Node, error) {

	var pairs [][2]Node

	for hasItems := p.token.Type != typeBraceClose; hasItems; { // disallow trailing commas

		key := p.parseExpression(0)
		p.consume(typeColon, true)
		value := p.parseExpression(0)

		pairs = append(pairs, [2]Node{key, value})

		if p.token.Type != typeComma {
			break
		}
		p.consume(typeComma, true)
	}

	p.consume(typeBraceClose, false)

	return &ObjectNode{
		Pairs: pairs,
	}, nil
}

func (n *ObjectNode) optimize() (Node, error) {

	var err error

	for i := range n.Pairs {
		for j := range 2 {
			n.Pairs[i][j], err = n.Pairs[i][j].optimize()
			if err != nil {
				return nil, err
			}
		}
	}

	return n, nil
}

func (n ObjectNode) String() string {

	values := make([]string, len(n.Pairs))

	for i, pair := range n.Pairs {
		values[i] = fmt.Sprintf("%s: %s", pair[0], pair[1])
	}

	return fmt.Sprintf("{%s}", strings.Join(values, ", "))
}

// A BlockNode represents a block expression.
type BlockNode struct {
	Exprs []Node
}

func parseBlock(p *parser, t token) (Node, error) {

	var exprs []Node

	for p.token.Type != typeParenClose { // allow trailing semicolons

		exprs = append(exprs, p.parseExpression(0))

		if p.token.Type != typeSemicolon {
			break
		}
		p.consume(typeSemicolon, true)
	}

	p.consume(typeParenClose, false)

	return &BlockNode{
		Exprs: exprs,
	}, nil
}

func (n *BlockNode) optimize() (Node, error) {

	var err error

	for i := range n.Exprs {
		n.Exprs[i], err = n.Exprs[i].optimize()
		if err != nil {
			return nil, err
		}
	}

	return n, nil
}

func (n BlockNode) String() string {
	return fmt.Sprintf("(%s)", joinNodes(n.Exprs, "; "))
}

// A WildcardNode represents the wildcard operator.
type WildcardNode struct{}

func parseWildcard(p *parser, t token) (Node, error) {
	return &WildcardNode{}, nil
}

func (n *WildcardNode) optimize() (Node, error) {
	return n, nil
}

func (WildcardNode) String() string {
	return "*"
}

// A DescendentNode represents the descendent operator.
type DescendentNode struct{}

func parseDescendent(p *parser, t token) (Node, error) {
	return &DescendentNode{}, nil
}

func (n *DescendentNode) optimize() (Node, error) {
	return n, nil
}

func (DescendentNode) String() string {
	return "**"
}

// An ObjectTransformationNode represents the object transformation
// operator.
type ObjectTransformationNode struct {
	Pattern Node
	Updates Node
	Deletes Node
}

func parseObjectTransformation(p *parser, t token) (Node, error) {

	var deletes Node

	pattern := p.parseExpression(0)
	p.consume(typePipe, true)
	updates := p.parseExpression(0)
	if p.token.Type == typeComma {
		p.consume(typeComma, true)
		deletes = p.parseExpression(0)
	}
	p.consume(typePipe, true)

	return &ObjectTransformationNode{
		Pattern: pattern,
		Updates: updates,
		Deletes: deletes,
	}, nil
}

func (n *ObjectTransformationNode) optimize() (Node, error) {

	var err error

	n.Pattern, err = n.Pattern.optimize()
	if err != nil {
		return nil, err
	}

	n.Updates, err = n.Updates.optimize()
	if err != nil {
		return nil, err
	}

	if n.Deletes != nil {
		n.Deletes, err = n.Deletes.optimize()
		if err != nil {
			return nil, err
		}
	}

	return n, nil
}

func (n ObjectTransformationNode) String() string {

	s := fmt.Sprintf("|%s|%s", n.Pattern, n.Updates)
	if n.Deletes != nil {
		s += fmt.Sprintf(", %s", n.Deletes)
	}
	s += "|"
	return s
}

// A ParamType represents the type of a parameter in a lambda
// function signature.
type ParamType uint

// Supported parameter types.
const (
	ParamTypeNumber ParamType = 1 << iota
	ParamTypeString
	ParamTypeBool
	ParamTypeNull
	ParamTypeArray
	ParamTypeObject
	ParamTypeFunc
	ParamTypeJSON
	ParamTypeAny
)

func parseParamType(r rune) (ParamType, bool) {

	var typ ParamType

	switch r {
	case 'n':
		typ = ParamTypeNumber
	case 's':
		typ = ParamTypeString
	case 'b':
		typ = ParamTypeBool
	case 'l':
		typ = ParamTypeNull
	case 'a':
		typ = ParamTypeArray
	case 'o':
		typ = ParamTypeObject
	case 'f':
		typ = ParamTypeFunc
	case 'j':
		typ = ParamTypeJSON
	case 'x':
		typ = ParamTypeAny
	default:
		return 0, false
	}

	return typ, true
}

func (typ ParamType) String() string {

	var s string

	if typ&ParamTypeNumber != 0 {
		s += "n"
	}
	if typ&ParamTypeString != 0 {
		s += "s"
	}
	if typ&ParamTypeBool != 0 {
		s += "b"
	}
	if typ&ParamTypeNull != 0 {
		s += "l"
	}
	if typ&ParamTypeArray != 0 {
		s += "a"
	}
	if typ&ParamTypeObject != 0 {
		s += "o"
	}
	if typ&ParamTypeFunc != 0 {
		s += "f"
	}
	if typ&ParamTypeJSON != 0 {
		s += "j"
	}
	if typ&ParamTypeAny != 0 {
		s += "x"
	}

	if len(s) > 1 {
		s = "(" + s + ")"
	}

	return s
}

// A ParamOpt represents the options on a parameter in a lambda
// function signature.
type ParamOpt uint8

const (
	_ ParamOpt = iota

	// ParamOptional denotes an optional parameter.
	ParamOptional

	// ParamVariadic denotes a variadic parameter.
	ParamVariadic

	// ParamContextable denotes a parameter that can be
	// replaced by the evaluation context if no value is
	// provided by the caller.
	ParamContextable
)

func parseParamOpt(r rune) (ParamOpt, bool) {

	var opt ParamOpt

	switch r {
	case '?':
		opt = ParamOptional
	case '+':
		opt = ParamVariadic
	case '-':
		opt = ParamContextable
	default:
		return 0, false
	}

	return opt, true
}

func (opt ParamOpt) String() string {
	switch opt {
	case ParamOptional:
		return "?"
	case ParamVariadic:
		return "+"
	case ParamContextable:
		return "-"
	default:
		return ""
	}
}

// A Param represents a parameter in a lambda function signature.
type Param struct {
	Type      ParamType
	Option    ParamOpt
	SubParams []Param
}

func (p Param) String() string {

	var s strings.Builder
	s.WriteString(p.Type.String())

	if p.SubParams != nil {
		s.WriteString("<")
		for _, sub := range p.SubParams {
			s.WriteString(sub.String())
		}
		s.WriteString(">")
	}

	s.WriteString(p.Option.String())
	return s.String()
}

func parseParams(s string) ([]Param, error) {

	params := []Param{}

	for len(s) > 0 {

		r, w := utf8.DecodeRuneInString(s)

		if r == ':' {
			break
		}

		if typ, ok := parseParamType(r); ok {
			params = append(params, Param{
				Type: typ,
			})
			s = s[w:]
			continue
		}

		if r == '(' {
			part := getBracketedString(s, '(', ')')
			var types ParamType
			for _, c := range part {
				typ, ok := parseParamType(c)
				if !ok {
					// TODO: Add position to this error.
					return nil, &Error{
						Type: ErrInvalidUnionType,
						Hint: string(c),
					}
				}
				types |= typ
			}
			params = append(params, Param{
				Type: types,
			})
			s = s[len(part)+2:]
			continue
		}

		if opt, ok := parseParamOpt(r); ok {
			if len(params) == 0 {
				// TODO: Add position to this error.
				return nil, &Error{
					Type: ErrUnmatchedOption,
					Hint: string(r),
				}
			}
			params[len(params)-1].Option = opt
			s = s[w:]
			continue
		}

		if r == '<' {
			if len(params) == 0 {
				// TODO: Add position to this error.
				return nil, &Error{
					Type: ErrUnmatchedSubtype,
				}
			}
			n := len(params) - 1
			if params[n].Type != ParamTypeArray && params[n].Type != ParamTypeFunc {
				// TODO: Add position to this error.
				return nil, &Error{
					Type: ErrInvalidSubtype,
					Hint: params[n].Type.String(),
				}
			}
			part := getBracketedString(s, '<', '>')
			sub, err := parseParams(part)
			if err != nil {
				return nil, err
			}
			params[n].SubParams = sub
			s = s[len(part)+2:]
			continue
		}

		// TODO: Add position to this error.
		return nil, &Error{
			Type: ErrInvalidParamType,
			Hint: string(r),
		}
	}

	return params, nil
}

func getBracketedString(s string, open, close rune) string {

	var depth int

	for pos, c := range s {

		if pos == 0 && c != open {
			break
		}

		if c == open {
			depth++
			continue
		}

		if c == close {
			depth--
			if depth == 0 {
				return s[utf8.RuneLen(open):pos]
			}
		}
	}

	return ""
}

// A LambdaNode represents a user-defined JSONata function.
type LambdaNode struct {
	Body       Node
	ParamNames []string
	shorthand  bool
}

func (n *LambdaNode) optimize() (Node, error) {

	var err error

	n.Body, err = n.Body.optimize()
	if err != nil {
		return nil, err
	}

	return n, nil
}

func (n LambdaNode) String() string {

	name := "function"
	if n.shorthand {
		name = "λ"
	}

	params := make([]string, len(n.ParamNames))
	for i, s := range n.ParamNames {
		params[i] = "$" + s
	}

	return fmt.Sprintf("%s(%s){%s}", name, strings.Join(params, ", "), n.Body)
}

// Shorthand returns true if the lambda function was defined
// with the shorthand symbol "λ", and false otherwise. This
// doesn't affect evaluation but may be useful when recreating
// a JSONata expression from its AST.
func (n LambdaNode) Shorthand() bool {
	return n.shorthand
}

// A TypedLambdaNode represents a user-defined JSONata function
// with a type signature.
type TypedLambdaNode struct {
	*LambdaNode
	In  []Param
	Out []Param
}

func (n *TypedLambdaNode) optimize() (Node, error) {

	node, err := n.LambdaNode.optimize()
	if err != nil {
		return nil, err
	}
	n.LambdaNode = node.(*LambdaNode)

	return n, nil
}

func (n TypedLambdaNode) String() string {

	name := "function"
	if n.shorthand {
		name = "λ"
	}

	params := make([]string, len(n.ParamNames))
	for i, s := range n.ParamNames {
		params[i] = "$" + s
	}

	inputs := make([]string, len(n.In))
	for i, p := range n.In {
		inputs[i] = p.String()
	}

	return fmt.Sprintf("%s(%s)<%s>{%s}", name, strings.Join(params, ", "), strings.Join(inputs, ""), n.Body)
}

// A PartialNode represents a partially applied function.
type PartialNode struct {
	Func Node
	Args []Node
}

func (n *PartialNode) optimize() (Node, error) {

	var err error

	n.Func, err = n.Func.optimize()
	if err != nil {
		return nil, err
	}

	for i := range n.Args {
		n.Args[i], err = n.Args[i].optimize()
		if err != nil {
			return nil, err
		}
	}

	return n, nil
}

func (n PartialNode) String() string {
	return fmt.Sprintf("%s(%s)", n.Func, joinNodes(n.Args, ", "))
}

// A PlaceholderNode represents a placeholder argument
// in a partially applied function.
type PlaceholderNode struct{}

func (n *PlaceholderNode) optimize() (Node, error) {
	return n, nil
}

func (PlaceholderNode) String() string {
	return "?"
}

// A FunctionCallNode represents a call to a function.
type FunctionCallNode struct {
	Func Node
	Args []Node
}

const typePlaceholder = typeCondition

func parseFunctionCall(p *parser, t token, lhs Node) (Node, error) {

	if isLambda, shorthand := isLambdaName(lhs); isLambda {
		return parseLambdaDefinition(p, shorthand)
	}

	var args []Node
	var isPartial bool

	for hasArgs := p.token.Type != typeParenClose; hasArgs; { // disallow trailing commas

		var arg Node

		if p.token.Type == typePlaceholder {
			isPartial = true
			arg = &PlaceholderNode{}
			p.consume(typePlaceholder, true)
		} else {
			arg = p.parseExpression(0)
		}

		args = append(args, arg)

		if p.token.Type != typeComma {
			break
		}
		p.consume(typeComma, true)
	}

	p.consume(typeParenClose, false)

	if isPartial {
		return &PartialNode{
			Func: lhs,
			Args: args,
		}, nil
	}

	return &FunctionCallNode{
		Func: lhs,
		Args: args,
	}, nil
}

func (n *FunctionCallNode) optimize() (Node, error) {

	var err error

	n.Func, err = n.Func.optimize()
	if err != nil {
		return nil, err
	}

	for i := range n.Args {
		n.Args[i], err = n.Args[i].optimize()
		if err != nil {
			return nil, err
		}
	}

	return n, nil
}

func (n FunctionCallNode) String() string {
	return fmt.Sprintf("%s(%s)", n.Func, joinNodes(n.Args, ", "))
}

func isLambdaName(n Node) (bool, bool) {
	switch n := n.(type) {
	case *NameNode:
		return n.Value == "function" || n.Value == "λ", n.Value == "λ"
	default:
		return false, false
	}
}

func parseLambdaDefinition(p *parser, shorthand bool) (Node, error) {

	var params []Param

	paramNames, err := extractParamNames(p)
	if err != nil {
		return nil, err
	}

	sig, isTyped := extractSignature(p)
	if isTyped {
		params, err = parseParams(sig)
		if err != nil {
			return nil, err
		}
		hasVariadic := false
		for _, param := range params {
			if param.Option == ParamVariadic {
				hasVariadic = true
				break
			}
		}
		if (!hasVariadic && len(params) != len(paramNames)) || (hasVariadic && len(params) > len(paramNames)) {
			return nil, newError(ErrParamCount, p.token)
		}
	}

	p.consume(typeBraceOpen, true)
	body := p.parseExpression(0)
	p.consume(typeBraceClose, true)

	lambda := &LambdaNode{
		Body:       body,
		ParamNames: paramNames,
		shorthand:  shorthand,
	}

	if !isTyped {
		return lambda, nil
	}

	return &TypedLambdaNode{
		LambdaNode: lambda,
		In:         params,
	}, nil
}

func extractParamNames(p *parser) ([]string, error) {

	var names []string
	usedNames := map[string]bool{}

	currToken := p.token
	for hasArgs := p.token.Type != typeParenClose; hasArgs; { // disallow trailing commas

		arg := p.parseExpression(0)

		v, ok := arg.(*VariableNode)
		if !ok {
			return nil, newError(ErrIllegalParam, currToken)
		}

		if usedNames[v.Name] {
			return nil, newError(ErrDuplicateParam, currToken)
		}

		usedNames[v.Name] = true
		names = append(names, v.Name)

		if p.token.Type != typeComma {
			break
		}
		p.consume(typeComma, true)

		currToken = p.token
	}

	p.consume(typeParenClose, false)

	return names, nil
}

func extractSignature(p *parser) (string, bool) {

	const (
		typeSigStart = typeLess
		typeSigEnd   = typeGreater
	)

	if p.token.Type != typeSigStart {
		return "", false
	}

	var sig strings.Builder
	depth := 1

Loop:
	for p.token.Type != typeBraceOpen && p.token.Type != typeEOF {

		p.advance(true)

		switch p.token.Type {
		case typeSigEnd:
			depth--
			if depth == 0 {
				break Loop
			}
		case typeSigStart:
			depth++
		}

		sig.WriteString(p.token.Value)
	}

	p.consume(typeSigEnd, true)
	return sig.String(), true
}

// A PredicateNode represents a predicate expression.
type PredicateNode struct {
	Expr    Node
	Filters []Node
}

func (n *PredicateNode) optimize() (Node, error) {
	return n, nil
}

func (n PredicateNode) String() string {
	return fmt.Sprintf("%s[%s]", n.Expr, joinNodes(n.Filters, ", "))
}

// A GroupNode represents a group expression.
type GroupNode struct {
	Expr Node
	*ObjectNode
}

func parseGroup(p *parser, t token, lhs Node) (Node, error) {

	obj, err := parseObject(p, t)
	if err != nil {
		return nil, err
	}

	return &GroupNode{
		Expr:       lhs,
		ObjectNode: obj.(*ObjectNode),
	}, nil
}

func (n *GroupNode) optimize() (Node, error) {

	var err error

	n.Expr, err = n.Expr.optimize()
	if err != nil {
		return nil, err
	}

	if p, ok := n.Expr.(*PathNode); ok {
		p.Tuple = true
	}

	if _, isGroup := n.Expr.(*GroupNode); isGroup {
		// TODO: Add position info.
		return nil, &Error{
			Type: ErrGroupGroup,
		}
	}

	obj, err := n.ObjectNode.optimize()
	if err != nil {
		return nil, err
	}
	n.ObjectNode = obj.(*ObjectNode)

	return n, nil
}

func (n GroupNode) String() string {
	return fmt.Sprintf("%s%s", n.Expr, n.ObjectNode)
}

// A ConditionalNode represents an if-then-else expression.
type ConditionalNode struct {
	If   Node
	Then Node
	Else Node
}

func parseConditional(p *parser, t token, lhs Node) (Node, error) {

	var els Node
	rhs := p.parseExpression(0)

	if p.token.Type == typeColon {
		p.consume(typeColon, true)
		els = p.parseExpression(0)
	}

	return &ConditionalNode{
		If:   lhs,
		Then: rhs,
		Else: els,
	}, nil
}

func (n *ConditionalNode) optimize() (Node, error) {

	var err error

	n.If, err = n.If.optimize()
	if err != nil {
		return nil, err
	}

	n.Then, err = n.Then.optimize()
	if err != nil {
		return nil, err
	}

	if n.Else != nil {
		n.Else, err = n.Else.optimize()
		if err != nil {
			return nil, err
		}
	}

	return n, nil
}

func (n ConditionalNode) String() string {

	s := fmt.Sprintf("%s ? %s", n.If, n.Then)
	if n.Else != nil {
		s += fmt.Sprintf(" : %s", n.Else)
	}

	return s
}

// An AssignmentNode represents a variable assignment.
type AssignmentNode struct {
	Name  string
	Value Node
}

func parseAssignment(p *parser, t token, lhs Node) (Node, error) {

	v, ok := lhs.(*VariableNode)
	if !ok {
		return nil, newErrorHint(ErrIllegalAssignment, t, lhs.String())
	}

	return &AssignmentNode{
		Name:  v.Name,
		Value: p.parseExpression(p.bp(t.Type) - 1), // right-associative
	}, nil
}

func (n *AssignmentNode) optimize() (Node, error) {

	var err error

	n.Value, err = n.Value.optimize()
	if err != nil {
		return nil, err
	}

	return n, nil
}

func (n AssignmentNode) String() string {
	return fmt.Sprintf("$%s := %s", n.Name, n.Value)
}

// A NumericOperator is a mathematical operation between two
// numeric values.
type NumericOperator uint8

// Numeric operations supported by JSONata.
const (
	_ NumericOperator = iota
	NumericAdd
	NumericSubtract
	NumericMultiply
	NumericDivide
	NumericModulo
)

func (op NumericOperator) String() string {
	switch op {
	case NumericAdd:
		return "+"
	case NumericSubtract:
		return "-"
	case NumericMultiply:
		return "*"
	case NumericDivide:
		return "/"
	case NumericModulo:
		return "%"
	default:
		return ""
	}
}

// A NumericOperatorNode represents a numeric operation.
type NumericOperatorNode struct {
	Type NumericOperator
	LHS  Node
	RHS  Node
}

func parseNumericOperator(p *parser, t token, lhs Node) (Node, error) {

	var op NumericOperator

	switch t.Type {
	case typePlus:
		op = NumericAdd
	case typeMinus:
		op = NumericSubtract
	case typeMult:
		op = NumericMultiply
	case typeDiv:
		op = NumericDivide
	case typeMod:
		op = NumericModulo
	default: // should be unreachable
		panicf("parseNumericOperator: unexpected operator %q", t.Value)
	}

	return &NumericOperatorNode{
		Type: op,
		LHS:  lhs,
		RHS:  p.parseExpression(p.bp(t.Type)),
	}, nil
}

func (n *NumericOperatorNode) optimize() (Node, error) {

	var err error

	n.LHS, err = n.LHS.optimize()
	if err != nil {
		return nil, err
	}

	n.RHS, err = n.RHS.optimize()
	if err != nil {
		return nil, err
	}

	return n, nil
}

func (n NumericOperatorNode) String() string {
	return fmt.Sprintf("%s %s %s", n.LHS, n.Type, n.RHS)
}

// A ComparisonOperator is an operation that compares two values.
type ComparisonOperator uint8

// Comparison operations supported by JSONata.
const (
	_ ComparisonOperator = iota
	ComparisonEqual
	ComparisonNotEqual
	ComparisonLess
	ComparisonLessEqual
	ComparisonGreater
	ComparisonGreaterEqual
	ComparisonIn
)

func (op ComparisonOperator) String() string {
	switch op {
	case ComparisonEqual:
		return "="
	case ComparisonNotEqual:
		return "!="
	case ComparisonLess:
		return "<"
	case ComparisonLessEqual:
		return "<="
	case ComparisonGreater:
		return ">"
	case ComparisonGreaterEqual:
		return ">="
	case ComparisonIn:
		return "in"
	default:
		return ""
	}
}

// A ComparisonOperatorNode represents a comparison operation.
type ComparisonOperatorNode struct {
	Type ComparisonOperator
	LHS  Node
	RHS  Node
}

func parseComparisonOperator(p *parser, t token, lhs Node) (Node, error) {

	var op ComparisonOperator

	switch t.Type {
	case typeEqual:
		op = ComparisonEqual
	case typeNotEqual:
		op = ComparisonNotEqual
	case typeLess:
		op = ComparisonLess
	case typeLessEqual:
		op = ComparisonLessEqual
	case typeGreater:
		op = ComparisonGreater
	case typeGreaterEqual:
		op = ComparisonGreaterEqual
	case typeIn:
		op = ComparisonIn
	default: // should be unreachable
		panicf("parseComparisonOperator: unexpected operator %q", t.Value)
	}

	return &ComparisonOperatorNode{
		Type: op,
		LHS:  lhs,
		RHS:  p.parseExpression(p.bp(t.Type)),
	}, nil
}

func (n *ComparisonOperatorNode) optimize() (Node, error) {

	var err error

	n.LHS, err = n.LHS.optimize()
	if err != nil {
		return nil, err
	}

	n.RHS, err = n.RHS.optimize()
	if err != nil {
		return nil, err
	}

	return n, nil
}

func (n ComparisonOperatorNode) String() string {
	return fmt.Sprintf("%s %s %s", n.LHS, n.Type, n.RHS)
}

// A BooleanOperator is a logical AND or OR operation between
// two values.
type BooleanOperator uint8

// Boolean operations supported by JSONata.
const (
	_ BooleanOperator = iota
	BooleanAnd
	BooleanOr
)

func (op BooleanOperator) String() string {
	switch op {
	case BooleanAnd:
		return "and"
	case BooleanOr:
		return "or"
	default:
		return ""
	}
}

// A BooleanOperatorNode represents a boolean operation.
type BooleanOperatorNode struct {
	Type BooleanOperator
	LHS  Node
	RHS  Node
}

func parseBooleanOperator(p *parser, t token, lhs Node) (Node, error) {

	var op BooleanOperator

	switch t.Type {
	case typeAnd:
		op = BooleanAnd
	case typeOr:
		op = BooleanOr
	default: // should be unreachable
		panicf("parseBooleanOperator: unexpected operator %q", t.Value)
	}

	return &BooleanOperatorNode{
		Type: op,
		LHS:  lhs,
		RHS:  p.parseExpression(p.bp(t.Type)),
	}, nil
}

func (n *BooleanOperatorNode) optimize() (Node, error) {

	var err error

	n.LHS, err = n.LHS.optimize()
	if err != nil {
		return nil, err
	}

	n.RHS, err = n.RHS.optimize()
	if err != nil {
		return nil, err
	}

	return n, nil
}

func (n BooleanOperatorNode) String() string {
	return fmt.Sprintf("%s %s %s", n.LHS, n.Type, n.RHS)
}

// A StringConcatenationNode represents a string concatenation
// operation.
type StringConcatenationNode struct {
	LHS Node
	RHS Node
}

func parseStringConcatenation(p *parser, t token, lhs Node) (Node, error) {
	return &StringConcatenationNode{
		LHS: lhs,
		RHS: p.parseExpression(p.bp(t.Type)),
	}, nil
}

func (n *StringConcatenationNode) optimize() (Node, error) {

	var err error

	n.LHS, err = n.LHS.optimize()
	if err != nil {
		return nil, err
	}

	n.RHS, err = n.RHS.optimize()
	if err != nil {
		return nil, err
	}

	return n, nil
}

func (n StringConcatenationNode) String() string {
	return fmt.Sprintf("%s & %s", n.LHS, n.RHS)
}

// SortDir describes the sort order of a sort operation.
type SortDir uint8

// Sort orders supported by JSONata.
const (
	_ SortDir = iota
	SortDefault
	SortAscending
	SortDescending
)

// A SortTerm defines a JSONata sort term.
type SortTerm struct {
	Dir  SortDir
	Expr Node
}

// A SortNode represents a sort clause on a JSONata path step.
type SortNode struct {
	Expr  Node
	Terms []SortTerm
}

func parseSort(p *parser, t token, lhs Node) (Node, error) {

	var terms []SortTerm

	p.consume(typeParenOpen, true)

	for {
		dir := SortDefault

		switch typ := p.token.Type; typ {
		case typeLess:
			dir = SortAscending
			p.consume(typ, true)
		case typeGreater:
			dir = SortDescending
			p.consume(typ, true)
		}

		terms = append(terms, SortTerm{
			Dir:  dir,
			Expr: p.parseExpression(0),
		})

		if p.token.Type != typeComma {
			break
		}
		p.consume(typeComma, true)
	}

	p.consume(typeParenClose, true)

	return &SortNode{
		Expr:  lhs,
		Terms: terms,
	}, nil
}

func (n *SortNode) optimize() (Node, error) {

	var err error

	if n.Expr != nil {
		n.Expr, err = n.Expr.optimize()
		if err != nil {
			return nil, err
		}
	}

	for i := range n.Terms {
		n.Terms[i].Expr, err = n.Terms[i].Expr.optimize()
		if err != nil {
			return nil, err
		}
	}

	if n.Expr != nil {
		if p, ok := n.Expr.(*PathNode); ok {
			steps := make([]Node, 0, len(p.Steps)+1)
			steps = append(steps, p.Steps...)
			steps = append(steps, &SortNode{Terms: n.Terms})
			path := &PathNode{
				Steps:         steps,
				KeepArrays:    p.KeepArrays,
				Tuple:         p.Tuple,
				SeekingParent: append([]*AncestorSlot(nil), p.SeekingParent...),
			}
			if err := resolveAncestry(path); err != nil {
				return nil, err
			}
			return path, nil
		}
		if s, ok := n.Expr.(*StepNode); ok {
			path := &PathNode{
				Steps: []Node{s, &SortNode{Terms: n.Terms}},
			}
			if err := resolveAncestry(path); err != nil {
				return nil, err
			}
			return path, nil
		}
	}

	return n, nil
}

func (n SortNode) String() string {

	terms := make([]string, len(n.Terms))

	for i, t := range n.Terms {

		var sym string

		switch t.Dir {
		case SortAscending:
			sym = "<"
		case SortDescending:
			sym = ">"
		}

		terms[i] = sym + t.Expr.String()
	}

	if n.Expr != nil {
		return fmt.Sprintf("%s^(%s)", n.Expr, strings.Join(terms, ", "))
	}
	return fmt.Sprintf("^(%s)", strings.Join(terms, ", "))
}

// A FunctionApplicationNode represents a function application
// operation.
type FunctionApplicationNode struct {
	LHS Node
	RHS Node
}

func parseFunctionApplication(p *parser, t token, lhs Node) (Node, error) {
	return &FunctionApplicationNode{
		LHS: lhs,
		RHS: p.parseExpression(p.bp(t.Type)),
	}, nil
}

func (n *FunctionApplicationNode) optimize() (Node, error) {

	var err error

	n.LHS, err = n.LHS.optimize()
	if err != nil {
		return nil, err
	}

	n.RHS, err = n.RHS.optimize()
	if err != nil {
		return nil, err
	}

	return n, nil
}

func (n FunctionApplicationNode) String() string {
	return fmt.Sprintf("%s ~> %s", n.LHS, n.RHS)
}

// A dotNode is an interim structure used to process JSONata path
// expressions. It is deliberately unexported and creates a PathNode
// during its optimize phase.
type dotNode struct {
	lhs Node
	rhs Node
}

func parseDot(p *parser, t token, lhs Node) (Node, error) {
	return &dotNode{
		lhs: lhs,
		rhs: p.parseExpression(p.bp(t.Type)),
	}, nil
}

func (n *dotNode) optimize() (Node, error) {

	path := &PathNode{}

	lhs, err := n.lhs.optimize()
	if err != nil {
		return nil, err
	}

	switch lhs := lhs.(type) {
	case *NumberNode, *StringNode, *BooleanNode, *NullNode:
		// TODO: Add position info.
		return nil, &Error{
			Type: ErrPathLiteral,
			Hint: lhs.String(),
		}
	case *SortNode:
		if p, ok := lhs.Expr.(*PathNode); ok {
			path.Steps = append(path.Steps, p.Steps...)
		} else {
			path.Steps = append(path.Steps, lhs.Expr)
		}
		path.Steps = append(path.Steps, &SortNode{Terms: lhs.Terms})
	case *PathNode:
		path.Steps = lhs.Steps
		if lhs.KeepArrays {
			path.KeepArrays = true
		}
		if lhs.Tuple {
			path.Tuple = true
		}
		path.SeekingParent = append(path.SeekingParent, lhs.SeekingParent...)
	case *StepNode:
		path.Steps = []Node{lhs}
		if p, ok := lhs.Expr.(*ParentNode); ok && p.Slot != nil {
			path.SeekingParent = append(path.SeekingParent, p.Slot)
		}
		path.SeekingParent = append(path.SeekingParent, lhs.SeekingParent...)
	default:
		path.Steps = []Node{lhs}
		if p, ok := lhs.(*ParentNode); ok && p.Slot != nil {
			path.SeekingParent = append(path.SeekingParent, p.Slot)
		}
	}

	rhs, err := n.rhs.optimize()
	if err != nil {
		return nil, err
	}

	switch rhs := rhs.(type) {
	case *NumberNode, *StringNode, *BooleanNode, *NullNode:
		// TODO: Add position info.
		return nil, &Error{
			Type: ErrPathLiteral,
			Hint: rhs.String(),
		}
	case *PathNode:
		path.Steps = append(path.Steps, rhs.Steps...)
		if rhs.KeepArrays {
			path.KeepArrays = true
		}
		if rhs.Tuple {
			path.Tuple = true
		}
	default:
		path.Steps = append(path.Steps, rhs)
	}

	if err := resolveAncestry(path); err != nil {
		return nil, err
	}

	return path, nil
}

func (n dotNode) String() string {
	return fmt.Sprintf("%s.%s", n.lhs, n.rhs)
}

// A singletonArrayNode is an interim data structure used when
// processing path expressions. It is deliberately unexported
// and gets converted into a PathNode during optimization.
type singletonArrayNode struct {
	lhs Node
}

func (n *singletonArrayNode) optimize() (Node, error) {

	lhs, err := n.lhs.optimize()
	if err != nil {
		return nil, err
	}

	switch lhs := lhs.(type) {
	case *PathNode:
		lhs.KeepArrays = true
		return lhs, nil
	default:
		return &PathNode{
			Steps:      []Node{lhs},
			KeepArrays: true,
		}, nil
	}
}

func (n singletonArrayNode) String() string {
	return fmt.Sprintf("%s[]", n.lhs)
}

// A predicateNode is an interim data structure used when processing
// predicate expressions. It is deliberately unexported and gets
// converted into a PredicateNode during optimization.
type predicateNode struct {
	lhs Node // the context for this predicate
	rhs Node // the predicate expression
}

func parsePredicate(p *parser, t token, lhs Node) (Node, error) {

	if p.token.Type == typeBracketClose {
		p.consume(typeBracketClose, false)

		// Empty brackets in a path mean that we should not
		// flatten singleton arrays into single values.
		return &singletonArrayNode{
			lhs: lhs,
		}, nil
	}

	rhs := p.parseExpression(0)
	p.consume(typeBracketClose, false)

	return &predicateNode{
		lhs: lhs,
		rhs: rhs,
	}, nil
}

func (n *predicateNode) optimize() (Node, error) {

	lhs, err := n.lhs.optimize()
	if err != nil {
		return nil, err
	}

	rhs, err := n.rhs.optimize()
	if err != nil {
		return nil, err
	}

	switch lhs := lhs.(type) {
	case *GroupNode:
		return nil, &Error{
			// TODO: Add position info.
			Type: ErrGroupPredicate,
		}
	case *PathNode:
		i := len(lhs.Steps) - 1
		rhsSlots := extractSeekingParents(rhs)
		if len(rhsSlots) > 0 {
			stepNode := ToStepNode(lhs.Steps[i])
			lhs.Steps[i] = stepNode
			for _, slot := range rhsSlots {
				if slot.Level == 1 {
					if _, err := seekParent(stepNode, slot); err != nil {
						return nil, err
					}
				} else {
					slot.Level--
					stepNode.SeekingParent = append(stepNode.SeekingParent, slot)
				}
			}
			stepNode.Stages = append(stepNode.Stages, Stage{
				Type: StageFilter,
				Expr: rhs,
			})
			stepNode.Tuple = true
			return lhs, nil
		}

		switch last := lhs.Steps[i].(type) {
		case *StepNode:
			last.Stages = append(last.Stages, Stage{
				Type: StageFilter,
				Expr: rhs,
			})
		case *PredicateNode:
			last.Filters = append(last.Filters, rhs)
		default:
			step := &PredicateNode{
				Expr:    last,
				Filters: []Node{rhs},
			}
			lhs.Steps = append(lhs.Steps[:i], step)
		}
		return lhs, nil
	default:
		rhsSlots := extractSeekingParents(rhs)
		if len(rhsSlots) > 0 {
			stepNode := ToStepNode(lhs)
			for _, slot := range rhsSlots {
				if slot.Level == 1 {
					if _, err := seekParent(stepNode, slot); err != nil {
						return nil, err
					}
				} else {
					slot.Level--
					stepNode.SeekingParent = append(stepNode.SeekingParent, slot)
				}
			}
			stepNode.Stages = append(stepNode.Stages, Stage{
				Type: StageFilter,
				Expr: rhs,
			})
			stepNode.Tuple = true
			p := &PathNode{
				Steps: []Node{stepNode},
			}
			p.SeekingParent = append(p.SeekingParent, stepNode.SeekingParent...)
			return p, nil
		}
		return &PredicateNode{
			Expr:    lhs,
			Filters: []Node{rhs},
		}, nil
	}
}

func (n *predicateNode) String() string {
	return fmt.Sprintf("%s[%s]", n.lhs, n.rhs)
}

// Helpers

func joinNodes(nodes []Node, sep string) string {

	values := make([]string, len(nodes))

	for i, n := range nodes {
		values[i] = n.String()
	}

	return strings.Join(values, sep)
}

var jsonEscapes = map[rune]string{
	'"':  "\"",
	'\\': "\\",
	'/':  "/",
	'b':  "\b",
	'f':  "\f",
	'n':  "\n",
	'r':  "\r",
	't':  "\t",
}

// unescape replaces JSON escape sequences in a string with their
// unescaped equivalents. Valid escape sequences are:
//
// \X, where X is a character from jsonEscapes
// \uXXXX, where XXXX is a 4-digit hexadecimal Unicode code point.
//
// unescape returns the unescaped string and true if successful,
// otherwise it returns the invalid escape sequence and false.
func unescape(src string) (string, bool) {

	pos := strings.IndexRune(src, '\\')
	if pos < 0 {
		return src, true
	}

	prefix := src[:pos]
	pos++

	esc, w := utf8.DecodeRuneInString(src[pos:])
	pos += w

	repl := jsonEscapes[esc]

	switch {
	case repl != "":
	case esc == 'u':
		hex, w := decodeRunes(src[pos:], 4)
		pos += w

		r := parseRune(hex)

		switch {
		case utf8.ValidRune(r):
		case utf16.IsSurrogate(r):
			hex2, w := decodeRunes(src[pos:], 6)
			pos += w

			if strings.HasPrefix(hex2, "\\u") {
				r = utf16.DecodeRune(r, parseRune(hex2[2:]))
				if r != utf8.RuneError {
					break
				}
			}
			fallthrough
		default:
			return "u" + hex, false
		}
		repl = string(r)
	default:
		return string(esc), false
	}

	rest, ok := unescape(src[pos:])
	if !ok {
		return rest, ok
	}

	return prefix + repl + rest, true
}

// decodeRunes reads n runes from the string s and returns them
// as a string along with the number of bytes read. The returned
// string will always be n runes long, padded with the unicode
// replacement character if the source string contains fewer
// than n runes.
func decodeRunes(s string, n int) (string, int) {

	pos := 0
	runes := make([]rune, n)

	for i := range runes {
		r, w := utf8.DecodeRuneInString(s[pos:])
		runes[i] = r
		pos += w
	}

	return string(runes), pos
}

// parseRune converts a string of hexadecimal digits into the
// equivalent rune. It returns an invalid rune if the input is
// not valid hex.
func parseRune(hex string) rune {

	n, err := strconv.ParseInt(hex, 16, 32)
	if err != nil {
		return -1
	}

	return rune(n)
}

// A DefaultOperatorNode represents the default (elvis) operator (?:).
type DefaultOperatorNode struct {
	LHS Node
	RHS Node
}

func parseDefaultOperator(p *parser, t token, lhs Node) (Node, error) {
	return &DefaultOperatorNode{
		LHS: lhs,
		RHS: p.parseExpression(p.bp(t.Type)),
	}, nil
}

func (n *DefaultOperatorNode) optimize() (Node, error) {
	var err error
	n.LHS, err = n.LHS.optimize()
	if err != nil {
		return nil, err
	}
	n.RHS, err = n.RHS.optimize()
	if err != nil {
		return nil, err
	}
	return n, nil
}

func (n DefaultOperatorNode) String() string {
	return fmt.Sprintf("%s ?: %s", n.LHS, n.RHS)
}

// A CoalescingOperatorNode represents the coalescing operator (??).
type CoalescingOperatorNode struct {
	LHS Node
	RHS Node
}

func parseCoalescingOperator(p *parser, t token, lhs Node) (Node, error) {
	return &CoalescingOperatorNode{
		LHS: lhs,
		RHS: p.parseExpression(p.bp(t.Type)),
	}, nil
}

func (n *CoalescingOperatorNode) optimize() (Node, error) {
	var err error
	n.LHS, err = n.LHS.optimize()
	if err != nil {
		return nil, err
	}
	n.RHS, err = n.RHS.optimize()
	if err != nil {
		return nil, err
	}
	return n, nil
}

func (n CoalescingOperatorNode) String() string {
	return fmt.Sprintf("%s ?? %s", n.LHS, n.RHS)
}

// An AncestorSlot represents a parent/ancestor reference level and label.
type AncestorSlot struct {
	Label string
	Level int
	Index int
}

// A ParentNode represents the parent operator (%).
type ParentNode struct {
	Slot     *AncestorSlot
	Position int
}

func parseParent(p *parser, t token) (Node, error) {
	return &ParentNode{
		Slot:     p.newAncestorSlot(),
		Position: t.Position,
	}, nil
}

func (n *ParentNode) optimize() (Node, error) {
	return n, nil
}

func (n ParentNode) String() string {
	return "%"
}

// StageType represents whether a stage in a step is a filter or an index binding.
type StageType int

const (
	StageFilter StageType = iota
	StageIndex
)

// A Stage represents an individual filter or index binding in a step.
type Stage struct {
	Type StageType
	Expr Node
	Var  string
}

// A StepNode represents a step in a path that binds focus/index or participates in tuple streams.
type StepNode struct {
	Expr          Node
	Focus         string
	Index         string
	Stages        []Stage
	Ancestor      *AncestorSlot
	Tuple         bool
	KeepArray     bool
	ConsArray     bool
	SeekingParent []*AncestorSlot
}

func (n *StepNode) optimize() (Node, error) {
	if n.Expr != nil {
		var err error
		n.Expr, err = n.Expr.optimize()
		if err != nil {
			return nil, err
		}
	}
	for i := range n.Stages {
		if n.Stages[i].Expr != nil {
			var err error
			n.Stages[i].Expr, err = n.Stages[i].Expr.optimize()
			if err != nil {
				return nil, err
			}
		}
	}
	return n, nil
}

func (n StepNode) String() string {
	s := ""
	if n.Expr != nil {
		s = n.Expr.String()
	}
	if n.Focus != "" {
		s += "@$" + n.Focus
	}
	if n.Index != "" {
		s += "#$" + n.Index
	}
	for _, st := range n.Stages {
		switch st.Type {
		case StageFilter:
			s += fmt.Sprintf("[%s]", st.Expr)
		case StageIndex:
			s += "#$" + st.Var
		}
	}
	if n.KeepArray {
		s += "[]"
	}
	return s
}

func ToStepNode(n Node) *StepNode {
	if s, ok := n.(*StepNode); ok {
		return s
	}
	if p, ok := n.(*PredicateNode); ok {
		stages := make([]Stage, len(p.Filters))
		for i, f := range p.Filters {
			stages[i] = Stage{Type: StageFilter, Expr: f}
		}
		return &StepNode{
			Expr:   p.Expr,
			Stages: stages,
		}
	}
	return &StepNode{
		Expr: n,
	}
}

func IsSortNode(n Node) bool {
	if s, ok := n.(*StepNode); ok {
		n = s.Expr
	}
	_, ok := n.(*SortNode)
	return ok
}

func isPredicate(n Node) bool {
	if _, ok := n.(*PredicateNode); ok {
		return true
	}
	if s, ok := n.(*StepNode); ok && len(s.Stages) > 0 {
		return true
	}
	return false
}

type focusBindNode struct {
	lhs Node
	rhs Node
	tok token
}

func parseFocusBind(p *parser, t token, lhs Node) (Node, error) {
	rhs := p.parseExpression(p.bp(t.Type))
	if _, ok := rhs.(*VariableNode); !ok {
		return nil, newError(ErrIllegalContextVar, t)
	}
	return &focusBindNode{
		lhs: lhs,
		rhs: rhs,
		tok: t,
	}, nil
}

func (n *focusBindNode) optimize() (Node, error) {
	lhs, err := n.lhs.optimize()
	if err != nil {
		return nil, err
	}
	rhs, err := n.rhs.optimize()
	if err != nil {
		return nil, err
	}
	v, ok := rhs.(*VariableNode)
	if !ok {
		return nil, newError(ErrIllegalContextVar, n.tok)
	}

	if p, ok := lhs.(*PathNode); ok {
		if len(p.Steps) == 0 {
			p.Steps = []Node{lhs}
		}
		lastIdx := len(p.Steps) - 1
		lastStep := p.Steps[lastIdx]

		if isPredicate(lastStep) {
			return nil, newError(ErrContextAfterPredicate, n.tok)
		}
		if IsSortNode(lastStep) {
			return nil, newError(ErrContextAfterSort, n.tok)
		}

		step := ToStepNode(lastStep)
		if len(step.Stages) > 0 {
			return nil, newError(ErrContextAfterPredicate, n.tok)
		}
		step.Focus = v.Name
		step.Tuple = true
		p.Steps[lastIdx] = step

		return p, nil
	}

	if isPredicate(lhs) {
		return nil, newError(ErrContextAfterPredicate, n.tok)
	}
	if IsSortNode(lhs) {
		return nil, newError(ErrContextAfterSort, n.tok)
	}

	step := ToStepNode(lhs)
	if len(step.Stages) > 0 {
		return nil, newError(ErrContextAfterPredicate, n.tok)
	}
	step.Focus = v.Name
	step.Tuple = true

	return step, nil
}

func (n focusBindNode) String() string {
	return fmt.Sprintf("%s@%s", n.lhs, n.rhs)
}

type indexBindNode struct {
	lhs Node
	rhs Node
	tok token
}

func parseIndexBind(p *parser, t token, lhs Node) (Node, error) {
	rhs := p.parseExpression(p.bp(t.Type))
	if _, ok := rhs.(*VariableNode); !ok {
		return nil, newError(ErrIllegalContextVar, t)
	}
	return &indexBindNode{
		lhs: lhs,
		rhs: rhs,
		tok: t,
	}, nil
}

func (n *indexBindNode) optimize() (Node, error) {
	lhs, err := n.lhs.optimize()
	if err != nil {
		return nil, err
	}
	rhs, err := n.rhs.optimize()
	if err != nil {
		return nil, err
	}
	v, ok := rhs.(*VariableNode)
	if !ok {
		return nil, newError(ErrIllegalContextVar, n.tok)
	}

	if s, ok := lhs.(*SortNode); ok {
		var steps []Node
		if p, ok := s.Expr.(*PathNode); ok {
			steps = append(steps, p.Steps...)
		} else {
			steps = append(steps, s.Expr)
		}
		steps = append(steps, &SortNode{Terms: s.Terms})
		lhs = &PathNode{Steps: steps}
	}

	var path *PathNode
	if p, ok := lhs.(*PathNode); ok {
		path = p
	} else {
		path = &PathNode{Steps: []Node{lhs}}
	}

	if len(path.Steps) == 0 {
		path.Steps = []Node{lhs}
	}

	lastIdx := len(path.Steps) - 1
	step := ToStepNode(path.Steps[lastIdx])

	if len(step.Stages) == 0 {
		step.Index = v.Name
	} else {
		step.Stages = append(step.Stages, Stage{
			Type: StageIndex,
			Var:  v.Name,
		})
	}
	step.Tuple = true
	path.Steps[lastIdx] = step

	return path, nil
}

func (n indexBindNode) String() string {
	return fmt.Sprintf("%s#%s", n.lhs, n.rhs)
}

func extractSeekingParents(n Node) []*AncestorSlot {
	var slots []*AncestorSlot
	seen := make(map[*AncestorSlot]bool)
	addSlot := func(slot *AncestorSlot) {
		if slot != nil && slot.Level > 0 && !seen[slot] {
			seen[slot] = true
			slots = append(slots, slot)
		}
	}

	var walk func(node Node)
	walk = func(node Node) {
		if node == nil {
			return
		}
		switch n := node.(type) {
		case *ParentNode:
			addSlot(n.Slot)
		case *PathNode:
			for _, s := range n.SeekingParent {
				addSlot(s)
			}
			for _, step := range n.Steps {
				walk(step)
			}
		case *StepNode:
			for _, s := range n.SeekingParent {
				addSlot(s)
			}
			walk(n.Expr)
			for _, stage := range n.Stages {
				walk(stage.Expr)
			}
		case *BlockNode:
			for _, expr := range n.Exprs {
				walk(expr)
			}
		case *ArrayNode:
			for _, expr := range n.Items {
				walk(expr)
			}
		case *ObjectNode:
			for _, pair := range n.Pairs {
				walk(pair[0])
				walk(pair[1])
			}
		case *GroupNode:
			walk(n.Expr)
			if n.ObjectNode != nil {
				walk(n.ObjectNode)
			}
		case *PredicateNode:
			walk(n.Expr)
			for _, f := range n.Filters {
				walk(f)
			}
		case *SortNode:
			walk(n.Expr)
			for _, term := range n.Terms {
				walk(term.Expr)
			}
		case *AssignmentNode:
			walk(n.Value)
		case *ConditionalNode:
			walk(n.If)
			walk(n.Then)
			if n.Else != nil {
				walk(n.Else)
			}
		case *FunctionCallNode:
			walk(n.Func)
			for _, arg := range n.Args {
				walk(arg)
			}
		case *FunctionApplicationNode:
			walk(n.LHS)
			walk(n.RHS)
		case *NumericOperatorNode:
			walk(n.LHS)
			walk(n.RHS)
		case *ComparisonOperatorNode:
			walk(n.LHS)
			walk(n.RHS)
		case *BooleanOperatorNode:
			walk(n.LHS)
			walk(n.RHS)
		case *StringConcatenationNode:
			walk(n.LHS)
			walk(n.RHS)
		case *NegationNode:
			walk(n.RHS)
		case *RangeNode:
			walk(n.LHS)
			walk(n.RHS)
		case *DefaultOperatorNode:
			walk(n.LHS)
			walk(n.RHS)
		case *CoalescingOperatorNode:
			walk(n.LHS)
			walk(n.RHS)
		case *ObjectTransformationNode:
			walk(n.Pattern)
			walk(n.Updates)
			if n.Deletes != nil {
				walk(n.Deletes)
			}
		}
	}
	walk(n)
	return slots
}

func seekParent(node Node, slot *AncestorSlot) (*AncestorSlot, error) {
	switch n := node.(type) {
	case *StepNode:
		if n.Expr == nil {
			return nil, &Error{Type: ErrParentNotDerived}
		}
		slot, err := seekParent(n.Expr, slot)
		if err != nil {
			return nil, err
		}
		if slot.Level == 0 {
			n.Tuple = true
			switch n.Expr.(type) {
			case *BlockNode, *PathNode:
				// Inner steps already set their own Ancestor; do not overwrite.
			default:
				if n.Ancestor == nil {
					n.Ancestor = slot
				} else {
					slot.Label = n.Ancestor.Label
					n.Ancestor = slot
				}
			}
		}
		return slot, nil
	case *PredicateNode:
		return seekParent(n.Expr, slot)
	case *NameNode, *WildcardNode:
		slot.Level--
		return slot, nil
	case *ParentNode:
		slot.Level++
		return slot, nil
	case *BlockNode:
		if len(n.Exprs) > 0 {
			return seekParent(n.Exprs[len(n.Exprs)-1], slot)
		}
		return slot, nil
	case *PathNode:
		n.Tuple = true
		idx := len(n.Steps) - 1
		for slot.Level > 0 && idx >= 0 {
			stepNode := ToStepNode(n.Steps[idx])
			n.Steps[idx] = stepNode
			var err error
			slot, err = seekParent(stepNode, slot)
			if err != nil {
				return nil, err
			}
			idx--
		}
		return slot, nil
	default:
		return nil, &Error{
			Type: ErrParentNotDerived,
		}
	}
}

func resolveAncestry(path *PathNode) error {
	if len(path.Steps) == 0 {
		return nil
	}
	lastIdx := len(path.Steps) - 1
	lastStep := path.Steps[lastIdx]
	slots := extractSeekingParents(lastStep)

	for _, slot := range slots {
		idx := len(path.Steps) - 2
		for slot.Level > 0 {
			if idx < 0 {
				path.SeekingParent = append(path.SeekingParent, slot)
				break
			}
			stepNode := ToStepNode(path.Steps[idx])
			path.Steps[idx] = stepNode
			idx--

			for idx >= 0 && stepNode.Focus != "" && ToStepNode(path.Steps[idx]).Focus != "" {
				stepNode = ToStepNode(path.Steps[idx])
				path.Steps[idx] = stepNode
				idx--
			}

			var err error
			slot, err = seekParent(stepNode, slot)
			if err != nil {
				return err
			}
		}
	}
	return nil
}
