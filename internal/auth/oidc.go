package auth

import (
	"context"
	"fmt"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
)

type OIDCVerifier struct{ verifier *oidc.IDTokenVerifier }

func NewOIDCVerifier(ctx context.Context, issuer, audience string) (*OIDCVerifier, error) {
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("discover OIDC provider: %w", err)
	}
	return &OIDCVerifier{verifier: provider.Verifier(&oidc.Config{ClientID: audience})}, nil
}

func (v *OIDCVerifier) Verify(ctx context.Context, raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", fmt.Errorf("bearer token is required")
	}
	token, err := v.verifier.Verify(ctx, raw)
	if err != nil {
		return "", fmt.Errorf("verify bearer token: %w", err)
	}
	if strings.TrimSpace(token.Subject) == "" {
		return "", fmt.Errorf("bearer token has no subject")
	}
	return token.Subject, nil
}
