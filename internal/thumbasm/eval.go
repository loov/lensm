package thumbasm

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
)

// This file is a small interpreter for the subset of ARM's ASL pseudocode
// that appears in T32 decode sections: let-bindings, if/then, bit-string
// literals and concatenation, and a handful of library functions
// (UInt, ZeroExtend, T32ExpandImm, DecodeImmShift, ...). The encoding
// tables carry the pseudocode verbatim; evaluating it at decode time is
// what gives every immediate, shift and register list its exact meaning
// without hand-written per-instruction rules.

// errReject means the pseudocode redirected to another encoding (See) or
// declared the bit pattern undefined; the decoder tries the next candidate.
var errReject = errors.New("encoding rejected")

type kind uint8

const (
	kInt kind = iota
	kBits
	kBool
	kEnum
	kString
	kTuple
	kFloat
)

type value struct {
	k kind
	i int64 // int, bool (0/1), bits (zero-extended)
	n int   // bit width for kBits
	s string
	t []value
	f float64 // kFloat: a VFP immediate
}

func mkInt(i int64) value { return value{k: kInt, i: i} }
func mkBool(b bool) value { return value{k: kBool, i: b2i(b)} }
func mkBits(v uint64, n int) value {
	if n < 64 {
		v &= 1<<uint(n) - 1
	}
	return value{k: kBits, i: int64(v), n: n}
}

func b2i(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

func (v value) uint() uint64 { return uint64(v.i) }

// sint reads a bits value as two's complement.
func (v value) sint() int64 {
	if v.k != kBits || v.n == 0 || v.n >= 64 {
		return v.i
	}
	if v.i&(1<<uint(v.n-1)) != 0 {
		return v.i - 1<<uint(v.n)
	}
	return v.i
}

func (v value) truthy() bool { return v.i != 0 }

// ---- lexer ----

type tokKind uint8

const (
	tEOF tokKind = iota
	tIdent
	tInt
	tBits
	tString
	tOp
)

type token struct {
	k tokKind
	s string
}

func lex(src string) ([]token, error) {
	var toks []token
	i := 0
	for i < len(src) {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '\'':
			j := strings.IndexByte(src[i+1:], '\'')
			if j < 0 {
				return nil, fmt.Errorf("unterminated bit string")
			}
			toks = append(toks, token{tBits, strings.ReplaceAll(src[i+1:i+1+j], " ", "")})
			i += j + 2
		case c == '"':
			j := strings.IndexByte(src[i+1:], '"')
			if j < 0 {
				return nil, fmt.Errorf("unterminated string")
			}
			toks = append(toks, token{tString, src[i+1 : i+1+j]})
			i += j + 2
		case unicode.IsLetter(rune(c)) || c == '_':
			j := i
			for j < len(src) && (unicode.IsLetter(rune(src[j])) || unicode.IsDigit(rune(src[j])) || src[j] == '_') {
				j++
			}
			toks = append(toks, token{tIdent, src[i:j]})
			i = j
		case unicode.IsDigit(rune(c)):
			j := i
			for j < len(src) && (unicode.IsDigit(rune(src[j])) || unicode.IsLetter(rune(src[j]))) {
				j++
			}
			toks = append(toks, token{tInt, src[i:j]})
			i = j
		default:
			for _, op := range []string{"==", "!=", "&&", "||", "::", "<=", ">=", "<<", ">>"} {
				if strings.HasPrefix(src[i:], op) {
					toks = append(toks, token{tOp, op})
					i += len(op)
					goto next
				}
			}
			toks = append(toks, token{tOp, string(c)})
			i++
		next:
		}
	}
	return append(toks, token{tEOF, ""}), nil
}

// ---- parser ----

type node struct {
	op   string  // "lit", "ident", "call", "bin", "un", "index", "slice", "if", "tuple", "set", "in", "member"
	v    value   // for lit
	s    string  // ident / func / operator name
	args []*node // operands
}

type stmt struct {
	kind string // "let", "assign", "if", "call", "case"
	name []string
	expr *node // let/assign value, if/case subject, call
	then []stmt
	els  []stmt
	// case arms: each when-clause's patterns and body; els is otherwise.
	arms []caseArm
}

type caseArm struct {
	pats []*node
	body []stmt
}

type program struct {
	stmts []stmt
}

type parser struct {
	toks []token
	pos  int
	// bitsMode reads bare numbers as bit patterns: the XML bitdiffs and
	// field constraints write "Rd != 1111" without quotes.
	bitsMode bool
}

