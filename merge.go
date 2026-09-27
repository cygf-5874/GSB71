package layercfg

// overlay 是一组按优先级从低到高排好的对象视图：同一路径在多层都是对象时，
// 这些对象逐键递归合并。合并不物化整棵树，而是让快照持有各层的引用，
// 在 Lookup / Keys 时按 overlay 现场算出合并结果 —— 因此 Merge 本身的代价
// 与层数、键数无关，而观察到的语义与「逐层深拷贝再合并」逐键相同。
//
// 入参 Layer 从不被修改（契约 9），所以快照可以安全地共享它们。
type overlay []map[string]any

// topValue 返回 key 在 overlay 里的「定夺值」：优先级最高的、含有该键的
// 对象所带的值。第二个返回值表示该键是否出现。定夺值是 nil 时表示该键
// 被删除标记抹掉（连同整棵子树）。
func (o overlay) topValue(key string) (any, bool) {
	for i := len(o) - 1; i >= 0; i-- {
		if v, ok := o[i][key]; ok {
			return v, true
		}
	}
	return nil, false
}

// children 返回 key 之下参与递归合并的子视图。调用方保证定夺值是对象。
//
// 从定夺层向下扫描时，第一个值不是对象的层会「截断」更低的层：
// nil 是删除标记，标量/数组则被高层的对象整个取代。截断层（不含）之上、
// 值为对象的层都参与递归，顺序保持优先级从低到高。
func (o overlay) children(key string) overlay {
	top := -1
	for i := len(o) - 1; i >= 0; i-- {
		if _, ok := o[i][key]; ok {
			top = i
			break
		}
	}
	if top < 0 {
		return nil
	}
	floor := -1
	for i := top - 1; i >= 0; i-- {
		if v, ok := o[i][key]; ok {
			if _, isObj := asObject(v); !isObj {
				floor = i
				break
			}
		}
	}
	out := make(overlay, 0, top-floor)
	for i := floor + 1; i <= top; i++ {
		if v, ok := o[i][key]; ok {
			if m, isObj := asObject(v); isObj {
				out = append(out, m)
			}
		}
	}
	return out
}

// collectLeaves 把 overlay 合并视图下的全部叶子路径追加到 out。
// 对象向下展开，标量与数组算叶子，被 nil 删掉的键与空对象不产生路径。
// 追加顺序与 map 迭代顺序有关，调用方负责在最后整体排序。
func (o overlay) collectLeaves(prefix string, out *[]string) {
	seen := make(map[string]struct{})
	keys := make([]string, 0, 8)
	for _, m := range o {
		for k := range m {
			if _, ok := seen[k]; !ok {
				seen[k] = struct{}{}
				keys = append(keys, k)
			}
		}
	}
	for _, k := range keys {
		v, _ := o.topValue(k)
		if v == nil {
			continue
		}
		p := appendSegment(prefix, k)
		if _, isObj := asObject(v); isObj {
			o.children(k).collectLeaves(p, out)
			continue
		}
		*out = append(*out, p)
	}
}
