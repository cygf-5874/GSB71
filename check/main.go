// Command check 是 layercfg 的固定验收程序。
//
// ⚠️ 不要修改本文件。它是判定「题目有没有做对」的依据；
// 修改它只会让判定失效，不会让实现变对。
//
// 10 个场景分四组：
//
//	semantics 4 个：标量按优先级覆盖 / 对象递归合并与标量-对象互替 /
//	                数组整体替换 / nil 删除标记与点路径、转义路径、ParsePath
//	frozen    2 个：旧快照不受后续合并影响 / Keys() 升序且多次调用稳定
//	cost      3 个：AllocsPerRun ≤ 12 / 6 层分配次数 ≤ 1 层的 3 倍 /
//	                与逐层深拷贝参照实现逐键对拍
//	api       1 个：Merge 与 Store 不改动入参 Layer
//
// 用法：
//
//	go run ./check                  # 跑全部场景
//	go run ./check --only cost      # 只跑一组
//	go run ./check -list            # 列出场景
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"runtime/pprof"
	"sort"
	"strings"
	"testing"
	"time"

	"layercfg"
)

const (
	childTimeout = 25 * time.Second
	parentGrace  = 10 * time.Second
)

type scenario struct {
	Group string
	Name  string
	Run   func() error
}

var scenarios = []scenario{
	{"semantics", "SEM1_scalar_override", sem1},
	{"semantics", "SEM2_object_recursive_merge", sem2},
	{"semantics", "SEM3_array_replaced_wholesale", sem3},
	{"semantics", "SEM4_nil_delete_and_paths", sem4},
	{"frozen", "FRZ1_old_snapshot_stable", frz1},
	{"frozen", "FRZ2_keys_sorted_and_stable", frz2},
	{"cost", "COST1_allocs_under_budget", cost1},
	{"cost", "COST2_alloc_growth_bounded", cost2},
	{"cost", "COST3_semantic_parity_with_deepcopy", cost3},
	{"api", "API1_merge_does_not_mutate_layers", api1},
}

var groups = []string{"semantics", "frozen", "cost", "api"}

func main() {
	only := flag.String("only", "", "只跑指定组：semantics / frozen / cost / api")
	name := flag.String("scenario", "", "内部使用：只跑单个场景")
	list := flag.Bool("list", false, "列出全部场景")
	flag.Parse()

	if *list {
		for _, sc := range scenarios {
			fmt.Printf("%-10s %s\n", sc.Group, sc.Name)
		}
		return
	}
	if *name != "" {
		os.Exit(runChild(*name))
	}
	os.Exit(runParent(*only))
}

func runChild(name string) int {
	var sc *scenario
	for i := range scenarios {
		if scenarios[i].Name == name {
			sc = &scenarios[i]
			break
		}
	}
	if sc == nil {
		fmt.Fprintf(os.Stderr, "未知场景 %q\n", name)
		return 2
	}

	watchdog := time.AfterFunc(childTimeout, func() {
		fmt.Fprintf(os.Stderr, "看门狗：场景 %s 超过 %s 仍未结束，下面是全部 goroutine 栈\n",
			sc.Name, childTimeout)
		_ = pprof.Lookup("goroutine").WriteTo(os.Stderr, 2)
		os.Exit(3)
	})
	defer watchdog.Stop()

	if err := sc.Run(); err != nil {
		fmt.Println(err.Error())
		return 1
	}
	fmt.Println("ok")
	return 0
}

func runParent(only string) int {
	exe, err := os.Executable()
	if err != nil {
		fmt.Println("无法定位自身可执行文件:", err)
		return 1
	}

	selected := groups
	if only != "" {
		found := false
		for _, g := range groups {
			if g == only {
				found = true
			}
		}
		if !found {
			fmt.Printf("未知分组 %q（可选：semantics / frozen / cost / api）\n", only)
			return 2
		}
		selected = []string{only}
	}

	total, passed := 0, 0
	for _, g := range selected {
		fmt.Printf("== 组 %s ==\n", g)
		for _, sc := range scenarios {
			if sc.Group != g {
				continue
			}
			total++

			ctx, cancel := context.WithTimeout(context.Background(), childTimeout+parentGrace)
			cmd := exec.CommandContext(ctx, exe, "-scenario", sc.Name)
			cmd.WaitDelay = 5 * time.Second
			out, runErr := cmd.CombinedOutput()
			cancel()

			text := strings.TrimSpace(string(out))
			if runErr == nil {
				passed++
				fmt.Printf("PASS %s/%s\n", sc.Group, sc.Name)
				continue
			}
			detail := lastLine(text)
			if !strings.HasPrefix(detail, "期望=") {
				detail = "期望=场景正常结束 实际=" + detail
			}
			fmt.Printf("FAIL %s/%s  %s\n", sc.Group, sc.Name, detail)
		}
	}

	fmt.Println()
	fmt.Printf("结果：通过 %d/%d\n", passed, total)
	if passed != total {
		return 1
	}
	return 0
}

