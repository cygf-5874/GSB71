package layercfg

// resolveKey 返回 key 在一组同路径层中的最高优先级结果。
//
// frames 按优先级从高到低排列。若最高值是对象，返回值是仍需递归合并的
// 对象层；否则返回标量、数组或 nil 删除标记。对象会挡住更低层的非对象，
// 因而低层对象只在它与更高层对象之间没有非对象屏障时参与合并。
func resolveKey(frames []Layer, key string) ([]Layer, any, bool) {
	for i, frame := range frames {
		value, ok := frame[key]
		if !ok {
			continue
		}
		object, ok := asObject(value)
		if !ok {
			return nil, value, true
		}

		objects := []Layer{object}
		for _, lower := range frames[i+1:] {
			lowerValue, ok := lower[key]
			if !ok {
				continue
			}
			if lowerObject, ok := asObject(lowerValue); ok {
				objects = append(objects, lowerObject)
			} else {
				break
			}
		}
		return objects, nil, true
	}
	return nil, nil, false
}
