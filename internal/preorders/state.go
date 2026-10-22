package preorders

func Next(status string) []string {
	switch status {
	case "requested":
		return []string{"confirmed", "rejected", "cancelled"}
	case "confirmed":
		return []string{"preparing", "ready", "collected", "cancelled", "uncollected"}
	case "preparing":
		return []string{"ready", "collected", "cancelled", "uncollected"}
	case "ready":
		return []string{"collected", "cancelled", "uncollected"}
	default:
		return []string{}
	}
}