func (p *parser) peek() token { return p.toks[p.pos] }
func (p *parser) next() token { t := p.toks[p.pos]; p.pos++; return t }
func (p *parser) is(k tokKind, s string) bool {
	t := p.peek()
	return t.k == k && t.s == s
}
func (p *parser) accept(k tokKind, s string) bool {
	if p.is(k, s) {
		p.pos++
		return true
	}
	return false
}
func (p *parser) expect(k tokKind, s string) error {
	if !p.accept(k, s) {
		return fmt.Errorf("expected %q, got %q", s, p.peek().s)
	}
	return nil
}

func parseProgram(src string) (*program, error) {
	toks, err := lex(src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	var stmts []stmt
	for p.peek().k != tEOF {
		s, err := p.stmt()
		if err != nil {
			return nil, fmt.Errorf("%v (at token %d of %q)", err, p.pos, src)
		}
		stmts = append(stmts, s)
	}
	return &program{stmts}, nil
}

func parseExpr(src string) (*node, error) { return parse(src, false) }

// parseCond parses a bitdiffs/constraint expression, where numbers are
// bit patterns.
func parseCond(src string) (*node, error) { return parse(src, true) }

func parse(src string, bitsMode bool) (*node, error) {
	toks, err := lex(src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks, bitsMode: bitsMode}
	n, err := p.expr()
	if err != nil {
		return nil, err
	}
	if p.peek().k != tEOF {
		return nil, fmt.Errorf("trailing tokens in %q", src)
	}
	return n, nil
}

// skipType skips a type annotation: an identifier optionally followed by
// a balanced (...) or {...}, or a parenthesised tuple of types.
func (p *parser) skipType() {
	depth := 0
	for {
		t := p.peek()
		if t.k == tEOF {
			return
		}
		if depth == 0 && t.k == tOp && (t.s == "=" || t.s == ";" || t.s == ")" || t.s == ",") {
			return
		}
		if t.k == tOp && (t.s == "(" || t.s == "{") {
			depth++
		}
		if t.k == tOp && (t.s == ")" || t.s == "}") {
			depth--
		}
		p.next()
	}
}

func (p *parser) stmt() (stmt, error) {
	switch {
	case p.accept(tIdent, "let"), p.accept(tIdent, "constant"), p.accept(tIdent, "var"):
		isVar := p.toks[p.pos-1].s == "var"
		var names []string
		if p.accept(tOp, "(") {
			for {
				names = append(names, p.next().s)
				if p.accept(tOp, ")") {
					break
				}
				if err := p.expect(tOp, ","); err != nil {
					return stmt{}, err
				}
			}
		} else {
			names = append(names, p.next().s)
		}
		if p.accept(tOp, ":") {
			p.skipType()
		}
		if isVar && p.accept(tOp, ";") {
			// A bare declaration; the value is assigned later.
			return stmt{kind: "let", name: names, expr: &node{op: "lit", v: mkInt(0)}}, nil
		}
		if err := p.expect(tOp, "="); err != nil {
			return stmt{}, err
		}
		e, err := p.expr()
		if err != nil {
			return stmt{}, err
		}
		p.accept(tOp, ";")
		return stmt{kind: "let", name: names, expr: e}, nil
	case p.accept(tIdent, "case"):
		subject, err := p.expr()
		if err != nil {
			return stmt{}, err
		}
		if err := p.expect(tIdent, "of"); err != nil {
			return stmt{}, err
		}
		s := stmt{kind: "case", expr: subject}
		for {
			switch {
			case p.accept(tIdent, "when"):
				var arm caseArm
				for {
					pat, err := p.expr()
					if err != nil {
						return stmt{}, err
					}
					arm.pats = append(arm.pats, pat)
					if !p.accept(tOp, ",") {
						break
					}
				}
				p.accept(tOp, "=")
				p.accept(tOp, ">")
				for !p.is(tIdent, "when") && !p.is(tIdent, "otherwise") && !p.is(tIdent, "end") && p.peek().k != tEOF {
					t, err := p.stmt()
					if err != nil {
						return stmt{}, err
					}
					arm.body = append(arm.body, t)
				}
				s.arms = append(s.arms, arm)
			case p.accept(tIdent, "otherwise"):
				p.accept(tOp, "=")
				p.accept(tOp, ">")
				for !p.is(tIdent, "end") && p.peek().k != tEOF {
					t, err := p.stmt()
					if err != nil {
						return stmt{}, err
					}
					s.els = append(s.els, t)
				}
			default:
				if err := p.expect(tIdent, "end"); err != nil {
					return stmt{}, err
				}
				p.accept(tOp, ";")
				return s, nil
			}
		}
	case p.accept(tIdent, "if"):
		cond, err := p.expr()
		if err != nil {
			return stmt{}, err
		}
		if err := p.expect(tIdent, "then"); err != nil {
			return stmt{}, err
		}
		s := stmt{kind: "if", expr: cond}
		for !p.is(tIdent, "end") && !p.is(tIdent, "else") && !p.is(tIdent, "elsif") && p.peek().k != tEOF {
			t, err := p.stmt()
			if err != nil {
				return stmt{}, err
			}
			s.then = append(s.then, t)
		}
		if p.is(tIdent, "elsif") {
			p.toks[p.pos].s = "if" // rewrite and parse the tail as a nested if
			t, err := p.stmt()
			if err != nil {
				return stmt{}, err
			}
			s.els = []stmt{t}
			return s, nil // nested if consumed the end
		}
		if p.accept(tIdent, "else") {
			for !p.is(tIdent, "end") && p.peek().k != tEOF {
				t, err := p.stmt()
				if err != nil {
					return stmt{}, err
				}
				s.els = append(s.els, t)
			}
		}
		if err := p.expect(tIdent, "end"); err != nil {
			return stmt{}, err
		}
		p.accept(tOp, ";")
		return s, nil
	default:
		e, err := p.expr()
		if err != nil {
			return stmt{}, err
		}
		if p.accept(tOp, "=") {
			rhs, err := p.expr()
			if err != nil {
				return stmt{}, err
			}
			p.accept(tOp, ";")
			if e.op != "ident" {
				return stmt{}, fmt.Errorf("unsupported assignment target")
			}
			return stmt{kind: "assign", name: []string{e.s}, expr: rhs}, nil
		}
		p.accept(tOp, ";")
		return stmt{kind: "call", expr: e}, nil
	}
}

var binPrec = map[string]int{
	"||": 1, "OR": 1,
	"&&": 2, "AND": 2,
	"==": 3, "!=": 3, "<": 3, "<=": 3, ">": 3, ">=": 3, "IN": 3,
	"::": 4,
	"+":  5, "-": 5, "XOR": 5, "EOR": 5,
	"*": 6, "/": 6, "DIV": 6, "DIVRM": 6, "MOD": 6, "REM": 6, "<<": 6, ">>": 6,
}

func (p *parser) expr() (*node, error) { return p.binary(1) }

func (p *parser) binary(minPrec int) (*node, error) {
	lhs, err := p.unary()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		if t.k != tOp && t.k != tIdent {
			return lhs, nil
		}
		prec, ok := binPrec[t.s]
		if !ok || prec < minPrec {
			return lhs, nil
		}
		p.next()
		if t.s == "IN" {
			if err := p.expect(tOp, "{"); err != nil {
				return nil, err
			}
			set := &node{op: "set"}
			for !p.accept(tOp, "}") {
				e, err := p.expr()
				if err != nil {
					return nil, err
				}
				set.args = append(set.args, e)
				p.accept(tOp, ",")
			}
			lhs = &node{op: "in", args: []*node{lhs, set}}
			continue
		}
		rhs, err := p.binary(prec + 1)
		if err != nil {
			return nil, err
		}
		lhs = &node{op: "bin", s: t.s, args: []*node{lhs, rhs}}
	}
}

