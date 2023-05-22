package main

import (
	"fmt"
	"go/types"
	"os"
	"reflect"

	"github.com/rossmacarthur/cases"
	"github.com/rossmacarthur/fudge"
	"github.com/rossmacarthur/fudge/errors"
	"golang.org/x/tools/go/packages"
)

func parseConfig() (*Config, error) {
	pkg, err := loadPackage(".")
	if err != nil {
		return nil, err
	}

	st, err := loadStruct(pkg, "glean")
	if err != nil {
		return nil, err
	}

	var fields []Field

	first := st.Field(0)
	if first == nil || !first.Embedded() {
		return nil, errors.New("first field must be an embedded struct")
	}

	embedded, ok := first.Type().Underlying().(*types.Struct)
	if !ok {
		return nil, errors.New("embedded type is not a struct")
	}

	for i := 0; i < embedded.NumFields(); i++ {
		field := embedded.Field(i)
		if field.Embedded() {
			return nil, errors.New("nested embedded fields are not supported")
		}

		fields = append(fields, Field{
			Name:   field.Name(),
			Column: cases.ToSnake(field.Name()),
		})
	}

	ft, ok := first.Type().(*types.Named)
	if !ok {
		return nil, errors.New("embedded type is not a named type")
	}

	outputType := fmt.Sprintf("%s.%s", ft.Obj().Pkg().Name(), ft.Obj().Name())
	outputImport := ft.Obj().Pkg().Path()

	for i := 1; i < st.NumFields(); i++ {
		field := st.Field(i)

		// Find the field from the embedded struct
		idx := -1
		for j := range fields {
			if fields[j].Name == field.Name() {
				idx = j
				break
			}
		}
		if idx < 0 {
			return nil, errors.New("could not find field in embedded struct", fudge.KV("field_name", field.Name()))
		}

		// Update the field
		f := &fields[idx]
		tag := reflect.StructTag(st.Tag(i)).Get("glean")

		if tag == "-" {
			// Remove field from the list
			fields = append(fields[:idx], fields[idx+1:]...)
			continue
		} else if tag == "" {
			f.Column = cases.ToSnake(field.Name())
		} else {
			f.Column = tag
		}

		f.Accessor = getAccessor(field)
	}

	fields[0].First = true

	return &Config{
		PackageName:  pkg.Name,
		GenSource:    getGenSource(),
		BackTick:     "`",
		TableName:    *table,
		OutputType:   outputType,
		OutputImport: outputImport,
		Fields:       fields,
	}, nil
}

func getGenSource() string {
	f := os.Getenv("GOFILE")
	l := os.Getenv("GOLINE")
	if f == "" || l == "" {
		return ""
	}
	return fmt.Sprintf("%s:%s", f, l)
}

var commons = map[string]string{
	"sql.NullBool":    ".Bool",
	"sql.NullInt32":   ".Int32",
	"sql.NullInt64":   ".Int64",
	"sql.NullFloat64": ".Float64",
	"sql.NullString":  ".String",
	"sql.NullTime":    ".Time",
}

func getAccessor(field *types.Var) string {
	t, ok := field.Type().(*types.Named)
	if !ok {
		return ""
	}
	key := fmt.Sprintf("%s.%s", t.Obj().Pkg().Name(), t.Obj().Name())
	return commons[key]
}

func loadPackage(name string) (*packages.Package, error) {
	pkgs, err := packages.Load(
		&packages.Config{
			Mode: packages.NeedName |
				packages.NeedImports |
				packages.NeedDeps |
				packages.NeedTypes |
				packages.NeedTypesInfo |
				packages.NeedSyntax,
		}, ".")
	if err != nil {
		return nil, errors.Wrap(err, "failed to load package", fudge.KV("package_name", name))
	}
	if len(pkgs) != 1 {
		return nil, errors.New("expected one package", fudge.KV("package_count", len(pkgs)))
	}
	return pkgs[0], nil
}

func loadStruct(pkg *packages.Package, name string) (*types.Struct, error) {
	obj := pkg.Types.Scope().Lookup(name)
	if obj == nil {
		return nil, errors.New("failed to find type by name", fudge.KV("struct_name", name))
	}
	st, ok := obj.Type().Underlying().(*types.Struct)
	if !ok {
		return nil, errors.New("expected type to be a struct", fudge.MKV{
			"struct_name": name,
			"actual_type": obj.Type().Underlying().String(),
		})
	}
	return st, nil
}
