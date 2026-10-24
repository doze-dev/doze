package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// trimParser drops function bodies of files outside root: deps are needed only for their API.
func trimParser(root string) func(*token.FileSet, string, []byte) (*ast.File, error) {
	return func(fset *token.FileSet, filename string, src []byte) (*ast.File, error) {
		f, err := parser.ParseFile(fset, filename, src, parser.AllErrors|parser.ParseComments)
		if strings.HasPrefix(filename, root) || f == nil {
			return f, err
		}
		f.Comments = nil
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok {
				fd.Body = nil
			}
		}
		return f, err
	}
}
