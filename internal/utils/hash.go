package utils

// computes fnv-1a hash suitable for sharding
func FNV32(s string) uint32 {
	const (
		prime  uint32 = 16777619
		offset uint32 = 2166136261
	)
	h := offset
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= prime
	}
	return h
}
