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
}

// NewStore 建一个空 Store。
func NewStore() *Store { return &Store{} }

// Add 追加一层，优先级高于此前所有层。
func (s *Store) Add(l Layer) {
	s.layers = append(s.layers, l)
}

// Snapshot 合并当前各层并返回快照。
func (s *Store) Snapshot() *Snapshot {
	return &Snapshot{layers: highPriorityLayers(s.layers)}
}

// Merge 合并给定各层并返回快照；靠前的层优先级低，靠后的层优先级高。
func Merge(layers ...Layer) *Snapshot {
	return &Snapshot{layers: highPriorityLayers(layers)}
}

func highPriorityLayers(layers []Layer) []Layer {
	out := make([]Layer, len(layers))
	for i, l := range layers {
		out[len(layers)-1-i] = l
	}
	return out
}

// Snapshot 是某一时刻合并结果的只读视图。
type Snapshot struct {
	layers []Layer
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

	frames := s.layers
	for i, seg := range segs {
		next, value, exists := resolveKey(frames, seg)
		if !exists {
			return nil, false
		}
		if len(next) > 0 {
			frames = next
			continue
		}
		if i != len(segs)-1 || value == nil {
			return nil, false
		}
		return value, true
	}
	return nil, false
}

// Keys 返回全部叶子路径。
func (s *Snapshot) Keys() []string {
	var out []string
	collectLeafPaths(s.layers, "", &out)
	return out
}
