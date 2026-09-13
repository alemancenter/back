package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/imanjo/fiber-api/internal/config"
)

func newTestOAuthHandler() *Handler {
	return &Handler{
		cfg: &config.Config{
			App:      config.AppConfig{Env: "production"},
			Frontend: config.FrontendConfig{URL: "https://imanjo.com"},
		},
	}
}

// A valid callback must present the exact nonce beginOAuthFlow issued, and the redirect_to
// embedded alongside it must come back intact. This is the actual CSRF protection this session
// added — GoogleRedirect/FacebookRedirect used to pass the literal string "state" with nothing
// checked on the way back, so anyone could construct their own callback URL with a `code` for
// their own account and get the victim silently logged into it.
func TestOAuthState_RoundTrip(t *testing.T) {
	h := newTestOAuthHandler()
	app := fiber.New()

	var capturedState string
	app.Get("/redirect", func(c *fiber.Ctx) error {
		state, err := h.beginOAuthFlow(c, "/account/messages")
		if err != nil {
			t.Fatalf("beginOAuthFlow: %v", err)
		}
		capturedState = state
		return c.SendString("ok")
	})

	var gotRedirectTo string
	var gotOK bool
	app.Get("/callback", func(c *fiber.Ctx) error {
		gotRedirectTo, gotOK = h.verifyOAuthState(c)
		return c.SendString("ok")
	})

	redirectReq := httptest.NewRequest("GET", "/redirect", nil)
	redirectResp, err := app.Test(redirectReq)
	if err != nil {
		t.Fatalf("redirect request: %v", err)
	}
	var stateCookie string
	for _, c := range redirectResp.Cookies() {
		if c.Name == oauthStateCookieName {
			stateCookie = c.Value
		}
	}
	if stateCookie == "" {
		t.Fatal("oauth_state cookie was not set")
	}
	if capturedState == "" {
		t.Fatal("beginOAuthFlow returned an empty state")
	}

	callbackReq := httptest.NewRequest("GET", "/callback?state="+capturedState, nil)
	callbackReq.AddCookie(&http.Cookie{Name: oauthStateCookieName, Value: stateCookie})
	if _, err := app.Test(callbackReq); err != nil {
		t.Fatalf("callback request: %v", err)
	}

	if !gotOK {
		t.Fatal("verifyOAuthState rejected a legitimate matching state/cookie pair")
	}
	if gotRedirectTo != "/account/messages" {
		t.Fatalf("redirect_to = %q, want %q", gotRedirectTo, "/account/messages")
	}
}

// The core CSRF check: a callback whose state nonce doesn't match the cookie (forged, replayed,
// or simply absent) must be rejected outright rather than proceeding to exchange a code.
func TestOAuthState_RejectsMismatchedNonce(t *testing.T) {
	h := newTestOAuthHandler()
	app := fiber.New()

	var gotOK bool
	app.Get("/callback", func(c *fiber.Ctx) error {
		_, gotOK = h.verifyOAuthState(c)
		return c.SendString("ok")
	})

	req := httptest.NewRequest("GET", "/callback?state=attacker-supplied-nonce|/", nil)
	req.AddCookie(&http.Cookie{Name: oauthStateCookieName, Value: "real-nonce-from-cookie"})
	if _, err := app.Test(req); err != nil {
		t.Fatalf("callback request: %v", err)
	}

	if gotOK {
		t.Fatal("verifyOAuthState accepted a state whose nonce does not match the cookie")
	}
}

func TestOAuthState_RejectsMissingCookie(t *testing.T) {
	h := newTestOAuthHandler()
	app := fiber.New()

	var gotOK bool
	app.Get("/callback", func(c *fiber.Ctx) error {
		_, gotOK = h.verifyOAuthState(c)
		return c.SendString("ok")
	})

	req := httptest.NewRequest("GET", "/callback?state=some-nonce|/", nil)
	if _, err := app.Test(req); err != nil {
		t.Fatalf("callback request: %v", err)
	}

	if gotOK {
		t.Fatal("verifyOAuthState accepted a state with no oauth_state cookie present at all")
	}
}

func TestSanitizeOAuthRedirectTo(t *testing.T) {
	cases := map[string]string{
		"/account/messages": "/account/messages",
		"":                  "",
		"//evil.com":        "",
		"https://evil.com":  "",
		"not-a-path":        "",
		"/a|b":              "",
	}
	for input, want := range cases {
		if got := sanitizeOAuthRedirectTo(input); got != want {
			t.Errorf("sanitizeOAuthRedirectTo(%q) = %q, want %q", input, got, want)
		}
	}
}
