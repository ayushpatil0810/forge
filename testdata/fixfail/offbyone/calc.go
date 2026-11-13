package offbyone

// SumRange sums elements from start to end (inclusive).
func SumRange(nums []int, start, end int) int {
	total := 0
	for i := start; i < end; i++ { // BUG: off-by-one, should be i <= end
		total += nums[i]
	}
	return total
}
