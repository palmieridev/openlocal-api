package management

import (
	"context"
	"strings"

	"github.com/google/uuid"
)

type Service struct {
	products Products
	variants Variants
	public   PublicProducts
}

func New(products Products, variants Variants, public PublicProducts) *Service {
	return &Service{products: products, variants: variants, public: public}
}

func requireIdentity(identity Identity) error {
	if identity.BusinessID == uuid.Nil || identity.ActorID == uuid.Nil {
		return invalid("authorized business and actor are required")
	}
	return nil
}

func page(input Page) (Page, error) {
	if input.Limit == 0 {
		input.Limit = 25
	}
	if input.Limit < 1 || input.Limit > 100 {
		return Page{}, invalid("limit must be between 1 and 100")
	}
	if input.Offset < 0 || input.Offset > 2_000_000_000 {
		return Page{}, invalid("offset must be between 0 and 2000000000")
	}
	return input, nil
}

func (s *Service) CreateProduct(ctx context.Context, identity Identity, input ProductInput) (Product, error) {
	if err := requireIdentity(identity); err != nil {
		return Product{}, err
	}
	data, err := ParseProduct(input)
	if err != nil {
		return Product{}, err
	}
	return s.products.CreateProduct(ctx, identity.BusinessID, data)
}

func (s *Service) UpdateProduct(ctx context.Context, identity Identity, id uuid.UUID, input ProductInput) (Product, error) {
	if err := requireIdentity(identity); err != nil {
		return Product{}, err
	}
	data, err := ParseProduct(input)
	if err != nil {
		return Product{}, err
	}
	if _, err := s.products.Product(ctx, identity.BusinessID, id); err != nil {
		return Product{}, err
	}
	// The store deliberately does not update slug or business ownership.
	return s.products.UpdateProduct(ctx, identity.BusinessID, id, data)
}

func (s *Service) Product(ctx context.Context, identity Identity, id uuid.UUID) (Product, error) {
	if err := requireIdentity(identity); err != nil {
		return Product{}, err
	}
	return s.products.Product(ctx, identity.BusinessID, id)
}

func (s *Service) Products(ctx context.Context, identity Identity, input Page) ([]Product, error) {
	if err := requireIdentity(identity); err != nil {
		return nil, err
	}
	input, err := page(input)
	if err != nil {
		return nil, err
	}
	return s.products.Products(ctx, identity.BusinessID, input)
}

func (s *Service) ArchiveProduct(ctx context.Context, identity Identity, id uuid.UUID) error {
	if err := requireIdentity(identity); err != nil {
		return err
	}
	return s.products.ArchiveProduct(ctx, identity.BusinessID, id)
}

func (s *Service) CreateVariant(ctx context.Context, identity Identity, input VariantInput) (Variant, error) {
	return s.writeVariant(ctx, identity, uuid.Nil, input)
}

func (s *Service) UpdateVariant(ctx context.Context, identity Identity, id uuid.UUID, input VariantInput) (Variant, error) {
	if id == uuid.Nil {
		return Variant{}, ErrNotFound
	}
	return s.writeVariant(ctx, identity, id, input)
}

func (s *Service) writeVariant(ctx context.Context, identity Identity, id uuid.UUID, input VariantInput) (Variant, error) {
	if err := requireIdentity(identity); err != nil {
		return Variant{}, err
	}
	data, err := ParseVariant(input)
	if err != nil {
		return Variant{}, err
	}
	image, err := ParseImageURL(input.ImageURL)
	if err != nil {
		return Variant{}, err
	}
	tx, err := s.variants.Begin(ctx)
	if err != nil {
		return Variant{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if id == uuid.Nil {
		if err := tx.RequireProduct(ctx, identity.BusinessID, data.ProductID); err != nil {
			return Variant{}, err
		}
	}
	var result Variant
	if id == uuid.Nil {
		if data.IsPublic == nil {
			value := true
			data.IsPublic = &value
		}
		result, err = tx.CreateVariant(ctx, identity.BusinessID, data)
	} else {
		_, readErr := tx.LockVariant(ctx, identity.BusinessID, id)
		if readErr != nil {
			return Variant{}, readErr
		}
		result, err = tx.UpdateVariant(ctx, identity.BusinessID, id, data)
	}
	if err != nil {
		return Variant{}, err
	}
	result.ImageURL, err = tx.ApplyImage(ctx, result.ID, image)
	if err != nil {
		return Variant{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Variant{}, err
	}
	return result, nil
}

func (s *Service) Variant(ctx context.Context, identity Identity, id uuid.UUID) (Variant, error) {
	if err := requireIdentity(identity); err != nil {
		return Variant{}, err
	}
	return s.variants.Variant(ctx, identity.BusinessID, id)
}

func (s *Service) Variants(ctx context.Context, identity Identity, productID uuid.UUID, input Page) ([]Variant, error) {
	if err := requireIdentity(identity); err != nil {
		return nil, err
	}
	input, err := page(input)
	if err != nil {
		return nil, err
	}
	if _, err := s.products.Product(ctx, identity.BusinessID, productID); err != nil {
		return nil, err
	}
	return s.variants.Variants(ctx, identity.BusinessID, productID, input)
}

func (s *Service) ByBarcode(ctx context.Context, identity Identity, barcode string) (Variant, error) {
	if err := requireIdentity(identity); err != nil {
		return Variant{}, err
	}
	barcode = clean(barcode)
	if err := stringLength(barcode, "barcode", 1, 128); err != nil {
		return Variant{}, err
	}
	return s.variants.ByBarcode(ctx, identity.BusinessID, barcode)
}

func (s *Service) BySKU(ctx context.Context, identity Identity, sku string) (Variant, error) {
	if err := requireIdentity(identity); err != nil {
		return Variant{}, err
	}
	sku = strings.ToUpper(clean(sku))
	if err := stringLength(sku, "sku", 2, 80); err != nil {
		return Variant{}, err
	}
	return s.variants.BySKU(ctx, identity.BusinessID, sku)
}

func (s *Service) ArchiveVariant(ctx context.Context, identity Identity, id uuid.UUID) error {
	if err := requireIdentity(identity); err != nil {
		return err
	}
	return s.variants.ArchiveVariant(ctx, identity.BusinessID, id)
}

func (s *Service) PublicProducts(ctx context.Context, businessSlug string, input Page) ([]PublicProduct, error) {
	input, err := page(input)
	if err != nil {
		return nil, err
	}
	businessSlug = slug(businessSlug)
	if err := validateSlug(businessSlug); err != nil {
		return nil, invalid(err.Error())
	}
	return s.public.PublicProducts(ctx, businessSlug, input)
}
