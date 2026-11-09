package goobj

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
)

// goArgNames lays out the arguments and results of the Go function
// named fn, declared in the file at path, as Go's wasm port passes them
// on the stack, and names each by its offset from the first argument:
// a.ptr, a.len and a.cap for a slice a. A result without a name is r0,
// r1 and so on. It is nil when the declaration isn't found or has a
// type whose size can't be told without type checking, such as a
// struct or a named type.
func goArgNames(path, fn string) map[int64]string {
	if path == "" {
		return nil
	}
	// main.dot, main.(*T).M or main.T.M, with the import path first.
	name := fn[strings.LastIndexByte(fn, '/')+1:]
	_, name, ok := strings.Cut(name, ".")
	if !ok {
		return nil
	}
	recv, method, isMethod := strings.Cut(name, ".")
	if !isMethod {
		recv, method = "", name
	}
	recv = strings.TrimSuffix(strings.TrimPrefix(recv, "("), ")")

	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil
	}
	var decl *ast.FuncDecl
	for _, d := range file.Decls {
		f, ok := d.(*ast.FuncDecl)
		if !ok || f.Name.Name != method || f.Type.TypeParams != nil {
			continue
		}
		if (f.Recv == nil) != (recv == "") {
			continue
		}
		if f.Recv != nil {
			// The receiver must be a pointer, which has a known size.
			star, ok := f.Recv.List[0].Type.(*ast.StarExpr)
			if !ok || "*"+exprName(star.X) != recv {
				continue
			}
		}
		decl = f
	}
	if decl == nil {
		return nil
	}

	names := map[int64]string{}
	var off int64
	place := func(name string, typ ast.Expr) bool {
		size, align, parts := goTypeLayout(typ)
		if size < 0 {
			return false
		}
		off = (off + align - 1) / align * align
		if name != "_" {
			for _, p := range parts {
				names[off+p.off] = name + p.suffix
			}
		}
		off += size
		return true
	}
	fields := func(list *ast.FieldList, unnamed func(i int) string) bool {
		if list == nil {
			return true
		}
		i := 0
		for _, f := range list.List {
			if len(f.Names) == 0 {
				if !place(unnamed(i), f.Type) {
					return false
				}
				i++
			}
			for _, n := range f.Names {
				if !place(n.Name, f.Type) {
					return false
				}
				i++
			}
		}
		return true
	}
	if decl.Recv != nil && !fields(decl.Recv, func(int) string { return "_" }) {
		return nil
	}
	if !fields(decl.Type.Params, func(int) string { return "_" }) {
		return nil
	}
	// The results start at a pointer's alignment after the arguments.
	off = (off + 7) / 8 * 8
	if !fields(decl.Type.Results, func(i int) string { return "r" + strconv.Itoa(i) }) {
		return nil
	}
	return names
}

// goPart is a word of a value: a slice's len is 8 bytes into it.
type goPart struct {
	off    int64
	suffix string
}

// goTypeLayout is the size and alignment of a type on wasm, where a
// pointer is 8 bytes, and the words worth naming in it. The size is -1
// for a type it can't tell without type checking.
func goTypeLayout(typ ast.Expr) (size, align int64, parts []goPart) {
	whole := []goPart{{0, ""}}
	switch t := typ.(type) {
	case *ast.Ident:
		switch t.Name {
		case "bool", "int8", "uint8", "byte":
			return 1, 1, whole
		case "int16", "uint16":
			return 2, 2, whole
		case "int32", "uint32", "float32", "rune":
			return 4, 4, whole
		case "int", "uint", "int64", "uint64", "float64", "uintptr":
			return 8, 8, whole
		case "complex64":
			return 8, 4, whole
		case "complex128":
			return 16, 8, whole
		case "string":
			return 16, 8, []goPart{{0, ".ptr"}, {8, ".len"}}
		case "any", "error":
			return 16, 8, []goPart{{0, ".type"}, {8, ".data"}}
		}
	case *ast.StarExpr, *ast.MapType, *ast.ChanType, *ast.FuncType:
		return 8, 8, whole
	case *ast.SelectorExpr:
		if exprName(t) == "unsafe.Pointer" {
			return 8, 8, whole
		}
	case *ast.InterfaceType:
		return 16, 8, []goPart{{0, ".type"}, {8, ".data"}}
	case *ast.Ellipsis:
		return 24, 8, []goPart{{0, ".ptr"}, {8, ".len"}, {16, ".cap"}}
	case *ast.ArrayType:
		if t.Len == nil {
			return 24, 8, []goPart{{0, ".ptr"}, {8, ".len"}, {16, ".cap"}}
		}
		lit, ok := t.Len.(*ast.BasicLit)
		n, err := strconv.ParseInt(strings.ReplaceAll(litValue(lit, ok), "_", ""), 0, 64)
		size, align, _ := goTypeLayout(t.Elt)
		if err != nil || size < 0 || n < 0 || n > 1<<20 {
			break
		}
		return n * size, align, whole
	case *ast.ParenExpr:
		return goTypeLayout(t.X)
	}
	return -1, 1, nil
}

func litValue(lit *ast.BasicLit, ok bool) string {
	if !ok || lit.Kind != token.INT {
		return ""
	}
	return lit.Value
}

// exprName spells an identifier or a qualified one.
func exprName(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return exprName(e.X) + "." + e.Sel.Name
	}
	return ""
}
