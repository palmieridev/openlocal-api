package appointments

func Next(status string) []string {
	switch status {
	case "requested":
		return []string{"confirmed", "rejected", "cancelled"}
	case "confirmed":
		return []string{"completed", "cancelled", "no_show"}
	default:
		return []string{}
	}
}
