package gomv

import (
	"errors"
	"go/ast"
	"go/types"
	"strconv"
	"strings"

	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/packages"
)

type funcMoveInfo struct {
	srcPkg, dstPkg *packages.Package
	srcAst, dstAst *ast.File
	funcDecl       *ast.FuncDecl
	funcObj        types.Object
	refInfos       []nodeInfo
}

func (fm funcMoveInfo) isValid() bool {
	return fm.srcPkg != nil && fm.dstPkg != nil &&
		fm.srcAst != nil && fm.dstAst != nil &&
		fm.funcDecl != nil && fm.funcObj != nil
}

func MoveFunc(pkgs []*packages.Package, funcName, srcPkgName, dstFileName string) error {
	dstPkgName, err := getPackageName(dstFileName)
	if err != nil {
		return err
	}
	fm := funcMoveInfo{}

	var srcFileName string

	for _, pkg := range pkgs {
		if pkg.Name == srcPkgName {
			fm.srcPkg = pkg
			fm.funcDecl, fm.srcAst = searchFunc(pkg, funcName)
			srcFileName = pkg.Fset.Position(fm.srcAst.Package).Filename
			if fm.funcDecl != nil {
				fm.funcObj = pkg.TypesInfo.ObjectOf(fm.funcDecl.Name)
			}
		} else if pkg.Name == dstPkgName {
			fm.dstPkg = pkg
			for _, f := range pkg.Syntax {
				if pkg.Fset.Position(f.Package).Filename == dstFileName {
					fm.dstAst = f
				}
			}
		}
	}
	fm.refInfos = searchFuncRefs(pkgs, fm.funcObj)

	if !fm.isValid() {
		return errors.New("invalid case")
	}
	if srcFileName == dstFileName {
		return errors.New("no op")
	}
	if err := fm.move(); err != nil {
		return err
	}
	return nil
}

func (fm funcMoveInfo) move() error {
	funcName := fm.funcDecl.Name.Name
	expFuncName := strings.ToUpper(funcName[:1]) + funcName[1:]

	// Remove the function declaration from source AST
	for idx, decl := range fm.srcAst.Decls {
		if fDecl, ok := decl.(*ast.FuncDecl); ok {
			if fDecl == fm.funcDecl {
				fm.srcAst.Decls = append(fm.srcAst.Decls[:idx], fm.srcAst.Decls[idx+1:]...)
				break
			}
		}
	}

	// Add the function declaration to destination AST
	fm.funcDecl.Name = &ast.Ident{Name: expFuncName}
	fm.dstAst.Decls = append(fm.dstAst.Decls, fm.funcDecl)

	// Add comments within function
	if fm.funcDecl.Doc != nil {
		fm.dstAst.Comments = append(fm.dstAst.Comments, fm.funcDecl.Doc)
	}
	firstIdx, lastIdx := -1, -1
	for idx, commentGroup := range fm.srcAst.Comments {
		if commentGroup == fm.funcDecl.Doc {
			firstIdx = idx
		}
		if commentGroup.Pos() > fm.funcDecl.Pos() && commentGroup.End() < fm.funcDecl.End() {
			if firstIdx == -1 {
				firstIdx = idx
			}
			fm.dstAst.Comments = append(fm.dstAst.Comments, commentGroup)
			lastIdx = idx
		}
	}

	if firstIdx != -1 && lastIdx != -1 {
		// Remove comments of source AST
		fm.srcAst.Comments = append(fm.srcAst.Comments[:firstIdx], fm.srcAst.Comments[lastIdx+1:]...)
	}

	// Find the used import of the function and add those imports
	// in the destination AST
	dstFset := fm.dstPkg.Fset
	usedImports := getUsedImports(fm.srcAst, fm.funcDecl)
	for _, importSpec := range usedImports {
		path, _ := strconv.Unquote(importSpec.Path.Value)
		if importSpec.Name == nil {
			astutil.AddImport(dstFset, fm.dstAst, path)
		} else {
			astutil.AddNamedImport(dstFset, fm.dstAst, importSpec.Name.Name, path)
		}
	}

	if err := writeAstToFile(fm.srcPkg.Fset, fm.srcAst); err != nil {
		return err
	}

	// Update all the call expressions of the moved function
	for _, ref := range fm.refInfos {
		callExp, ok := ref.node.(*ast.CallExpr)
		if !ok {
			continue
		}
		if ref.file.Name.Name == fm.dstPkg.Name {
			// The call expression can be in the destination package.
			// In that case, no need to make selection expression and
			// add import.
			callExp.Fun = &ast.Ident{Name: expFuncName}
		} else {
			callExp.Fun = &ast.SelectorExpr{
				X:   &ast.Ident{Name: fm.dstPkg.Name},
				Sel: &ast.Ident{Name: expFuncName},
			}
			astutil.AddImport(ref.fset, ref.file, fm.dstPkg.ID)
		}

		if err := writeAstToFile(ref.fset, ref.file); err != nil {
			return err
		}
	}

	if err := writeAstToFile(dstFset, fm.dstAst); err != nil {
		return err
	}

	return nil
}
