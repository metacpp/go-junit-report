package helper

import (
	"fmt"
)

// FormatTime formats time into a string
func FormatTime(time float64) string {
	return fmt.Sprintf("%.3f", float64(time))
}