func (p *parser) unary() (*node, error) {
	switch {
	case p.accept(tOp, "!"):
		e, err := p.unary()
		return &node{op: "un", s: "!", args: []*node{e}}, err
	case p.accept(tOp, "-"):
		e, err := p.unary()
		return &node{op: "un", s: "-", args: []*node{e}}, err
	case p.accept(tIdent, "NOT"):
		e, err := p.unary()
		return &node{op: "un", s: "NOT", args: []*node{e}}, err
	}
	return p.postfix()
}

func (p *parser) postfix() (*node, error) {
	n, err := p.primary()
	if err != nil {
		return nil, err
	}
	for {
		switch {
		case p.accept(tOp, "["):
			hi, err := p.expr()
			if err != nil {
				return nil, err
			}
			if p.accept(tOp, ":") {
				lo, err := p.expr()
				if err != nil {
					return nil, err
				}
				n = &node{op: "slice", args: []*node{n, hi, lo}}
			} else if p.accept(tOp, ",") {
				// x[a,b]: bit a concatenated with bit b.
				parts := &node{op: "bits", args: []*node{n, hi}}
				for {
					idx, err := p.expr()
					if err != nil {
						return nil, err
					}
					parts.args = append(parts.args, idx)
					if !p.accept(tOp, ",") {
						break
					}
				}
				n = parts
			} else {
				n = &node{op: "index", args: []*node{n, hi}}
			}
			if err := p.expect(tOp, "]"); err != nil {
				return nil, err
			}
		case p.accept(tOp, "."):
			n = &node{op: "member", s: p.next().s, args: []*node{n}}
		default:
			return n, nil
		}
	}
}