func lastLine(s string) string {
	if s == "" {
		return "(子进程没有输出)"
	}
	lines := strings.Split(s, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			return strings.TrimSpace(lines[i])
		}
	}
	return "(子进程没有输出)"
}

// ------------------------------------------------------------------ 失败描述

// detailError 的 Error() 恰好就是「期望=… 实际=…」。
type detailError struct{ msg string }

func (e *detailError) Error() string { return e.msg }

func expect(want, got string) error {
	return &detailError{msg: "期望=" + want + " 实际=" + got}
}

// --------------------------------------------------------------------- 辅助

// obj 是写嵌套对象的简写：obj("a", 1, "b", 2) 等价于 layercfg.Layer{"a": 1, "b": 2}。
func obj(pairs ...any) layercfg.Layer {
	m := layercfg.Layer{}
	for i := 0; i+1 < len(pairs); i += 2 {
		m[pairs[i].(string)] = pairs[i+1]
	}
	return m
}

func valueEqual(a, b any) bool { return reflect.DeepEqual(a, b) }

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

func keySet(s *layercfg.Snapshot) map[string]bool {
	set := map[string]bool{}
	for _, k := range s.Keys() {
		set[k] = true
	}
	return set
}

// lookupCheck 是各场景里最常用的一句：查 path 应得到 want。
func lookupCheck(s *layercfg.Snapshot, path string, want any) error {
	got, ok := s.Lookup(path)
	if !ok || !valueEqual(got, want) {
		return expect(fmt.Sprintf("Lookup(%q)=%v", path, want), fmt.Sprintf("%v ok=%v", got, ok))
	}
	return nil
}

func lookupMissing(s *layercfg.Snapshot, path string) error {
	if got, ok := s.Lookup(path); ok {
		return expect(fmt.Sprintf("Lookup(%q) 查不到", path), fmt.Sprintf("%v", got))
	}
	return nil
}

// deepCopyValue 是检查程序自带的深拷贝，用于构造「调用前」的基线快照。
func deepCopyValue(v any) any {
	if m, ok := v.(map[string]any); ok {
		out := make(map[string]any, len(m))
		for k, c := range m {
			out[k] = deepCopyValue(c)
		}
		return out
	}
	if arr, ok := v.([]any); ok {
		out := make([]any, len(arr))
		for i, e := range arr {
			out[i] = deepCopyValue(e)
		}
		return out
	}
	return v
}

// ------------------------------------------------------------ 语义参照实现（逐层深拷贝）

// naiveMerge 是 cost 组的语义参照物：逐层深拷贝 + 递归合并，语义与契约一致。
func naiveMerge(layers []layercfg.Layer) map[string]any {
	out := map[string]any{}
	for _, l := range layers {
		naiveApply(out, l)
	}
	return out
}

func naiveApply(dst map[string]any, src map[string]any) {
	for k, v := range src {
		if v == nil {
			delete(dst, k)
			continue
		}
		if sm, ok := v.(map[string]any); ok {
			if dm, ok := dst[k].(map[string]any); ok {
				naiveApply(dm, sm)
				continue
			}
			dm := map[string]any{}
			naiveApply(dm, sm)
			dst[k] = dm
			continue
		}
		dst[k] = deepCopyValue(v)
	}
}

