package layercfg

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
type Store struct {
	layers []Layer
	tree   map[string]any
}

// NewStore 建一个空 Store。
func NewStore() *Store { return &Store{} }

// Add 追加一层，优先级高于此前所有层。
func (s *Store) Add(l Layer) {
	s.layers = append(s.layers, l)
}

// Snapshot 合并当前各层并返回快照。
func (s *Store) Snapshot() *Snapshot {
	s.tree = buildTree(s.layers)
	return &Snapshot{store: s}
}

// Merge 合并给定各层并返回快照；靠前的层优先级低，靠后的层优先级高。
func Merge(layers ...Layer) *Snapshot {
	st := NewStore()
	for _, l := range layers {
		st.Add(l)
	}
	return st.Snapshot()
}

// buildTree 逐层合并出整棵配置树：每合一层，都把现有整棵树深拷贝一份再合上去。
func buildTree(layers []Layer) map[string]any {
	out := map[string]any{}
	for _, l := range layers {
		cloned, _ := cloneTree(out).(map[string]any)
		mergeLayer(cloned, l)
		out = cloned
	}
	return out
}

// Snapshot 是某一时刻合并结果的只读视图。
type Snapshot struct {
	store *Store
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
	var cur any = s.store.tree
	for i, seg := range segs {
		m, ok := asObject(cur)
		if !ok {
			return nil, false
		}
		v, ok := m[seg]
		if !ok || v == nil {
			return nil, false
		}
		if i == len(segs)-1 {
			if _, isObj := asObject(v); isObj {
				return nil, false
			}
			return v, true
		}
		cur = v
	}
	return nil, false
}

// Keys 返回全部叶子路径。
func (s *Snapshot) Keys() []string {
	var out []string
	for k, v := range s.store.tree {
		collectLeafPaths(v, appendSegment("", k), &out)
	}
	return out
}
