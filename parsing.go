package genbase

import (
	"errors"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"strings"

	"golang.org/x/tools/go/types"
)

var (
	// ErrNotStructType shows argument is not ast.StructType.
	ErrNotStructType = errors.New("type is not ast.StructType")
)

// Parser is center of parsing strategy.
type Parser struct {
	SkipSemanticsCheck bool
}

// PackageInfo is specified package informations.
type PackageInfo struct {
	Dir   string
	Files FileInfos
	Types *types.Package
}

// FileInfo is ast.File synonym.
type FileInfo ast.File

// FileInfos is []*FileInfo synonym.
type FileInfos []*FileInfo

// TypeInfo is type information gathering.
// try http://goast.yuroyoro.net/ with http://play.golang.org/p/ruqMMsbDaw
type TypeInfo struct {
	FileInfo         *FileInfo
	GenDecl          *ast.GenDecl
	TypeSpec         *ast.TypeSpec
	AnnotatedComment *ast.Comment
}

// TypeInfos is []*TypeInfo synonym.
type TypeInfos []*TypeInfo

// StructTypeInfo is ast.StructType synonym.
type StructTypeInfo ast.StructType

// FieldInfo is ast.Field synonym.
type FieldInfo ast.Field

// FieldInfos is []*FieldInfo synonym.
type FieldInfos []*FieldInfo

// ParsePackageDir parses specified directory.
func (p *Parser) ParsePackageDir(directory string) (*PackageInfo, error) {
	pkg, err := build.Default.ImportDir(directory, 0)
	if err != nil {
		return nil, fmt.Errorf("cannot process directory %s: %s", directory, err)
	}
	var names []string
	names = append(names, pkg.GoFiles...)
	names = append(names, pkg.CgoFiles...)
	names = append(names, pkg.SFiles...)
	names = pathJoinAll(directory, names...)
	return p.parsePackage(directory, names, nil)
}

// ParsePackageFiles parses specified files.
func (p *Parser) ParsePackageFiles(fileNames []string) (*PackageInfo, error) {
	return p.parsePackage(".", fileNames, nil)
}

func (p *Parser) ParseStringSource(fileName string, code string) (*PackageInfo, error) {
	return p.parsePackage(".", []string{fileName}, []string{code})
}

func (p *Parser) parsePackage(directory string, fileNames []string, codes []string) (*PackageInfo, error) {
	var files FileInfos
	pkg := &PackageInfo{}
	fs := token.NewFileSet()
	for idx, fileName := range fileNames {
		if !strings.HasSuffix(fileName, ".go") {
			continue
		}
		var code interface{}
		if idx < len(codes) {
			code = codes[idx]
		}
		parsedFile, err := parser.ParseFile(fs, fileName, code, parser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("parsing package: %s: %s", fileName, err)
		}
		files = append(files, (*FileInfo)(parsedFile))
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("%s: no buildable Go files", directory)
	}
	pkg.Files = files
	pkg.Dir = directory

	// resolve types
	config := types.Config{
		FakeImportC:              true,
		IgnoreFuncBodies:         true,
		DisableUnusedImportCheck: true,
	}
	info := &types.Info{
		Defs: make(map[*ast.Ident]types.Object),
	}
	typesPkg, err := config.Check(pkg.Dir, fs, files.AstFiles(), info)
	if p.SkipSemanticsCheck && err != nil {
		return pkg, nil
	} else if err != nil {
		return nil, err
	}
	pkg.Types = typesPkg

	return pkg, nil
}

// TypeInfos is gathering TypeInfos, it included in package.
func (pkg *PackageInfo) TypeInfos() TypeInfos {
	var types TypeInfos
	for _, file := range pkg.Files {
		if file == nil {
			continue
		}
		ast.Inspect(file.AstFile(), func(node ast.Node) bool {
			decl, ok := node.(*ast.GenDecl)
			if !ok || decl.Tok != token.TYPE {
				return true
			}
			found := false
			for _, spec := range decl.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				types = append(types, &TypeInfo{
					FileInfo: file,
					GenDecl:  decl,
					TypeSpec: ts,
				})
				found = true
			}
			return !found
		})
	}
	return types
}

// CollectTaggedTypeInfos collects tagged TypeInfos.
func (pkg *PackageInfo) CollectTaggedTypeInfos(tag string) TypeInfos {
	ret := TypeInfos{}

	types := pkg.TypeInfos()

	for _, t := range types {
		if c := findAnnotation(t.Doc(), tag); c != nil {
			t.AnnotatedComment = c
			ret = append(ret, t)
		}
	}

	return ret
}

// CollectTypeInfos collects specified TypeInfos.
func (pkg *PackageInfo) CollectTypeInfos(typeNames []string) TypeInfos {
	ret := TypeInfos{}

	types := pkg.TypeInfos()

outer:
	for _, t := range types {
		for _, name := range typeNames {
			if t.Name() == name {
				ret = append(ret, t)
				continue outer
			}
		}
	}

	return ret
}

// Name returns package name.
func (pkg *PackageInfo) Name() string {
	return pkg.Files[0].Name.Name
}

// AstFile returns *ast.File.
func (file *FileInfo) AstFile() *ast.File {
	return (*ast.File)(file)
}

// AstFiles returns []*ast.File.
func (files FileInfos) AstFiles() []*ast.File {
	astFiles := make([]*ast.File, len(files))
	for i, file := range files {
		astFiles[i] = file.AstFile()
	}
	return astFiles
}

