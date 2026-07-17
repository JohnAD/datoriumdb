package config

import "strings"

// AuthKey is one public signing key entry in __auth.json.
type AuthKey struct {
	Kid       string `json:"kid"`
	Alg       string `json:"alg"`
	Status    string `json:"status"`
	PublicKey string `json:"publicKey"`
}

// TokenLifetimeSeconds holds client/machine default token lifetimes.
type TokenLifetimeSeconds struct {
	Client  int `json:"client"`
	Machine int `json:"machine"`
}

// AuthBody is the "auth" object inside __auth.json.
type AuthBody struct {
	Issuer               string               `json:"issuer"`
	Audience             string               `json:"audience"`
	TokenLifetimeSeconds TokenLifetimeSeconds `json:"tokenLifetimeSeconds"`
	Keys                 []AuthKey            `json:"keys"`
}

// AuthFile is __auth.json.
type AuthFile struct {
	Auth AuthBody `json:"auth"`
}

// KeyByKid returns the key entry with the given kid, if present.
func (a *AuthFile) KeyByKid(kid string) (int, bool) {
	for i, k := range a.Auth.Keys {
		if k.Kid == kid {
			return i, true
		}
	}
	return 0, false
}

// ActiveKeyCount returns the number of keys with status "active".
func (a *AuthFile) ActiveKeyCount() int {
	n := 0
	for _, k := range a.Auth.Keys {
		if k.Status == "active" {
			n++
		}
	}
	return n
}

// LooksLikePrivateKeyMaterial performs a best-effort heuristic check that
// rejects PEM private keys and JWK private-key components.
func LooksLikePrivateKeyMaterial(material string) bool {
	upper := strings.ToUpper(material)
	if strings.Contains(upper, "PRIVATE KEY") {
		return true
	}
	if strings.Contains(material, "\"d\"") {
		// JWK private scalar component for EC/OKP keys.
		return true
	}
	return false
}
