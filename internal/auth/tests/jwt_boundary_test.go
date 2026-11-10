package auth_test

import (
	"crypto/rand"
	"crypto/rsa"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/palmieridev/openlocal-api/internal/auth"
)

func TestVerifiedJWTBoundary(t *testing.T) {
	verifier, key := newVerifier(t, []string{"https://shop.example"})
	other, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	reached := 0
	app := fiber.New()
	app.Post("/privileged", auth.Middleware{Verifier: verifier}.RequireAuth(), func(c *fiber.Ctx) error {
		reached++
		a, ok := auth.FromFiber(c)
		if !ok || a.ClerkUserID != "user_fixture" || a.ClerkOrgID != "org_fixture" || a.Role != "owner" {
			t.Errorf("verified identity = %+v", a)
		}
		return c.SendStatus(204)
	})
	for _, name := range []string{"valid v1", "valid v2", "wrong issuer", "missing expiry", "expired", "not yet valid", "missing subject", "wrong party", "missing party", "wrong signature", "unknown key", "wrong algorithm"} {
		t.Run(name, func(t *testing.T) {
			claims := jwt.MapClaims{"iss": "https://issuer.example", "sub": "user_fixture", "exp": time.Now().Add(time.Hour).Unix(), "azp": "https://shop.example", "org_id": "org_fixture", "org_role": "org:admin"}
			signingKey := any(key)
			method := jwt.SigningMethod(jwt.SigningMethodRS256)
			kid := "fixture-key"
			want := 401
			switch name {
			case "valid v1":
				want = 204
			case "valid v2":
				want = 204
				delete(claims, "org_id")
				delete(claims, "org_role")
				claims["o"] = map[string]string{"id": "org_fixture", "rol": "admin"}
			case "wrong issuer":
				claims["iss"] = "https://evil.example"
			case "missing expiry":
				delete(claims, "exp")
			case "expired":
				claims["exp"] = time.Now().Add(-time.Hour).Unix()
			case "not yet valid":
				claims["nbf"] = time.Now().Add(time.Hour).Unix()
			case "missing subject":
				delete(claims, "sub")
			case "wrong party":
				claims["azp"] = "https://evil.example"
			case "missing party":
				delete(claims, "azp")
			case "wrong signature":
				signingKey = other
			case "unknown key":
				kid = "unknown"
			case "wrong algorithm":
				method = jwt.SigningMethodHS256
				signingKey = []byte("fixture-only")
			}
			token := jwt.NewWithClaims(method, claims)
			token.Header["kid"] = kid
			signed, err := token.SignedString(signingKey)
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest("POST", "/privileged", nil)
			req.Header.Set("Authorization", "Bearer "+signed)
			before := reached
			res, err := app.Test(req, 5000)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			if res.StatusCode != want {
				t.Fatalf("status=%d want=%d", res.StatusCode, want)
			}
			if want == 401 && reached != before {
				t.Fatal("invalid claims reached privileged handler")
			}
		})
	}
}
