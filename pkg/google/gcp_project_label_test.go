package google

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// metricConstructors lists every helper that defines a Prometheus metric in
// this codebase. The detector below resolves the fqname produced by each call
// and enforces the `project` label invariant on any metric whose name starts
// with `cloudcost_gcp_`.
var metricConstructors = map[string]struct{}{
	"NewDesc":         {},
	"GenerateDesc":    {},
	"NewGaugeVec":     {},
	"NewCounterVec":   {},
	"NewHistogramVec": {},
	"NewSummaryVec":   {},
}

// Resolution of the well-known exporter-level prefix constants. Both live in
// the root `cloudcost_exporter` package and are stable.
var knownPrefixConstants = map[string]string{
	"MetricPrefix": "cloudcost",
	"ExporterName": "cloudcost_exporter",
}

// TestAllGCPCostMetricsHaveProjectLabel statically scans every metric
// constructor invocation under pkg/google, resolves each metric's fqname, and
// fails if any metric named `cloudcost_gcp_*` is missing the `project`
// variable label. New metrics are picked up automatically.
func TestAllGCPCostMetricsHaveProjectLabel(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "could not locate test source path")
	pkgRoot := filepath.Dir(thisFile)

	fset := token.NewFileSet()
	var failures []string
	costMetricsChecked := 0

	walkErr := filepath.WalkDir(pkgRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		consts := collectStringConstants(file)

		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			fnName, ok := selectorName(call.Fun)
			if !ok {
				return true
			}
			if _, isConstructor := metricConstructors[fnName]; !isConstructor {
				return true
			}

			fqname, fqnameResolved := resolveMetricFQName(call, fnName, consts)
			if !fqnameResolved || !strings.HasPrefix(fqname, "cloudcost_gcp_") {
				return true
			}

			costMetricsChecked++
			pos := fset.Position(call.Pos())
			labels, labelsResolved := extractStringSliceLiteral(call.Args)
			if !labelsResolved {
				failures = append(failures, fmt.Sprintf(
					"%s: metric %q labels are not an inline []string{...} literal; cannot statically verify `project`",
					pos, fqname))
				return true
			}
			if !slices.Contains(labels, "project") {
				failures = append(failures, fmt.Sprintf(
					"%s: metric %q is missing the `project` variable label; got %v",
					pos, fqname, labels))
			}
			return true
		})
		return nil
	})
	require.NoError(t, walkErr)

	require.Greater(t, costMetricsChecked, 0,
		"static analysis matched zero `cloudcost_gcp_*` metrics — the detector likely broke")

	if len(failures) > 0 {
		t.Fatalf("found %d cloudcost_gcp_* metric(s) without a `project` label:\n  %s",
			len(failures), strings.Join(failures, "\n  "))
	}
}

// resolveMetricFQName figures out what fqname a metric-constructor call will
// produce. Returns ("", false) when the prefix or subsystem cannot be
// determined statically (e.g. computed at runtime from non-literal data).
func resolveMetricFQName(call *ast.CallExpr, fnName string, consts map[string]string) (string, bool) {
	prefix, subsystem, ok := findFQNameParts(call, fnName)
	if !ok {
		return "", false
	}
	prefixStr, ok := resolvePrefix(prefix, consts)
	if !ok {
		return "", false
	}
	subsystemStr, ok := resolveSubsystem(subsystem, consts)
	if !ok {
		return "", false
	}
	return prefixStr + "_" + subsystemStr, true
}

// findFQNameParts locates the prefix and subsystem expressions for a metric
// constructor call. GenerateDesc takes them as positional arguments; the
// prometheus.* helpers wrap them inside a BuildFQName call (directly for
// NewDesc, or via the Name field of a *Opts struct literal for the Vec
// helpers).
func findFQNameParts(call *ast.CallExpr, fnName string) (prefix, subsystem ast.Expr, ok bool) {
	if fnName == "GenerateDesc" {
		if len(call.Args) < 3 {
			return nil, nil, false
		}
		return call.Args[0], call.Args[1], true
	}

	var buildCall *ast.CallExpr
	if fnName == "NewDesc" {
		if len(call.Args) < 1 {
			return nil, nil, false
		}
		bc, ok := call.Args[0].(*ast.CallExpr)
		if !ok {
			return nil, nil, false
		}
		buildCall = bc
	} else {
		// Vec helpers: first arg is an Opts composite literal; locate the
		// Name field's BuildFQName call.
		if len(call.Args) < 1 {
			return nil, nil, false
		}
		opts, ok := call.Args[0].(*ast.CompositeLit)
		if !ok {
			return nil, nil, false
		}
		for _, elt := range opts.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			ident, ok := kv.Key.(*ast.Ident)
			if !ok || ident.Name != "Name" {
				continue
			}
			bc, ok := kv.Value.(*ast.CallExpr)
			if !ok {
				continue
			}
			buildCall = bc
			break
		}
	}

	if buildCall == nil {
		return nil, nil, false
	}
	name, ok := selectorName(buildCall.Fun)
	if !ok || name != "BuildFQName" || len(buildCall.Args) < 2 {
		return nil, nil, false
	}
	return buildCall.Args[0], buildCall.Args[1], true
}

