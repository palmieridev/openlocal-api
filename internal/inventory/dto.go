package inventory

type StockMovementRequest struct {
	BusinessID    string  `json:"business_id"`
	VariantID     string  `json:"variant_id"`
	LocationID    *string `json:"location_id"`
	MovementType  string  `json:"movement_type"`
	Quantity      string  `json:"quantity"`
	UnitCost      *string `json:"unit_cost"`
	ReferenceType *string `json:"reference_type"`
	ReferenceID   *string `json:"reference_id"`
	Notes         string  `json:"notes"`
}
