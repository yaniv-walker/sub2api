package service

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestExecutorHasNoIdentityBranch 用 AST 扫描断言「执行器内不存在身份分支」。
//
// 这是**可执行的设计约束**，不是注释里的一句承诺：
// 「用户自测」与「定时检测」共用同一份执行代码，两者唯一的差异是
// 构造 ExecutionIdentity 时传了谁的 Key。一旦有人在执行器里写下
// `if isInternalAgent { ... }` 这样的分支，两条链路就会开始行为分叉，
// 「用谁的 Key 就扣谁的额度」这一天然隔离也就不再成立了。
//
// 扫描的是标识符（而不是文本），因此注释里出现这些词不会误报 ——
// 本文件自己的注释就大量提到它们。
func TestExecutorHasNoIdentityBranch(t *testing.T) {
	forbidden := []string{
		// 内部账号相关的概念：在执行器里出现就意味着它在区分身份。
		"InternalAgent",
		"IsInternalAgent",
		"ActiveDirectory",
		// 熔断与预算属于调度链路的编排职责，执行器不该看见它们。
		"Guard",
		"BudgetExhausted",
	}

	path := filepath.Join(".", "executor.go")
	src, err := os.ReadFile(path)
	require.NoError(t, err, "executor.go must exist in the service package")

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	require.NoError(t, err, "executor.go must parse")

	var hits []string
	ast.Inspect(file, func(n ast.Node) bool {
		ident, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		for _, banned := range forbidden {
			// 用包含匹配而不是相等：`IsInternalAgent` 也会被 `InternalAgent` 命中，
			// 反过来说，任何带这些前缀的新标识符同样应该被拦下。
			if strings.Contains(ident.Name, banned) {
				pos := fset.Position(ident.Pos())
				hits = append(hits, banned+" -> "+ident.Name+
					" at "+filepath.Base(pos.Filename)+":"+itoaForTest(pos.Line))
			}
		}
		return true
	})

	require.Empty(t, hits,
		"executor.go must not reference identity/guard concepts; it has to stay a pure "+
			"\"execute one case with the given identity\" implementation. Offending identifiers:\n  %s",
		strings.Join(hits, "\n  "))
}

// TestExecutorAcceptsOnlyIdentity 断言执行器的入参结构里没有账号/Key 实体。
//
// 类型系统是第一道保险：如果 ExecuteInput 里出现 *upstreamsvc.APIKey 或
// *iqbenchmark.InternalAgent，即使没有 if 分支，调用方也能从结构体里
// 反推出「这是谁的账号」，身份隔离就从「不可能」退化成「靠自觉」。
func TestExecutorAcceptsOnlyIdentity(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "executor.go", nil, 0)
	require.NoError(t, err)

	// 收集 ExecuteInput 的字段类型名。
	fieldTypes := map[string]string{}
	ast.Inspect(file, func(n ast.Node) bool {
		spec, ok := n.(*ast.TypeSpec)
		if !ok || spec.Name == nil || spec.Name.Name != "ExecuteInput" {
			return true
		}
		structType, ok := spec.Type.(*ast.StructType)
		if !ok {
			return true
		}
		for _, field := range structType.Fields.List {
			typeName := exprToName(field.Type)
			for _, name := range field.Names {
				fieldTypes[name.Name] = typeName
			}
		}
		return false
	})
	require.NotEmpty(t, fieldTypes, "ExecuteInput must be declared in executor.go")

	for _, banned := range []string{"APIKey", "InternalAgent", "Guard"} {
		for name, typeName := range fieldTypes {
			require.NotContains(t, typeName, banned,
				"ExecuteInput.%s has type %s; the executor must receive only "+
					"ExecutionIdentity, never an account or key entity", name, typeName)
		}
	}

	// 正例：身份必须经由 ExecutionIdentity 传递。
	require.Equal(t, "ExecutionIdentity", fieldTypes["Identity"],
		"ExecuteInput must carry the billing identity as an ExecutionIdentity value")
}

// exprToName 把类型表达式渲染成可比较的名字（只处理本测试需要的形态）。
func exprToName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return exprToName(t.X)
	case *ast.SelectorExpr:
		return exprToName(t.X) + "." + t.Sel.Name
	case *ast.ArrayType:
		return "[]" + exprToName(t.Elt)
	case *ast.MapType:
		return "map[" + exprToName(t.Key) + "]" + exprToName(t.Value)
	default:
		return ""
	}
}

// itoaForTest 避免为一行日志引入 strconv。
func itoaForTest(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf []byte
	for v > 0 {
		buf = append([]byte{byte('0' + v%10)}, buf...)
		v /= 10
	}
	if neg {
		return "-" + string(buf)
	}
	return string(buf)
}