// FindImportSpecByIdent finds *ast.ImportSpec by package ident.
func (file *FileInfo) FindImportSpecByIdent(packageIdent string) *ast.ImportSpec {
	for _, imp := range file.Imports {
		if imp.Name != nil && imp.Name.Name == packageIdent {
			// import foo "foobar"
			return imp
		} else if strings.HasSuffix(imp.Path.Value, fmt.Sprintf(`/%s"`, packageIdent)) {
			// import "favclip/foo"
			return imp
		} else if imp.Path.Value == fmt.Sprintf(`"%s"`, packageIdent) {
			// import "foo"
			return imp
		}
	}
	return nil
}

// StructType returns *StructTypeInfo.
func (t *TypeInfo) StructType() (*StructTypeInfo, error) {
	structType, ok := interface{}(t.TypeSpec.Type).(*ast.StructType)
	if !ok {
		return nil, ErrNotStructType
	}

	return (*StructTypeInfo)(structType), nil
}

// Name return type name.
func (t *TypeInfo) Name() string {
	return t.TypeSpec.Name.Name
}

// Doc returns *ast.CommentGroup of TypeInfo.
func (t *TypeInfo) Doc() *ast.CommentGroup {
	if t.TypeSpec.Doc != nil {
		return t.TypeSpec.Doc
	}
	if t.GenDecl.Doc != nil {
		return t.GenDecl.Doc
	}
	return nil
}

// AstStructType returns *ast.StructType.
func (st *StructTypeInfo) AstStructType() *ast.StructType {
	return (*ast.StructType)(st)
}

// FieldInfos returns FieldInfos of struct.
func (st *StructTypeInfo) FieldInfos() FieldInfos {
	var fields FieldInfos
	for _, field := range st.AstStructType().Fields.List {
		fields = append(fields, (*FieldInfo)(field))
	}

	return fields
}

// TypeName returns type name of field.
func (f *FieldInfo) TypeName() string {
	typeName, err := ExprToTypeName(f.Type)
	if err != nil {
		return fmt.Sprintf("!!%s!!", err.Error())
	}
	return typeName
}

// IsPtr returns true if FieldInfo is pointer, otherwise returns false.
func (f *FieldInfo) IsPtr() bool {
	_, ok := f.Type.(*ast.StarExpr)
	return ok
}

// IsArray returns true if FieldInfo is array, otherwise returns false.
func (f *FieldInfo) IsArray() bool {
	_, ok := f.Type.(*ast.ArrayType)
	return ok
}

// IsPtrArray returns true if FieldInfo is pointer array, otherwise returns false.
func (f *FieldInfo) IsPtrArray() bool {
	star, ok := f.Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	_, ok = star.X.(*ast.ArrayType)
	return ok
}

// IsArrayPtr returns true if FieldInfo is pointer of array, otherwise returns false.
func (f *FieldInfo) IsArrayPtr() bool {
	array, ok := f.Type.(*ast.ArrayType)
	if !ok {
		return false
	}
	_, ok = array.Elt.(*ast.StarExpr)
	return ok
}

// IsPtrArrayPtr returns true if FieldInfo is pointer of pointer array, otherwise returns false.
func (f *FieldInfo) IsPtrArrayPtr() bool {
	star, ok := f.Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	array, ok := star.X.(*ast.ArrayType)
	if !ok {
		return false
	}
	_, ok = array.Elt.(*ast.StarExpr)
	return ok
}

// IsInt64 returns true if FieldInfo is int64, otherwise returns false.
func (f *FieldInfo) IsInt64() bool {
	typeName, err := ExprToBaseTypeName(f.Type)
	if err != nil {
		return false
	}
	return typeName == "int64"
}

// IsInt returns true if FieldInfo is int, otherwise returns false.
func (f *FieldInfo) IsInt() bool {
	typeName, err := ExprToBaseTypeName(f.Type)
	if err != nil {
		return false
	}
	return typeName == "int"
}

// IsString returns true if FieldInfo is string, otherwise returns false.
func (f *FieldInfo) IsString() bool {
	typeName, err := ExprToBaseTypeName(f.Type)
	if err != nil {
		return false
	}
	return typeName == "string"
}

// IsFloat32 returns true if FieldInfo is float32, otherwise returns false.
func (f *FieldInfo) IsFloat32() bool {
	typeName, err := ExprToBaseTypeName(f.Type)
	if err != nil {
		return false
	}
	return typeName == "float32"
}

// IsFloat64 returns true if FieldInfo is float64, otherwise returns false.
func (f *FieldInfo) IsFloat64() bool {
	typeName, err := ExprToBaseTypeName(f.Type)
	if err != nil {
		return false
	}
	return typeName == "float64"
}

// IsNumber returns true if FieldInfo is int or int64 or float32 or float64, otherwise returns false.
func (f *FieldInfo) IsNumber() bool {
	return f.IsInt() || f.IsInt64() || f.IsFloat32() || f.IsFloat64()
}

// IsBool returns true if FieldInfo is bool, otherwise returns false.
func (f *FieldInfo) IsBool() bool {
	typeName, err := ExprToBaseTypeName(f.Type)
	if err != nil {
		return false
	}
	return typeName == "bool"
}

// IsTime returns true if FieldInfo is time.Time, otherwise returns false.
func (f *FieldInfo) IsTime() bool {
	typeName, err := ExprToBaseTypeName(f.Type)
	if err != nil {
		return false
	}
	return typeName == "time.Time"
}
