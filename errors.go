package layercfg

import "errors"

// ErrBadPath 由 ParsePath 返回：路径为空、含空段（例如 "a..b"、前缀或后缀是点）、
// 或方括号段格式非法（未闭合、缺引号、转义字符不完整）。
//
// Lookup 不返回错误：路径非法时一律当作「这条路径上没有值」，返回 (nil, false)。
var ErrBadPath = errors.New("layercfg: bad path")
