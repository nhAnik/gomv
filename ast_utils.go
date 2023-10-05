package gomv

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/packages"
)

type astInfo struct {
	oldText string
	new     *ast.File
	fset    *token.FileSet
}

type nodeInfo struct {
	node ast.Node
	file *ast.File
	fset *token.FileSet
}

func getFileText(fset *token.FileSet, f *ast.File) ([]byte, error) {
	var buf bytes.Buffer
	if err := format.Node(&buf, fset, f); err != nil {
		return nil, err
	}
	bytes, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, err
	}
	return bytes, nil
}

func getObjectOf(pkg *packages.Package, callExp *ast.CallExpr) types.Object {
	switch funExp := callExp.Fun.(type) {
	case *ast.Ident:
		return pkg.TypesInfo.Uses[funExp]
	case *ast.SelectorExpr:
		return pkg.TypesInfo.Uses[funExp.Sel]
	default:
		return nil
	}
}

func getPackageName(fileName string) (string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, fileName, nil, parser.ParseComments)
	if err != nil {
		return "", err
	}
	return f.Name.Name, nil
}

func searchFuncRefs(pkgs []*packages.Package, funcObj types.Object) []nodeInfo {
	var infos []nodeInfo
	for _, pkg := range pkgs {
		for _, f := range pkg.Syntax {
			ast.Inspect(f, func(nod ast.Node) bool {
				if callExp, ok := nod.(*ast.CallExpr); ok {
					if getObjectOf(pkg, callExp) == funcObj {
						infos = append(infos, nodeInfo{node: callExp, file: f, fset: pkg.Fset})
					}
				}
				return true
			})
		}
	}
	return infos
}

func searchFunc(pkg *packages.Package, funcName string) (*ast.FuncDecl, *ast.File) {
	var matchedFunc *ast.FuncDecl
	var file *ast.File
	matchFound := false
	for _, f := range pkg.Syntax {
		if matchFound {
			return matchedFunc, file
		}
		ast.Inspect(f, func(nod ast.Node) bool {
			if matchFound {
				return false
			}
			if funcNode, ok := nod.(*ast.FuncDecl); ok {
				if funcNode.Name.Name == funcName {
					matchedFunc = funcNode
					file = f
					matchFound = true
					return false
				}
			}
			return true
		})
	}
	return matchedFunc, file
}
