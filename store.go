package layercfg

import "sort"

// Store 按优先级从低到高累积若干层，并在取快照时把它们合并成一棵配置树。
//
//	low  := Layer{"app": Layer{"port": 80}}
//	high := Layer{"app": Layer{"port": 9090}}
//
//	st := NewStore()
//	st.Add(low)
//	st.Add(high)
//	s := st.Snapshot()
//	port, _ := s.Lookup("app.port") // 9090
//
// Add 与 Snapshot 都不会改动传入的层；已返回的快照被冻结，
// 之后的 Add / Snapshot 不影响它观察到的内容。
type Store struct {
	layers []Layer
}

// NewStore 建一个空 Store。
func NewStore() *Store { return &Store{} }

// Add 追加一层，优先级高于此前所有层。层本身不会被修改。
func (s *Store) Add(l Layer) {
	s.layers = append(s.layers, l)
}

// Snapshot 返回当前各层合并结果的只读快照。快照一旦返回即冻结：
// 它持有层列表的独立副本，此后的 Add / Snapshot 不会影响它。
func (s *Store) Snapshot() *Snapshot {
	layers := make([]Layer, len(s.layers))
	copy(layers, s.layers)
	return &Snapshot{layers: layers}
}

// Merge 合并给定各层并返回快照；靠前的层优先级低，靠后的层优先级高。
// 传入的层不会被修改。
func Merge(layers ...Layer) *Snapshot {
	st := Store{layers: layers}
	return st.Snapshot()
}

// Snapshot 是某一时刻合并结果的只读视图。
//
// 快照不物化合并后的整棵树，而是共享各入参层（它们从不被修改），
// 在 Lookup / Keys 时按 overlay 规则现场计算合并结果。因此合并的
// 分配代价与层数、键数无关，而语义与逐层深拷贝合并逐键相同。
// Lookup / Keys 只读，可安全地被多个 goroutine 并发调用。
type Snapshot struct {
	layers []Layer
}

// overlay 把各层根对象按优先级从低到高摆成合并视图。
func (s *Snapshot) overlay() overlay {
	o := make(overlay, len(s.layers))
	for i, l := range s.layers {
		o[i] = l
	}
	return o
}

// Lookup 按点路径查找叶子值。
//
// 路径指向对象、键不存在、或已被删除标记删掉时，返回 (nil, false)；
// 路径非法同样返回 (nil, false)，不 panic。
func (s *Snapshot) Lookup(path string) (any, bool) {
	segs, err := ParsePath(path)
	if err != nil {
		return nil, false
	}
	cur := s.overlay()
	for i, seg := range segs {
		v, ok := cur.topValue(seg)
		if !ok || v == nil {
			return nil, false
		}
		if _, isObj := asObject(v); isObj {
			if i == len(segs)-1 {
				return nil, false
			}
			cur = cur.children(seg)
			continue
		}
		if i != len(segs)-1 {
			return nil, false
		}
		return v, true
	}
	return nil, false
}

// Keys 返回全部叶子路径，按字典序升序（字节比较）。
// 多次调用结果逐项相同，与 map 迭代顺序无关。
func (s *Snapshot) Keys() []string {
	var out []string
	s.overlay().collectLeaves("", &out)
	sort.Strings(out)
	return out
}
