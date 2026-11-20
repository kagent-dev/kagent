package models

import "math"

func cachedTokenCount(tokens int64) int32 {
	return int32(min(max(tokens, 0), math.MaxInt32))
}