func (p *parser) primary() (*node, error) {
	t := p.next()
	switch t.k {
	case tInt:
		if p.bitsMode {
			return &node{op: "lit", v: value{k: kBits, n: len(t.s), s: t.s, i: int64(bitsValue(t.s))}}, nil
		}
		i, err := strconv.ParseInt(t.s, 0, 64)
		if err != nil {
			return nil, err
		}
		return &node{op: "lit", v: mkInt(i)}, nil
	case tBits:
		return &node{op: "lit", v: value{k: kBits, n: len(t.s), s: t.s, i: int64(bitsValue(t.s))}}, nil
	case tString:
		return &node{op: "lit", v: value{k: kString, s: t.s}}, nil
	case tOp:
		if t.s == "(" {
			e, err := p.expr()
			if err != nil {
				return nil, err
			}
			if p.accept(tOp, ",") {
				tup := &node{op: "tuple", args: []*node{e}}
				for {
					e, err := p.expr()
					if err != nil {
						return nil, err
					}
					tup.args = append(tup.args, e)
					if !p.accept(tOp, ",") {
						break
					}
				}
				e = tup
			}
			return e, p.expect(tOp, ")")
		}
		return nil, fmt.Errorf("unexpected %q", t.s)
	case tIdent:
		switch t.s {
		case "TRUE":
			return &node{op: "lit", v: mkBool(true)}, nil
		case "FALSE":
			return &node{op: "lit", v: mkBool(false)}, nil
		case "if":
			c, err := p.expr()
			if err != nil {
				return nil, err
			}
			if err := p.expect(tIdent, "then"); err != nil {
				return nil, err
			}
			a, err := p.expr()
			if err != nil {
				return nil, err
			}
			if err := p.expect(tIdent, "else"); err != nil {
				return nil, err
			}
			b, err := p.expr()
			if err != nil {
				return nil, err
			}
			return &node{op: "if", args: []*node{c, a, b}}, nil
		case "ARBITRARY", "UNKNOWN":
			if p.accept(tOp, ":") {
				p.skipType()
			}
			return &node{op: "lit", v: mkInt(0)}, nil
		}
		// Template arguments, e.g. ZeroExtend{}(x) or ZeroExtend{32}(x);
		// Zeros{} has no argument list at all.
		templated := false
		if p.accept(tOp, "{") {
			templated = true
			for !p.accept(tOp, "}") {
				p.next()
			}
		}
		if p.accept(tOp, "(") {
			call := &node{op: "call", s: t.s}
			for !p.accept(tOp, ")") {
				e, err := p.expr()
				if err != nil {
					return nil, err
				}
				call.args = append(call.args, e)
				p.accept(tOp, ",")
			}
			return call, nil
		}
		if templated {
			return &node{op: "call", s: t.s}, nil
		}
		return &node{op: "ident", s: t.s}, nil
	}
	return nil, fmt.Errorf("unexpected end of expression")
}

func bitsValue(s string) uint64 {
	var v uint64
	for _, c := range s {
		v <<= 1
		if c == '1' {
			v |= 1
		}
	}
	return v
}

// ---- evaluation ----

type env struct {
	vars map[string]value
	// IT block state of the decoder at this instruction.
	inIT, lastInIT bool
}

func (e *env) set(name string, v value) { e.vars[name] = v }

func (e *env) get(name string) (value, bool) {
	v, ok := e.vars[name]
	return v, ok
}

func (e *env) run(p *program) error {
	return e.runStmts(p.stmts)
}

