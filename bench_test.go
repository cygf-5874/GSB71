package layercfg

import (
	"fmt"
	"testing"
)

// BenchmarkMerge 合并 6 层、每层 512 个键。每层的每个键都指向一个小对象，
// 这样「深拷贝整棵树」的代价会随层数与键数一起涨。
func BenchmarkMerge(b *testing.B) {
	const layerCount = 6
	const keyCount = 512

	layers := make([]Layer, layerCount)
	for li := range layers {
		l := make(Layer, keyCount)
		for ki := 0; ki < keyCount; ki++ {
			l[fmt.Sprintf("k%04d", ki)] = Layer{
				"a": li*keyCount + ki,
				"b": fmt.Sprintf("v%d", li),
			}
		}
		layers[li] = l
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		snap := Merge(layers...)
		if snap == nil {
			b.Fatal("Merge 返回了 nil 快照")
		}
	}
}
