package support

type FeedbackRequest struct {
	DocID   string  `json:"doc_id"`
	Locale  string  `json:"locale"`
	Verdict string  `json:"verdict"`
	Comment *string `json:"comment"`
	Path    *string `json:"path"`
}

type FeedbackCreatedResponse struct {
	ID string `json:"id"`
}