func (e *env) runStmts(stmts []stmt) error {
	for i := range stmts {
		if err := e.runStmt(&stmts[i]); err != nil {
			return err
		}
	}
	return nil
}

func (e *env) runStmt(s *stmt) error {
	switch s.kind {
	case "let", "assign":
		v, err := e.eval(s.expr)
		if err != nil {
			return err
		}
		if len(s.name) == 1 {
			e.set(s.name[0], v)
			return nil
		}
		if v.k != kTuple || len(v.t) != len(s.name) {
			return fmt.Errorf("tuple arity mismatch for %v", s.name)
		}
		for i, n := range s.name {
			e.set(n, v.t[i])
		}
		return nil
	case "if":
		c, err := e.eval(s.expr)
		if err != nil {
			return err
		}
		if c.truthy() {
			return e.runStmts(s.then)
		}
		return e.runStmts(s.els)
	case "call":
		_, err := e.eval(s.expr)
		return err
	case "case":
		v, err := e.eval(s.expr)
		if err != nil {
			return err
		}
		for _, arm := range s.arms {
			for _, pat := range arm.pats {
				m, err := e.eval(pat)
				if err != nil {
					return err
				}
				if equal(v, m) {
					return e.runStmts(arm.body)
				}
			}
		}
		return e.runStmts(s.els)
	}
	return fmt.Errorf("unknown statement %q", s.kind)
}

func (e *env) eval(n *node) (value, error) {
	switch n.op {
	case "lit":
		return n.v, nil
	case "ident":
		if v, ok := e.get(n.s); ok {
			return v, nil
		}
		// Enumerations (SRType_LSL, InstrSet_T32, FEAT_CRC32) and system
		// state (PSTATE) are names that only matter by identity.
		return value{k: kEnum, s: n.s}, nil
	case "member":
		// System state is unknown at decode time. Zero is the harmless
		// guess (PSTATE.C only feeds a flag, FPSCR.Len/Stride zero means
		// scalar); EDSCR.HDE=1 keeps HLT decodable.
		if n.s == "HDE" {
			return mkBits(1, 1), nil
		}
		return mkBits(0, 1), nil
	case "bits":
		v, err := e.eval(n.args[0])
		if err != nil {
			return value{}, err
		}
		var out uint64
		for _, a := range n.args[1:] {
			idx, err := e.eval(a)
			if err != nil {
				return value{}, err
			}
			out = out<<1 | v.uint()>>uint(idx.i)&1
		}
		return mkBits(out, len(n.args)-1), nil
	case "tuple":
		t := value{k: kTuple}
		for _, a := range n.args {
			v, err := e.eval(a)
			if err != nil {
				return value{}, err
			}
			t.t = append(t.t, v)
		}
		return t, nil
	case "if":
		c, err := e.eval(n.args[0])
		if err != nil {
			return value{}, err
		}
		if c.truthy() {
			return e.eval(n.args[1])
		}
		return e.eval(n.args[2])
	case "un":
		v, err := e.eval(n.args[0])
		if err != nil {
			return value{}, err
		}
		switch n.s {
		case "!":
			return mkBool(!v.truthy()), nil
		case "-":
			return mkInt(-v.i), nil
		case "NOT":
			if v.k == kBits {
				return mkBits(^v.uint(), v.n), nil
			}
			return mkBool(!v.truthy()), nil
		}
	case "index", "slice":
		v, err := e.eval(n.args[0])
		if err != nil {
			return value{}, err
		}
		hi, err := e.eval(n.args[1])
		if err != nil {
			return value{}, err
		}
		lo := hi
		if n.op == "slice" {
			if lo, err = e.eval(n.args[2]); err != nil {
				return value{}, err
			}
		}
		w := int(hi.i - lo.i + 1)
		return mkBits(v.uint()>>uint(lo.i), w), nil
	case "in":
		v, err := e.eval(n.args[0])
		if err != nil {
			return value{}, err
		}
		for _, a := range n.args[1].args {
			m, err := e.eval(a)
			if err != nil {
				return value{}, err
			}
			if equal(v, m) {
				return mkBool(true), nil
			}
		}
		return mkBool(false), nil
	case "bin":
		// Short-circuit booleans so that See() guards behave.
		if n.s == "&&" || n.s == "||" || n.s == "AND" || n.s == "OR" {
			a, err := e.eval(n.args[0])
			if err != nil {
				return value{}, err
			}
			if a.k == kBits {
				b, err := e.eval(n.args[1])
				if err != nil {
					return value{}, err
				}
				if n.s == "AND" || n.s == "&&" {
					return mkBits(a.uint()&b.uint(), a.n), nil
				}
				return mkBits(a.uint()|b.uint(), a.n), nil
			}
			if (n.s == "&&" || n.s == "AND") && !a.truthy() {
				return mkBool(false), nil
			}
			if (n.s == "||" || n.s == "OR") && a.truthy() {
				return mkBool(true), nil
			}
			b, err := e.eval(n.args[1])
			if err != nil {
				return value{}, err
			}
			return mkBool(b.truthy()), nil
		}
		a, err := e.eval(n.args[0])
		if err != nil {
			return value{}, err
		}
		b, err := e.eval(n.args[1])
		if err != nil {
			return value{}, err
		}
		switch n.s {
		case "==":
			return mkBool(equal(a, b)), nil
		case "!=":
			return mkBool(!equal(a, b)), nil
		case "<":
			return mkBool(a.i < b.i), nil
		case "<=":
			return mkBool(a.i <= b.i), nil
		case ">":
			return mkBool(a.i > b.i), nil
		case ">=":
			return mkBool(a.i >= b.i), nil
		case "::":
			return mkBits(a.uint()<<uint(b.n)|b.uint(), a.n+b.n), nil
		case "+":
			return mkInt(a.i + b.i), nil
		case "-":
			return mkInt(a.i - b.i), nil
		case "*":
			return mkInt(a.i * b.i), nil
		case "/", "DIV", "DIVRM":
			if b.i == 0 {
				return value{}, fmt.Errorf("division by zero")
			}
			q := a.i / b.i
			if n.s == "DIVRM" && (a.i%b.i != 0) && (a.i < 0) != (b.i < 0) {
				q-- // round towards minus infinity
			}
			return mkInt(q), nil
		case "MOD", "REM":
			if b.i == 0 {
				return value{}, fmt.Errorf("division by zero")
			}
			return mkInt(a.i % b.i), nil
		case "<<":
			return mkInt(a.i << uint(b.i)), nil
		case ">>":
			return mkInt(a.i >> uint(b.i)), nil
		case "XOR", "EOR":
			return mkBits(a.uint()^b.uint(), max(a.n, b.n)), nil
		}
		return value{}, fmt.Errorf("unknown operator %q", n.s)
	case "call":
		return e.call(n)
	}
	return value{}, fmt.Errorf("cannot evaluate %q", n.op)
}