// refLeaves 从参照树的根出发，返回排序后的叶子路径集合与 path -> 值。
// 路径写法与 README 契约 6/7 一致。
func refLeaves(root map[string]any) ([]string, map[string]any) {
	vals := map[string]any{}
	var walk func(prefix string, v any)
	walk = func(prefix string, v any) {
		if m, ok := v.(map[string]any); ok {
			for k, c := range m {
				walk(refJoin(prefix, k), c)
			}
			return
		}
		vals[prefix] = v
	}
	for k, c := range root {
		walk(refJoin("", k), c)
	}
	paths := make([]string, 0, len(vals))
	for p := range vals {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths, vals
}

func refJoin(prefix, seg string) string {
	s := refSegment(seg)
	if prefix == "" {
		return s
	}
	return prefix + "." + s
}

func refSegment(seg string) string {
	if seg != "" && !strings.ContainsAny(seg, `.["\\]`) {
		return seg
	}
	var b strings.Builder
	b.WriteString(`["`)
	for i := 0; i < len(seg); i++ {
		if c := seg[i]; c == '"' || c == '\\' {
			b.WriteByte('\\')
		}
		b.WriteByte(seg[i])
	}
	b.WriteString(`"]`)
	return b.String()
}

// ---------------------------------------------------------------- 场景数据构造

const (
	costKeyCount   = 512
	costLayerCount = 6
)

// costLayers 造 N 层、每层 M 个键，每个键指向一个小对象。
func costLayers() []layercfg.Layer {
	layers := make([]layercfg.Layer, costLayerCount)
	for li := range layers {
		l := make(layercfg.Layer, costKeyCount)
		for ki := 0; ki < costKeyCount; ki++ {
			l[fmt.Sprintf("k%04d", ki)] = obj(
				"a", li*costKeyCount+ki,
				"b", fmt.Sprintf("v%d", li),
			)
		}
		layers[li] = l
	}
	return layers
}

// parityLayers 是 cost 组对拍用的层：对象、数组、覆盖、删除标记都覆盖到，键全是普通键。
func parityLayers() []layercfg.Layer {
	return []layercfg.Layer{
		obj(
			"a", obj("x", 1, "y", 2),
			"b", []any{1, 2, 3},
			"c", "low",
			"d", obj("deep", obj("v", 1)),
		),
		obj(
			"a", obj("y", 20, "z", 30),
			"c", "mid",
			"e", 5,
		),
		obj(
			"b", []any{9},
			"d", obj("deep", obj("v", 2, "w", 3)),
		),
		obj(
			"a", obj("x", nil),
			"e", nil,
			"f", obj("g", obj("h", 1)),
		),
	}
}

// ------------------------------------------------------------------ semantics

func sem1() error {
	s := layercfg.Merge(
		obj("a", 1, "b", "x", "c", true, "z", "keep"),
		obj("b", "y", "d", 2.5),
		obj("c", false, "a", 7),
	)
	for _, c := range []struct {
		path string
		want any
	}{
		{"a", 7}, {"b", "y"}, {"c", false}, {"d", 2.5}, {"z", "keep"},
	} {
		if err := lookupCheck(s, c.path, c.want); err != nil {
			return err
		}
	}
	return nil
}

func sem2() error {
	s := layercfg.Merge(
		obj(
			"svc", obj("host", "a", "port", 80, "opt", obj("tls", true, "retry", 3)),
			"flat", 1,
		),
		obj(
			"svc", obj("port", 8080, "opt", obj("retry", 5)),
			"flat", obj("now", "object"),
		),
	)
	for _, c := range []struct {
		path string
		want any
	}{
		{"svc.host", "a"},
		{"svc.port", 8080},
		{"svc.opt.tls", true},
		{"svc.opt.retry", 5},
		{"flat.now", "object"},
	} {
		if err := lookupCheck(s, c.path, c.want); err != nil {
			return err
		}
	}
	if err := lookupMissing(s, "flat"); err != nil {
		return err
	}

	s2 := layercfg.Merge(obj("k", 5), obj("k", obj("x", 1)))
	if err := lookupCheck(s2, "k.x", 1); err != nil {
		return err
	}
	return lookupMissing(s2, "k")
}

func sem3() error {
	s := layercfg.Merge(
		obj("list", []any{1, 2, 3}, "n", obj("arr", []any{"a", "b"}), "s", "x"),
		obj("list", []any{9}, "n", obj("arr", []any{"z"})),
	)
	got, ok := s.Lookup("list")
	if !ok || !valueEqual(got, []any{9}) {
		return expect("list=[9]", fmt.Sprintf("%v ok=%v", got, ok))
	}
	if arr, ok := got.([]any); !ok || len(arr) != 1 {
		return expect("list 整体替换后长度 1", fmt.Sprintf("%#v", got))
	}
	if err := lookupCheck(s, "n.arr", []any{"z"}); err != nil {
		return err
	}
	return lookupCheck(s, "s", "x")
}

func sem4() error {
	s := layercfg.Merge(
		obj("a", obj("b", obj("c", 1), "d", 2), "keep", 7, "gone", obj("inner", 5)),
		obj("a", obj("b", nil), "gone", nil),
	)

	for _, p := range []string{"a.b", "a.b.c", "gone", "gone.inner"} {
		if err := lookupMissing(s, p); err != nil {
			return err
		}
	}
	if err := lookupCheck(s, "a.d", 2); err != nil {
		return err
	}
	if err := lookupCheck(s, "keep", 7); err != nil {
		return err
	}

	set := keySet(s)
	for _, p := range []string{"a.b", "a.b.c", "gone", "gone.inner"} {
		if set[p] {
			return expect(fmt.Sprintf("Keys() 不含 %q", p), "含有了")
		}
	}
	for _, p := range []string{"a.d", "keep"} {
		if !set[p] {
			return expect(fmt.Sprintf("Keys() 含 %q", p), fmt.Sprintf("%v", sortedCopy(s.Keys())))
		}
	}

	// 转义路径与转义后的 Keys() 写法
	s2 := layercfg.Merge(obj("a.b", obj("c", 1), "plain", 2))
	if err := lookupCheck(s2, `["a.b"].c`, 1); err != nil {
		return err
	}
	if err := lookupMissing(s2, "a.b.c"); err != nil {
		return err
	}
	set2 := keySet(s2)
	if !set2[`["a.b"].c`] || !set2["plain"] {
		return expect(`Keys() 含 ["a.b"].c 与 plain`, fmt.Sprintf("%v", sortedCopy(s2.Keys())))
	}

	// ParsePath
	segs, err := layercfg.ParsePath(`["a.b"].c`)
	if err != nil || !equalStrings(segs, []string{"a.b", "c"}) {
		return expect(`ParsePath(["a.b"].c)=[a.b c]`, fmt.Sprintf("%v err=%v", segs, err))
	}
	segs, err = layercfg.ParsePath("a.b")
	if err != nil || !equalStrings(segs, []string{"a", "b"}) {
		return expect("ParsePath(a.b)=[a b]", fmt.Sprintf("%v err=%v", segs, err))
	}
	for _, bad := range []string{"", "a..b", ".a", "a.", "a[", `["x"`} {
		if _, err := layercfg.ParsePath(bad); !errors.Is(err, layercfg.ErrBadPath) {
			return expect(fmt.Sprintf("ParsePath(%q)=ErrBadPath", bad), fmt.Sprintf("%v", err))
		}
	}
	return nil
}

// --------------------------------------------------------------------- frozen

func frz1() error {
	base := obj("app", obj("name", "svc", "port", 80), "debug", false)
	st := layercfg.NewStore()
	st.Add(base)
	s1 := st.Snapshot()
	s1Keys := sortedCopy(s1.Keys())
	if err := lookupCheck(s1, "app.port", 80); err != nil {
		return err
	}

	over := obj("app", obj("port", 9090, "extra", "new"), "debug", true, "added", 42)
	st.Add(over)
	s2 := st.Snapshot()

	// 旧快照必须保持旧内容
	if err := lookupCheck(s1, "app.port", 80); err != nil {
		return err
	}
	if err := lookupCheck(s1, "debug", false); err != nil {
		return err
	}
	if err := lookupMissing(s1, "added"); err != nil {
		return err
	}
	if err := lookupMissing(s1, "app.extra"); err != nil {
		return err
	}
	if !equalStrings(sortedCopy(s1.Keys()), s1Keys) {
		return expect(fmt.Sprintf("旧快照 Keys=%v", s1Keys), fmt.Sprintf("%v", sortedCopy(s1.Keys())))
	}

	// 新快照必须看到新内容
	if err := lookupCheck(s2, "app.port", 9090); err != nil {
		return err
	}
	if err := lookupCheck(s2, "added", 42); err != nil {
		return err
	}

	// Merge 返回的快照同样冻结
	m1 := layercfg.Merge(base, obj("debug", true))
	if err := lookupCheck(m1, "debug", true); err != nil {
		return err
	}
	_ = layercfg.Merge(base, obj("app", obj("port", 1234)), obj("extra", 1))
	if err := lookupCheck(m1, "app.port", 80); err != nil {
		return err
	}
	return lookupMissing(m1, "extra")
}

func frz2() error {
	layer := layercfg.Layer{}
	for i := 0; i < 24; i++ {
		layer[fmt.Sprintf("k%02d", i)] = i
	}
	layer["nest"] = obj("c", 3, "a", 1, "b", 2)
	layer["z"] = 26

	s := layercfg.Merge(layer)
	first := s.Keys()
	if len(first) == 0 {
		return expect("Keys() 非空", "空")
	}
	for i := 1; i < len(first); i++ {
		if !(first[i-1] < first[i]) {
			return expect("Keys() 按字节严格升序", fmt.Sprintf("%q 不小于 %q", first[i-1], first[i]))
		}
	}
	for round := 1; round <= 5; round++ {
		if again := s.Keys(); !equalStrings(again, first) {
			return expect("Keys() 多次调用逐项相同", fmt.Sprintf("第 %d 次 %v", round, again))
		}
	}
	return nil
}

// ----------------------------------------------------------------------- cost

func cost1() error {
	layers := costLayers()
	var snap *layercfg.Snapshot
	allocs := testing.AllocsPerRun(50, func() { snap = layercfg.Merge(layers...) })
	if snap == nil {
		return expect("快照非空", "nil")
	}
	if allocs > 12 {
		return expect("合并 6 层 × 512 键时 AllocsPerRun ≤ 12", fmt.Sprintf("%.2f", allocs))
	}
	// 顺带确认结果没被「少做点事」偷掉
	if err := lookupCheck(snap, "k0000.a", 5*costKeyCount); err != nil {
		return err
	}
	return lookupCheck(snap, "k0511.b", "v5")
}

func cost2() error {
	layers := costLayers()
	var one, six *layercfg.Snapshot
	a1 := testing.AllocsPerRun(50, func() { one = layercfg.Merge(layers[:1]...) })
	a6 := testing.AllocsPerRun(50, func() { six = layercfg.Merge(layers...) })
	if one == nil || six == nil {
		return expect("快照非空", "nil")
	}
	limit := 3 * a1
	if a6 > limit {
		return expect(
			fmt.Sprintf("6 层分配次数 ≤ 1 层的 3 倍（3×%.2f=%.2f）", a1, limit),
			fmt.Sprintf("%.2f", a6),
		)
	}
	return nil
}

func cost3() error {
	layers := parityLayers()
	// 参照物必须基于「调用 Merge 之前」的层快照，避免被实现改动入参这件事污染对拍。
	baseline := make([]layercfg.Layer, len(layers))
	for i, l := range layers {
		baseline[i] = deepCopyValue(map[string]any(l)).(map[string]any)
	}
	refKeys, refVals := refLeaves(naiveMerge(baseline))
	if len(refKeys) == 0 {
		return expect("参照实现非空", "空")
	}
	snap := layercfg.Merge(layers...)
	for _, p := range refKeys {
		got, ok := snap.Lookup(p)
		if !ok {
			return expect(fmt.Sprintf("Lookup(%q)=%v", p, refVals[p]), "查不到")
		}
		if !valueEqual(got, refVals[p]) {
			return expect(fmt.Sprintf("Lookup(%q)=%v", p, refVals[p]), fmt.Sprintf("%v", got))
		}
	}
	if got := snap.Keys(); !equalStrings(got, refKeys) {
		return expect(fmt.Sprintf("Keys()=%v", refKeys), fmt.Sprintf("%v", got))
	}
	return nil
}

// ------------------------------------------------------------------------ api

func api1() error {
	layers := []layercfg.Layer{
		obj("a", obj("x", 1, "y", 2), "n", 3, "list", []any{1, 2}),
		obj("a", obj("y", nil, "z", 4), "n", nil, "new", obj("k", 1)),
		obj("a", obj("w", 9), "list", []any{3}),
	}
	before := make([]map[string]any, len(layers))
	for i, l := range layers {
		before[i] = deepCopyValue(map[string]any(l)).(map[string]any)
	}

	snap := layercfg.Merge(layers...)
	if err := lookupCheck(snap, "a.z", 4); err != nil {
		return err
	}
	if err := lookupMissing(snap, "n"); err != nil {
		return err
	}

	for i, l := range layers {
		if !reflect.DeepEqual(map[string]any(l), before[i]) {
			return expect(fmt.Sprintf("Merge 后第 %d 层调用前后逐键相同", i), fmt.Sprintf("%v", l))
		}
	}

	// Store 路径同样不得改动入参
	st := layercfg.NewStore()
	st.Add(layers[0])
	_ = st.Snapshot()
	if !reflect.DeepEqual(map[string]any(layers[0]), before[0]) {
		return expect("Store.Add + Snapshot 后第 0 层逐键相同", fmt.Sprintf("%v", layers[0]))
	}
	return nil
}
