package utils

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

func CacheKey(prefix string, parts ...any) string {
	raw, _ := json.Marshal(parts)
	hash := sha1.Sum(raw)

	return fmt.Sprintf("%s:%s", prefix, hex.EncodeToString(hash[:]))
}
