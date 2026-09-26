package layercfg

import (
	"reflect"
	"sort"
	"testing"
)

// obj 是写嵌套对象的简写：obj("a", 1, "b", 2) 等价于 Layer{"a": 1, "b": 2}。
func obj(pairs ...any) Layer {
	m := Layer{}
	for i := 0; i+1 < len(pairs); i += 2 {
		m[pairs[i].(string)] = pairs[i+1]
	}
	return m
}

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

// wantKeys 以「集合」的方式比对 Keys() 的内容（本组用例不约束 Keys() 的顺序）。
func wantKeys(t *testing.T, s *Snapshot, want ...string) {
	t.Helper()
	got := s.Keys()
	sort.Strings(got)
	sort.Strings(want)
	if !equalStrings(got, want) {
		t.Fatalf("Keys() = %v，期望 %v", got, want)
	}
}

func mustValue(t *testing.T, s *Snapshot, path string, want any) {
	t.Helper()
	got, ok := s.Lookup(path)
	if !ok {
		t.Fatalf("Lookup(%q) 没找到，期望 %#v", path, want)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Lookup(%q) = %#v，期望 %#v", path, got, want)
	}
}

func mustMissing(t *testing.T, s *Snapshot, path string) {
	t.Helper()
	if got, ok := s.Lookup(path); ok {
		t.Fatalf("Lookup(%q) = %#v，期望查不到", path, got)
	}
}

func TestMergeScalarLayers(t *testing.T) {
	low := Layer{"a": 1, "b": "x", "c": true}
	mid := Layer{"b": "y", "d": 2.5}
	high := Layer{"c": false, "e": []any{1, 2}}

	s := Merge(low, mid, high)
	mustValue(t, s, "a", 1)
	mustValue(t, s, "b", "y")
	mustValue(t, s, "c", false)
	mustValue(t, s, "d", 2.5)
	mustValue(t, s, "e", []any{1, 2})
	wantKeys(t, s, "a", "b", "c", "d", "e")
}

func TestHigherLayerWins(t *testing.T) {
	s := Merge(Layer{"k": 1}, Layer{"k": 2})
	mustValue(t, s, "k", 2)

	s2 := Merge(Layer{"k": 2}, Layer{"k": 1})
	mustValue(t, s2, "k", 1)
}

func TestScalarReplacesObject(t *testing.T) {
	s := Merge(Layer{"a": obj("x", 1, "y", 2), "b": 1}, Layer{"a": 5})
	mustValue(t, s, "a", 5)
	mustMissing(t, s, "a.x")
	mustValue(t, s, "b", 1)
	wantKeys(t, s, "a", "b")
}

func TestObjectReplacesScalar(t *testing.T) {
	s := Merge(Layer{"a": 5}, Layer{"a": obj("x", 1)})
	mustValue(t, s, "a.x", 1)
	mustMissing(t, s, "a")
	wantKeys(t, s, "a.x")
}

func TestNestedObjectsMergeRecursively(t *testing.T) {
	low := Layer{"svc": obj("host", "a", "port", 80, "opt", obj("tls", true, "retry", 3))}
	high := Layer{"svc": obj("port", 8080, "opt", obj("retry", 5))}

	s := Merge(low, high)
	mustValue(t, s, "svc.host", "a")
	mustValue(t, s, "svc.port", 8080)
	mustValue(t, s, "svc.opt.tls", true)
	mustValue(t, s, "svc.opt.retry", 5)
	wantKeys(t, s, "svc.host", "svc.opt.retry", "svc.opt.tls", "svc.port")
}

func TestArraysReplaceWholesale(t *testing.T) {
	low := Layer{"list": []any{1, 2, 3}, "n": obj("arr", []any{"a", "b"})}
	high := Layer{"list": []any{9}, "n": obj("arr", []any{"z"})}

	s := Merge(low, high)
	mustValue(t, s, "list", []any{9})
	mustValue(t, s, "n.arr", []any{"z"})
}

