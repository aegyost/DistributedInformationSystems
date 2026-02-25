package combinations

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
)

func TotalCombinations(alphabet string, maxLength int) (uint64, error) {
	base := uint64(len(alphabet))
	if base == 0 {
		return 0, nil
	}
	var total uint64 = 0 
	for l := 1; l <= maxLength; l++ {
		var power uint64 = 1 
		for i := 0; i < l; i++ {
			if power > (^uint64(0))/base { 
				return 0, fmt.Errorf("overflow")
			}
			power *= base
		}
		if total > ^uint64(0)-power { 
			return 0, fmt.Errorf("overflow")
		}
		total += power
	}
	return total, nil
}

func WordByIndex(index uint64, alphabet string, maxLength int) (string, error) {
	base := uint64(len(alphabet))
	if base == 0 {
		return "", fmt.Errorf("empty alphabet")
	}
	remaining := index
	for l := 1; l <= maxLength; l++ {
		count := uint64(1) 
		for i := 0; i < l; i++ {
			if count > (^uint64(0))/base {
				return "", fmt.Errorf("overflow")
			}
			count *= base
		}
		if remaining < count { 
			return wordFromIndex(remaining, l, alphabet), nil 
		}
		remaining -= count
	}
	return "", fmt.Errorf("index %d goes beyond the acceptable limits", index)
}

func wordFromIndex(index uint64, length int, alphabet string) string {
	base := uint64(len(alphabet)) 
	bytes := make([]byte, length) 
	for i := length - 1; i >= 0; i-- {
		bytes[i] = alphabet[index%base]
		index /= base
	}
	return string(bytes)
}

func CheckHash(word, targetHash string) bool {
	hash := md5.Sum([]byte(word)) 
	return hex.EncodeToString(hash[:]) == targetHash 
}