package layercfg

import "strings"

// Layer 是一层配置。Layer 是 map[string]any 的别名：
// 对象一律写成 map[string]any（也就是 Layer），数组写成 []any，
// 标量可以是任意 Go 值，nil 是删除标记。
type Layer = map[string]any

// asObject 取出 v 的对象视图；v 不是对象时第二个返回值为 false。
func asObject(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	return m, ok
}

// cloneTree 深拷贝一棵配置树：对象逐键递归拷贝，数组整体拷贝，标量按值拷贝。
// 入参是标量时原样返回。
func cloneTree(v any) any {
	if m, ok := asObject(v); ok {
		out := make(map[string]any, len(m))
		for k, child := range m {
			out[k] = cloneTree(child)
		}
		return out
	}
	if arr, ok := v.([]any); ok {
		out := make([]any, len(arr))
		for i, elem := range arr {
			out[i] = cloneTree(elem)
		}
		return out
	}
	return v
}

// collectLeafPaths 深度优先收集 v 底下的叶子路径，追加到 out。
// 对象向下展开；标量与数组算叶子；nil 不产生路径。
func collectLeafPaths(v any, prefix string, out *[]string) {
	if v == nil {
		return
	}
	if m, ok := asObject(v); ok {
		for k, child := range m {
			collectLeafPaths(child, appendSegment(prefix, k), out)
		}
		return
	}
	*out = append(*out, prefix)
}

// ParsePath 把点路径切成路径段。
//
// 文法：seg ('.' seg)*，其中 seg 是
//   - 裸段：不含 '.' 与 '['，且非空；或
//   - 方括号段：["..."]，段内的 '\' 与 '"' 用反斜杠转义。
//
// 非法路径返回 ErrBadPath。
func ParsePath(path string) ([]string, error) {
	if path == "" {
		return nil, ErrBadPath
	}
	segs := make([]string, 0, 4)
	i := 0
	for {
		if i >= len(path) {
			return nil, ErrBadPath
		}
		var seg string
		if path[i] == '[' {
			if i+1 >= len(path) || path[i+1] != '"' {
				return nil, ErrBadPath
			}
			var b strings.Builder
			j := i + 2
			closed := false
			for j < len(path) {
				c := path[j]
				if c == '\\' {
					if j+1 >= len(path) {
						return nil, ErrBadPath
					}
					b.WriteByte(path[j+1])
					j += 2
					continue
				}
				if c == '"' {
					closed = true
					j++
					break
				}
				b.WriteByte(c)
				j++
			}
			if !closed || j >= len(path) || path[j] != ']' {
				return nil, ErrBadPath
			}
			seg = b.String()
			i = j + 1
		} else {
			j := i
			for j < len(path) && path[j] != '.' && path[j] != '[' {
				j++
			}
			if j == i {
				return nil, ErrBadPath
			}
			seg = path[i:j]
			i = j
		}
		segs = append(segs, seg)
		if i == len(path) {
			return segs, nil
		}
		if path[i] != '.' {
			return nil, ErrBadPath
		}
		i++
	}
}

// appendSegment 把一段按契约的写法接到前缀后面。
func appendSegment(prefix, seg string) string {
	if prefix == "" {
		return formatSegment(seg)
	}
	return prefix + "." + formatSegment(seg)
}

// formatSegment 输出路径段：普通段按原样，含特殊字符的段用 ["..."] 形式。
func formatSegment(seg string) string {
	if !needsQuote(seg) {
		return seg
	}
	var b strings.Builder
	b.WriteString(`["`)
	for i := 0; i < len(seg); i++ {
		c := seg[i]
		if c == '"' || c == '\\' {
			b.WriteByte('\\')
		}
		b.WriteByte(c)
	}
	b.WriteString(`"]`)
	return b.String()
}

// needsQuote 判断路径段是否必须用方括号形式书写。
func needsQuote(seg string) bool {
	if seg == "" {
		return true
	}
	return strings.ContainsAny(seg, `.["\\]`)
}