func TestNilDeletesTopLevelKey(t *testing.T) {
	s := Merge(Layer{"a": 1, "b": 2}, Layer{"a": nil})
	mustMissing(t, s, "a")
	mustValue(t, s, "b", 2)
	wantKeys(t, s, "b")
}

func TestNilDeletesNestedSubtree(t *testing.T) {
	low := Layer{"a": obj("b", obj("c", 1), "d", 2), "keep": 7}
	high := Layer{"a": obj("b", nil)}

	s := Merge(low, high)
	mustMissing(t, s, "a.b")
	mustMissing(t, s, "a.b.c")
	mustValue(t, s, "a.d", 2)
	mustValue(t, s, "keep", 7)
	wantKeys(t, s, "a.d", "keep")
}

func TestDotPathLookup(t *testing.T) {
	s := Merge(Layer{"a": obj("b", obj("c", 42), "s", "v")})
	mustValue(t, s, "a.b.c", 42)
	mustValue(t, s, "a.s", "v")
	mustMissing(t, s, "a.b")
	mustMissing(t, s, "a.b.c.d")
	mustMissing(t, s, "a.nope")
}

func TestEscapedPathAndParsePath(t *testing.T) {
	segs, err := ParsePath(`["a.b"].c`)
	if err != nil || !equalStrings(segs, []string{"a.b", "c"}) {
		t.Fatalf("ParsePath(%q) = %v, %v，期望 [a.b c]", `["a.b"].c`, segs, err)
	}
	segs, err = ParsePath("a.b")
	if err != nil || !equalStrings(segs, []string{"a", "b"}) {
		t.Fatalf("ParsePath(%q) = %v, %v，期望 [a b]", "a.b", segs, err)
	}
	for _, bad := range []string{"", "a..b", ".a", "a.", "a[", `["x"`, `["x"]]`} {
		if _, err := ParsePath(bad); err == nil {
			t.Fatalf("ParsePath(%q) 期望 ErrBadPath，实际 nil", bad)
		}
	}

	s := Merge(Layer{"a.b": obj("c", 1), "plain": 2})
	mustValue(t, s, `["a.b"].c`, 1)
	mustMissing(t, s, `["a.b"]`)
	mustMissing(t, s, "a.b.c")
	wantKeys(t, s, `["a.b"].c`, "plain")
}

func TestKeysIsLeafPathSet(t *testing.T) {
	s := Merge(Layer{
		"a": obj("b", obj("c", 1), "d", 2),
		"e": []any{1, 2},
		"f": 3,
	})
	wantKeys(t, s, "a.b.c", "a.d", "e", "f")

	// 空对象不产生任何路径。
	s2 := Merge(Layer{"empty": Layer{}, "x": 1})
	wantKeys(t, s2, "x")
}

func TestEmptyLayerAndEmptyStore(t *testing.T) {
	s := Merge()
	if len(s.Keys()) != 0 {
		t.Fatalf("空 Store 的 Keys() = %v，期望空", s.Keys())
	}
	mustMissing(t, s, "a")

	s2 := Merge(Layer{}, Layer{"a": 1}, Layer{})
	mustValue(t, s2, "a", 1)
	wantKeys(t, s2, "a")
}

func TestDuplicateKeysLastLayerWins(t *testing.T) {
	s := Merge(Layer{"k": 1}, Layer{"k": 2}, Layer{"k": 3})
	mustValue(t, s, "k", 3)
	wantKeys(t, s, "k")
}

func TestStoreAccumulatesLayers(t *testing.T) {
	st := NewStore()
	st.Add(Layer{"a": 1, "b": obj("x", 1)})
	s1 := st.Snapshot()
	mustValue(t, s1, "a", 1)
	mustValue(t, s1, "b.x", 1)

	st.Add(Layer{"b": obj("y", 2), "c": 3})
	s2 := st.Snapshot()
	mustValue(t, s2, "a", 1)
	mustValue(t, s2, "b.x", 1)
	mustValue(t, s2, "b.y", 2)
	mustValue(t, s2, "c", 3)
	wantKeys(t, s2, "a", "b.x", "b.y", "c")
}
