package formats

import "strconv"

// ParseInt64 Метод парсит строку в uint64
func ParseInt64(value string) uint64 {
	if value == "" {
		return 0
	}
	uint64, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0
	}
	return uint64
}