func resolvePrefix(expr ast.Expr, consts map[string]string) (string, bool) {
	if s, ok := stringLiteralValue(expr); ok {
		return s, true
	}
	if sel, ok := expr.(*ast.SelectorExpr); ok {
		if v, hit := knownPrefixConstants[sel.Sel.Name]; hit {
			return v, true
		}
	}
	if ident, ok := expr.(*ast.Ident); ok {
		if v, hit := consts[ident.Name]; hit {
			return v, true
		}
		if v, hit := knownPrefixConstants[ident.Name]; hit {
			return v, true
		}
	}
	return "", false
}

func resolveSubsystem(expr ast.Expr, consts map[string]string) (string, bool) {
	if s, ok := stringLiteralValue(expr); ok {
		return s, true
	}
	if ident, ok := expr.(*ast.Ident); ok {
		if v, hit := consts[ident.Name]; hit {
			return v, true
		}
	}
	// `fmt.Sprintf("gcp_%s", ...)` — fall back to the literal portion of the
	// format string that precedes the first verb. That's enough to verify the
	// "cloudcost_gcp_" prefix.
	if call, ok := expr.(*ast.CallExpr); ok {
		if name, ok := selectorName(call.Fun); ok && name == "Sprintf" && len(call.Args) >= 1 {
			if format, ok := stringLiteralValue(call.Args[0]); ok {
				if idx := strings.Index(format, "%"); idx >= 0 {
					return format[:idx], true
				}
				return format, true
			}
		}
	}
	return "", false
}

// collectStringConstants scans top-level `const` / `var` declarations and
// returns a map of identifier → string-literal value. Used to resolve
// `subsystem`-style identifiers referenced from metric constructors.
func collectStringConstants(file *ast.File) map[string]string {
	out := map[string]string{}
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		if gd.Tok != token.CONST && gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if i >= len(vs.Values) {
					continue
				}
				if s, ok := stringLiteralValue(vs.Values[i]); ok {
					out[name.Name] = s
				}
			}
		}
	}
	return out
}

func stringLiteralValue(e ast.Expr) (string, bool) {
	bl, ok := e.(*ast.BasicLit)
	if !ok || bl.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(bl.Value)
	if err != nil {
		return "", false
	}
	return s, true
}

// selectorName returns the rightmost identifier of a function expression,
// e.g. `prometheus.NewDesc` → "NewDesc", `utils.GenerateDesc` → "GenerateDesc".
func selectorName(fn ast.Expr) (string, bool) {
	switch e := fn.(type) {
	case *ast.SelectorExpr:
		return e.Sel.Name, true
	case *ast.Ident:
		return e.Name, true
	}
	return "", false
}

// extractStringSliceLiteral scans the given arguments for the last []string{...}
// composite literal and returns its element values. Returns false if the
// labels can't be determined statically.
func extractStringSliceLiteral(args []ast.Expr) ([]string, bool) {
	var lit *ast.CompositeLit
	for _, arg := range args {
		cl, ok := arg.(*ast.CompositeLit)
		if !ok {
			continue
		}
		if !isStringSliceType(cl.Type) {
			continue
		}
		lit = cl
	}
	if lit == nil {
		return nil, false
	}
	out := make([]string, 0, len(lit.Elts))
	for _, elt := range lit.Elts {
		s, ok := stringLiteralValue(elt)
		if !ok {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

func isStringSliceType(t ast.Expr) bool {
	arr, ok := t.(*ast.ArrayType)
	if !ok || arr.Len != nil {
		return false
	}
	ident, ok := arr.Elt.(*ast.Ident)
	return ok && ident.Name == "string"
}
