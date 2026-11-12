package boundary

// CalculateDiscount returns 10% discount for orders of $100 or more.
// BUG: uses orderTotal > 100 instead of orderTotal >= 100
func CalculateDiscount(orderTotal int) int {
	if orderTotal > 100 {
		return orderTotal / 10
	}
	return 0
}
