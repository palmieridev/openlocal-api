package discovery

import "context"

type Service struct{ store Store }

func New(store Store) *Service { return &Service{store} }
func (s *Service) Business(ctx context.Context, value string) (Business, error) {
	value = slug(value)
	if err := validateSlug(value); err != nil {
		return Business{}, invalid(err.Error())
	}
	return s.store.Business(ctx, value)
}
func (s *Service) Businesses(ctx context.Context, in Input) ([]Business, error) {
	filter, err := Parse(in)
	if err != nil {
		return nil, err
	}
	return s.store.Businesses(ctx, filter)
}
func (s *Service) Products(ctx context.Context, in Input) ([]Product, error) {
	filter, err := Parse(in)
	if err != nil {
		return nil, err
	}
	return s.store.Products(ctx, filter)
}
