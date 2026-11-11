package marketplace_test

import (
	"context"
	"errors"
	"testing"

	"github.com/palmieridev/openlocal-api/internal/marketplace/discovery"
)

func TestDiscoveryNormalizesReachAndBoundsBeforeStorage(t *testing.T) {
	filter, err := discovery.Parse(discovery.Input{Country: " mx ", Municipality: " Coyoacán ", PostalCode: "04 000", MinLat: " 19.5 ", Query: "  local   bread "})
	if err != nil {
		t.Fatal(err)
	}
	if filter.Limit != 25 || filter.Query != "local bread" || !filter.HasBBox || !filter.HasAreaFilter || *filter.Country != "MX" || *filter.MunicipalityKey != "coyoacan" || *filter.PostalCodeKey != "04000" {
		t.Fatalf("filter %+v", filter)
	}
	service := discovery.New(nil)
	for _, in := range []discovery.Input{{Limit: 101}, {Offset: -1}, {MinLng: "181"}, {MinLat: "0.1234567"}, {Country: "!@"}, {MinLng: "2", MaxLng: "1"}} {
		_, err := service.Products(context.Background(), in)
		var validation *discovery.ValidationError
		if !errors.As(err, &validation) {
			t.Fatalf("invalid input reached storage or wrong error: %v", err)
		}
	}
}
