package layercfg

// mergeLayer 把 src 这一层合并进 dst，dst 被原地改写。
//
// 规则：高优先级覆盖低优先级；两边都是对象时逐键递归；
// 数组整体替换（不逐元素合并）；nil 是删除标记。
func mergeLayer(dst map[string]any, src map[string]any) {
	for k, v := range src {
		if v == nil {
			// nil 是删除标记：这个键彻底消失，不在两边留痕迹。
			delete(src, k)
			delete(dst, k)
			continue
		}
		if sm, ok := asObject(v); ok {
			if dm, ok := asObject(dst[k]); ok {
				mergeLayer(dm, sm)
				continue
			}
		}
		dst[k] = cloneTree(v)
	}
}
