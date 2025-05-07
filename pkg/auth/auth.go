package auth

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

type GithubAppTokenReqPermissions struct {
	Repositories []string          `json:"repositories"`
	Permissions  map[string]string `json:"permissions"`
}

type GithubAppToken struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

func GithubAppInstallationToken(ctx context.Context,
	appID, installationID, privateKeyPath string, reqPerms GithubAppTokenReqPermissions,
) (*GithubAppToken, error) {
	privatePEMData, err := os.ReadFile(privateKeyPath)
	if err != nil {
		return nil, err
	}

	block, _ := pem.Decode(privatePEMData)
	if block == nil || block.Type != "RSA PRIVATE KEY" {
		return nil, fmt.Errorf("failed to decode PEM block containing private key")
	}

	privateKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}

	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: privateKey}, nil)
	if err != nil {
		return nil, err
	}

	cl := jwt.Claims{
		// GitHub App's ID or client ID
		Issuer: appID,
		// issued at time, 60 seconds in the past to allow for clock drift
		IssuedAt: jwt.NewNumericDate(time.Now().Add(-60 * time.Second)),
		// JWT expiration time (10 minute maximum)
		Expiry: jwt.NewNumericDate(time.Now().Add(10 * time.Minute)),
	}

	jwtToken, err := jwt.Signed(signer).Claims(cl).Serialize()
	if err != nil {
		return nil, err
	}

	reqBody, err := json.Marshal(reqPerms)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("https://api.github.com/app/installations/%s/access_tokens", installationID)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+jwtToken)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		errMessage, err := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub app token response status %d, body:%q  err:%w", resp.StatusCode, errMessage, err)
	}

	var tokenResponse GithubAppToken
	if err := json.NewDecoder(resp.Body).Decode(&tokenResponse); err != nil {
		return nil, err
	}

	return &tokenResponse, nil
}
