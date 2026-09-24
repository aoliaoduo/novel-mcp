// Modified for novel-mcp: LLM-call chain removed (see UPSTREAM.md); schema helpers only.
// Package llmcontract 提供 strict 工具调用所需的 schema 小工具：可空联合表达
// 与 strict 就绪校验。包名沿用上游；直接调模型的执行层（Execute、llmretry）
// 已按 UPSTREAM.md 移除，本包不再发起任何模型调用。
package llmcontract

import (
	"fmt"
	"maps"
	"slices"
)

// Nullable 把一个 schema 的 type 扩展为可空联合(["<t>","null"]),用于 strict
// 模式下"全字段 required、可选语义用 null"的表达。返回拷贝,不修改传入 map。
func Nullable(s map[string]any) map[string]any {
	out := maps.Clone(s)
	if t, ok := out["type"].(string); ok {
		out["type"] = []string{t, "null"}
	}
	switch values := out["enum"].(type) {
	case []string:
		enum := make([]any, 0, len(values)+1)
		for _, value := range values {
			enum = append(enum, value)
		}
		out["enum"] = append(enum, nil)
	case []any:
		enum := slices.Clone(values)
		for _, value := range enum {
			if value == nil {
				return out
			}
		}
		out["enum"] = append(enum, nil)
	}
	return out
}

// ValidateStrictReady 递归校验 schema 满足 OpenAI strict 子集的结构前提:
// 所有 object 的属性都必须列入 required(可选语义用 null 联合表达)。litellm
// 在请求期做同样校验并自动补 additionalProperties:false;契约测试用本函数
// 前置断言(RFC §11.1),不把结构问题留到运行时。
func ValidateStrictReady(s map[string]any) error {
	return validateStrictReady(s, "$")
}

func validateStrictReady(s map[string]any, path string) error {
	if typeIncludes(s["type"], "object") {
		props, _ := s["properties"].(map[string]any)
		required, _ := s["required"].([]string)
		for name, sub := range props {
			if !slices.Contains(required, name) {
				return fmt.Errorf("%s.%s 未列入 required(strict 要求全属性 required)", path, name)
			}
			if subMap, ok := sub.(map[string]any); ok {
				if err := validateStrictReady(subMap, path+"."+name); err != nil {
					return err
				}
			}
		}
	}
	if items, ok := s["items"].(map[string]any); ok {
		return validateStrictReady(items, path+"[]")
	}
	return nil
}

func typeIncludes(t any, want string) bool {
	switch v := t.(type) {
	case string:
		return v == want
	case []string:
		return slices.Contains(v, want)
	}
	return false
}
