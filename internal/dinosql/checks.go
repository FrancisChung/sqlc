package dinosql

import (
	"fmt"
	"strings"

	"github.com/kyleconroy/sqlc/internal/catalog"
	"github.com/kyleconroy/sqlc/internal/pg"
	nodes "github.com/lfittl/pg_query_go/nodes"
)

func validateParamRef(n nodes.Node) error {
	var allrefs []nodes.ParamRef

	// Find all parameter references
	Walk(VisitorFunc(func(node nodes.Node) {
		switch n := node.(type) {
		case nodes.ParamRef:
			allrefs = append(allrefs, n)
		}
	}), n)

	seen := map[int]struct{}{}
	for _, r := range allrefs {
		seen[r.Number] = struct{}{}
	}

	for i := 1; i <= len(seen); i += 1 {
		if _, ok := seen[i]; !ok {
			return pg.Error{
				Code:    "42P18",
				Message: fmt.Sprintf("could not determine data type of parameter $%d", i),
			}
		}
	}
	return nil
}

type funcCallVisitor struct {
	catalog *pg.Catalog
	err     error
}

func (v *funcCallVisitor) Visit(node nodes.Node) Visitor {
	if v.err != nil {
		return nil
	}

	funcCall, ok := node.(nodes.FuncCall)
	if !ok {
		return v
	}

	fqn, err := catalog.ParseList(funcCall.Funcname)
	if err != nil {
		v.err = err
		return v
	}

	// Do not validate unknown functions
	funs, err := v.catalog.LookupFunctions(fqn)
	if err != nil {
		return v
	}

	args := len(funcCall.Args.Items)
	for _, fun := range funs {
		arity := fun.ArgN
		if fun.Arguments != nil {
			arity = len(fun.Arguments)
		}
		if arity == args {
			return v
		}
	}

	var sig []string
	for range funcCall.Args.Items {
		sig = append(sig, "unknown")
	}

	v.err = pg.Error{
		Code:     "42883",
		Message:  fmt.Sprintf("function %s(%s) does not exist", fqn.Rel, strings.Join(sig, ", ")),
		Hint:     "No function matches the given name and argument types. You might need to add explicit type casts.",
		Location: funcCall.Location,
	}

	return nil
}

func validateFuncCall(c *pg.Catalog, n nodes.Node) error {
	visitor := funcCallVisitor{catalog: c}
	Walk(&visitor, n)
	return visitor.err
}

func validateInsertStmt(stmt nodes.InsertStmt) error {
	sel, ok := stmt.SelectStmt.(nodes.SelectStmt)
	if !ok {
		return nil
	}
	if len(sel.ValuesLists) != 1 {
		return nil
	}

	colsLen := len(stmt.Cols.Items)
	valsLen := len(sel.ValuesLists[0])
	switch {
	case colsLen > valsLen:
		return pg.Error{
			Code:    "42601",
			Message: "INSERT has more target columns than expressions",
		}
	case colsLen < valsLen:
		return pg.Error{
			Code:    "42601",
			Message: "INSERT has more expressions than target columns",
		}
	}
	return nil
}
