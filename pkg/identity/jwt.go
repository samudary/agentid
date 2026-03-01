package identity

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"

	"github.com/golang-jwt/jwt/v5"
	"github.com/samudary/agentid/pkg/store"
)

// TaskClaims defines the JWT claims for a task identity token.
type TaskClaims struct {
	jwt.RegisteredClaims
	Purpose            string                 `json:"purpose"`
	Scopes             []string               `json:"scopes"`
	DelegationChain    []store.DelegationLink `json:"delegation_chain"`
	PolicyContext      map[string]string      `json:"policy_context,omitempty"`
	MaxDelegationDepth int                    `json:"max_delegation_depth"`
	MaxTTLSeconds      int                    `json:"max_ttl_seconds"`
}

// KeyPair holds an ES256 (ECDSA P-256) key pair for JWT signing and verification.
type KeyPair struct {
	Private *ecdsa.PrivateKey
	Public  *ecdsa.PublicKey
}

// GenerateKeyPair creates a new ES256 key pair.
func GenerateKeyPair() (*KeyPair, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ES256 key: %w", err)
	}
	return &KeyPair{Private: key, Public: &key.PublicKey}, nil
}

// SaveKeyPair writes an ES256 private key to a PEM file.
// The file is created with 0600 permissions (owner read/write only).
func SaveKeyPair(kp *KeyPair, path string) error {
	der, err := x509.MarshalECPrivateKey(kp.Private)
	if err != nil {
		return fmt.Errorf("marshal private key: %w", err)
	}
	block := &pem.Block{Type: "EC PRIVATE KEY", Bytes: der}
	return os.WriteFile(path, pem.EncodeToMemory(block), 0600)
}

// LoadKeyPair reads an ES256 private key from a PEM file and derives the
// public key from it.
func LoadKeyPair(path string) (*KeyPair, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read key file: %w", err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found in %s", path)
	}
	key, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse EC private key: %w", err)
	}
	if key.Curve != elliptic.P256() {
		return nil, fmt.Errorf("expected P-256 key, got %s", key.Curve.Params().Name)
	}
	return &KeyPair{Private: key, Public: &key.PublicKey}, nil
}

// LoadOrGenerateKeyPair loads a key pair from path if the file exists,
// otherwise generates a new key pair and saves it to path.
func LoadOrGenerateKeyPair(path string) (*KeyPair, error) {
	if _, err := os.Stat(path); err == nil {
		return LoadKeyPair(path)
	}
	kp, err := GenerateKeyPair()
	if err != nil {
		return nil, err
	}
	if err := SaveKeyPair(kp, path); err != nil {
		return nil, fmt.Errorf("save generated key: %w", err)
	}
	return kp, nil
}

// SignToken signs the given claims with the ES256 private key and returns
// the compact JWT string.
func SignToken(claims TaskClaims, key *ecdsa.PrivateKey) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	signed, err := token.SignedString(key)
	if err != nil {
		return "", fmt.Errorf("sign token: %w", err)
	}
	return signed, nil
}

// VerifyToken parses and validates a JWT string using the ES256 public key.
// It checks signature, expiry, and issuer (must be "agentid").
// Returns the parsed TaskClaims on success.
func VerifyToken(tokenString string, key *ecdsa.PublicKey) (*TaskClaims, error) {
	claims := &TaskClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (interface{}, error) {
		if t.Method.Alg() != jwt.SigningMethodES256.Alg() {
			return nil, fmt.Errorf("unexpected signing method: %s", t.Method.Alg())
		}
		return key, nil
	},
		jwt.WithIssuer("agentid"),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return nil, fmt.Errorf("verify token: %w", err)
	}
	if !token.Valid {
		return nil, fmt.Errorf("token is not valid")
	}
	return claims, nil
}
