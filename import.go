package gomv

import (
	"go/ast"
	"strconv"
	"strings"
)

func getUsedImports(f *ast.File, funcDecl *ast.FuncDecl) []*ast.ImportSpec {
	var usedImports []*ast.ImportSpec
	for _, importSpec := range f.Imports {
		if usesImport(funcDecl, importSpec) {
			usedImports = append(usedImports, importSpec)
		}
	}
	return usedImports
}

func usesImport(funcDecl *ast.FuncDecl, importSpec *ast.ImportSpec) bool {
	var importName string
	if importSpec.Name != nil {
		importName = importSpec.Name.Name
	} else {
		path, _ := strconv.Unquote(importSpec.Path.Value)
		lastIdx := strings.LastIndex(path, "/")
		if lastIdx == -1 {
			importName = path
		} else {
			importName = path[lastIdx+1:]
		}
	}
	if importName == "_" || importName == "." {
		// to be on safe side
		return true
	}
	used := false
	ast.Inspect(funcDecl, func(n ast.Node) bool {
		if used {
			return false
		}
		if selExp, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := selExp.X.(*ast.Ident); ok {
				if id.Name == importName && id.Obj == nil {
					used = true
					return false
				}
			}
		}
		return true
	})

	return used
}