// equal compares values; a bit-string literal may contain 'x' wildcards.
func equal(a, b value) bool {
	if a.k == kBits && b.k == kBits && (strings.ContainsRune(a.s, 'x') || strings.ContainsRune(b.s, 'x')) {
		pat, v := a, b
		if strings.ContainsRune(b.s, 'x') {
			pat, v = b, a
		}
		for i, c := range pat.s {
			bit := (v.uint() >> uint(pat.n-1-i)) & 1
			if c == '0' && bit != 0 || c == '1' && bit != 1 {
				return false
			}
		}
		return true
	}
	if a.k == kEnum || b.k == kEnum {
		return a.s == b.s
	}
	return a.i == b.i
}

func (e *env) call(n *node) (value, error) {
	args := make([]value, len(n.args))
	for i, a := range n.args {
		v, err := e.eval(a)
		if err != nil {
			return value{}, err
		}
		args[i] = v
	}
	arg := func(i int) value {
		if i < len(args) {
			return args[i]
		}
		return value{}
	}
	switch n.s {
	case "UInt":
		return mkInt(int64(arg(0).uint())), nil
	case "SInt":
		return mkInt(arg(0).sint()), nil
	case "ZeroExtend":
		return mkBits(arg(0).uint(), 32), nil
	case "SignExtend":
		return mkBits(uint64(arg(0).sint()), 32), nil
	case "Zeros":
		return mkBits(0, 32), nil
	case "Ones":
		return mkBits(^uint64(0), 32), nil
	case "NOT":
		v := arg(0)
		return mkBits(^v.uint(), v.n), nil
	case "BitCount":
		c := 0
		for v := arg(0).uint(); v != 0; v &= v - 1 {
			c++
		}
		return mkInt(int64(c)), nil
	case "IsZero":
		return mkBool(arg(0).uint() == 0), nil
	case "IsZeroBit":
		return mkBits(uint64(b2i(arg(0).uint() == 0)), 1), nil
	case "Align":
		a := arg(1).i
		return mkInt(arg(0).i &^ (a - 1)), nil
	case "LowestSetBit":
		v := arg(0).uint()
		for i := 0; i < 64; i++ {
			if v>>uint(i)&1 != 0 {
				return mkInt(int64(i)), nil
			}
		}
		return mkInt(int64(arg(0).n)), nil
	case "HighestSetBit":
		v := arg(0).uint()
		for i := 63; i >= 0; i-- {
			if v>>uint(i)&1 != 0 {
				return mkInt(int64(i)), nil
			}
		}
		return mkInt(-1), nil
	case "VFPExpandImm":
		return value{k: kFloat, f: vfpExpandImm(uint8(arg(0).uint()))}, nil
	case "T32ExpandImm":
		v, _ := t32ExpandImmC(uint32(arg(0).uint()), false)
		return mkBits(uint64(v), 32), nil
	case "T32ExpandImm_C", "A32ExpandImm_C":
		v, c := t32ExpandImmC(uint32(arg(0).uint()), arg(1).truthy())
		return value{k: kTuple, t: []value{mkBits(uint64(v), 32), mkBits(uint64(b2i(c)), 1)}}, nil
	case "DecodeImmShift":
		t, amount := decodeImmShift(uint8(arg(0).uint()), int(arg(1).uint()))
		return value{k: kTuple, t: []value{{k: kEnum, s: t}, mkInt(int64(amount))}}, nil
	case "DecodeRegShift":
		return value{k: kEnum, s: srTypes[arg(0).uint()&3]}, nil
	case "InITBlock":
		return mkBool(e.inIT), nil
	case "LastInITBlock":
		return mkBool(e.lastInIT), nil
	case "CurrentInstrSet":
		return value{k: kEnum, s: "InstrSet_T32"}, nil
	case "FPDecodeRM", "FPDecodeRounding":
		return value{k: kEnum, s: "FPRounding"}, nil
	case "IsFeatureImplemented", "HaveEL", "HaltingAllowed", "HaveAArch32EL", "ELUsingAArch32":
		return mkBool(true), nil
	case "UnpredictableProcedure", "ExecuteAsNOP", "EndOfInstruction", "ConstrainUnpredictable":
		return value{}, nil
	case "See", "Undefined", "Unpredictable", "UNDEFINED":
		return value{}, errReject
	}
	return value{}, fmt.Errorf("unknown function %q", n.s)
}

