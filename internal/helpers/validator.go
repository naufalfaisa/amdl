// ============================================
// File: internal/helpers/validator.go
package helpers

import "slices"

func IsInArray(arr []int, target int) bool {
	return slices.Contains(arr, target)
}

func Contains(slice []string, item string) bool {
	return slices.Contains(slice, item)
}
