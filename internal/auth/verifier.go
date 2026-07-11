package auth

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type Verifier struct {
	issuer            string
	jwksURL           string
	authorizedParties map[string]struct{}
	client            *http.Client
	mu                sync.RWMutex
	keys              map[string]*rsa.PublicKey
	loaded            time.Time
}

func NewVerifier(issuer, jwksURL string, authorizedParties []string) *Verifier {
	parties := make(map[string]struct{}, len(authorizedParties))
	for _, party := range authorizedParties {
		party = strings.TrimRight(strings.TrimSpace(party), "/")
		if party != "" {
			parties[party] = struct{}{}
		}
	}
	return &Verifier{
		issuer:            strings.TrimRight(issuer, "/"),
		jwksURL:           jwksURL,
		authorizedParties: parties,
		client:            &http.Client{Timeout: 5 * time.Second},
		keys:              map[string]*rsa.PublicKey{},
	}
}

type Claims struct {
	Email           string    `json:"email"`
	FirstName       string    `json:"first_name"`
	LastName        string    `json:"last_name"`
	ImageURL        string    `json:"image_url"`
	OrgID           string    `json:"org_id"`
	OrgRole         string    `json:"org_role"`
	Org             *orgClaim `json:"o"`
	AuthorizedParty string    `json:"azp"`
	jwt.RegisteredClaims
}

// orgClaim is the active-organization object in Clerk v2 session tokens
// (`o: {id, rol, slg}`), which replaced the flat v1 `org_id`/`org_role` claims.
type orgClaim struct {
	ID   string `json:"id"`
	Role string `json:"rol"`
	Slug string `json:"slg"`
}

func (v *Verifier) Verify(ctx context.Context, token string) (Claims, error) {
	var claims Claims
	parsed, err := jwt.ParseWithClaims(token, &claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, errors.New("unexpected jwt signing method")
		}
		kid, _ := t.Header["kid"].(string)
		if kid == "" {
			return nil, errors.New("jwt kid is missing")
		}
		return v.key(ctx, kid)
	}, jwt.WithIssuer(v.issuer), jwt.WithExpirationRequired())
	if err != nil {
		return Claims{}, err
	}
	if !parsed.Valid || claims.Subject == "" {
		return Claims{}, errors.New("invalid jwt")
	}
	if err := v.verifyAuthorizedParty(claims.AuthorizedParty); err != nil {
		return Claims{}, err
	}
	claims.applyOrgFallback()
	return claims, nil
}

// applyOrgFallback copies the Clerk v2 nested `o` claim into the flat
// OrgID/OrgRole fields when the v1 claims are absent, so downstream code can
// read OrgID/OrgRole regardless of token version.
func (c *Claims) applyOrgFallback() {
	if c.Org == nil {
		return
	}
	if c.OrgID == "" {
		c.OrgID = c.Org.ID
	}
	if c.OrgRole == "" {
		c.OrgRole = c.Org.Role
	}
}

func (v *Verifier) verifyAuthorizedParty(azp string) error {
	if len(v.authorizedParties) == 0 {
		return nil
	}
	azp = strings.TrimRight(strings.TrimSpace(azp), "/")
	if azp == "" {
		return errors.New("jwt authorized party is missing")
	}
	if _, ok := v.authorizedParties[azp]; !ok {
		return errors.New("jwt authorized party is not allowed")
	}
	return nil
}

func (v *Verifier) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.mu.RLock()
	key, ok := v.keys[kid]
	fresh := time.Since(v.loaded) < 5*time.Minute
	v.mu.RUnlock()
	if ok && fresh {
		return key, nil
	}
	if err := v.refresh(ctx); err != nil {
		return nil, err
	}
	v.mu.RLock()
	defer v.mu.RUnlock()
	key, ok = v.keys[kid]
	if !ok {
		return nil, errors.New("jwt key not found")
	}
	return key, nil
}

func (v *Verifier) refresh(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.jwksURL, nil)
	if err != nil {
		return err
	}
	res, err := v.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return errors.New("jwks fetch failed")
	}
	var jwks struct {
		Keys []struct {
			Kty string `json:"kty"`
			Kid string `json:"kid"`
			Use string `json:"use"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(res.Body).Decode(&jwks); err != nil {
		return err
	}
	keys := make(map[string]*rsa.PublicKey, len(jwks.Keys))
	for _, raw := range jwks.Keys {
		if raw.Kty != "RSA" || raw.Kid == "" {
			continue
		}
		n, err := base64.RawURLEncoding.DecodeString(raw.N)
		if err != nil {
			return err
		}
		eBytes, err := base64.RawURLEncoding.DecodeString(raw.E)
		if err != nil {
			return err
		}
		e := 0
		for _, b := range eBytes {
			e = e<<8 + int(b)
		}
		keys[raw.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: e}
	}
	v.mu.Lock()
	v.keys = keys
	v.loaded = time.Now()
	v.mu.Unlock()
	return nil
}