// vfpExpandImm decodes the 8-bit VFP immediate: sign, a 3-bit exponent
// around 2^0 and a 4-bit fraction, i.e. ±(16..31)/16 × 2^(-3..4).
func vfpExpandImm(imm8 uint8) float64 {
	frac := 1 + float64(imm8&0xf)/16
	exp := int(imm8>>4&3) - 3
	if imm8>>6&1 == 0 {
		exp = int(imm8>>4&3) + 1
	}
	v := frac * math.Ldexp(1, exp)
	if imm8>>7 != 0 {
		v = -v
	}
	return v
}

var srTypes = [...]string{"SRType_LSL", "SRType_LSR", "SRType_ASR", "SRType_ROR"}

func decodeImmShift(typ uint8, imm5 int) (string, int) {
	switch typ & 3 {
	case 0:
		return "SRType_LSL", imm5
	case 1:
		if imm5 == 0 {
			imm5 = 32
		}
		return "SRType_LSR", imm5
	case 2:
		if imm5 == 0 {
			imm5 = 32
		}
		return "SRType_ASR", imm5
	default:
		if imm5 == 0 {
			return "SRType_RRX", 1
		}
		return "SRType_ROR", imm5
	}
}

// t32ExpandImmC implements the T32 modified-immediate expansion.
func t32ExpandImmC(imm12 uint32, carry bool) (uint32, bool) {
	if imm12>>10&3 == 0 {
		imm8 := imm12 & 0xff
		switch imm12 >> 8 & 3 {
		case 0:
			return imm8, carry
		case 1:
			return imm8<<16 | imm8, carry
		case 2:
			return imm8<<24 | imm8<<8, carry
		default:
			return imm8<<24 | imm8<<16 | imm8<<8 | imm8, carry
		}
	}
	unrotated := 0x80 | imm12&0x7f
	rot := imm12 >> 7 & 0x1f
	v := unrotated>>rot | unrotated<<(32-rot)
	return v, v>>31 != 0
}
