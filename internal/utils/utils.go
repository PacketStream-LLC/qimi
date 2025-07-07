package utils

import "os"

func Map[T, U any](arr []T, function func(T) U) []U {
	if arr == nil {
		return nil
	}

	var result = make([]U, 0)
	for _, v := range arr {
		result = append(result, function(v))
	}

	return result
}

// IsRoot checks if the current user is running as root
func IsRoot() bool {
	return os.Getuid() == 0
}
