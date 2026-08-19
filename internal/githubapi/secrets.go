package githubapi

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"

	"golang.org/x/crypto/nacl/box"
)

// EncryptSecret encrypts a plaintext value using the public key returned by
// GitHub's Actions or Agents secrets API. The plaintext is never returned.
func EncryptSecret(publicKey PublicKey, plaintext []byte) (EncryptedSecret, error) {
	if publicKey.KeyID == "" || publicKey.Key == "" {
		return EncryptedSecret{}, errors.New("GitHub secret public key is incomplete")
	}
	decoded, err := base64.StdEncoding.DecodeString(publicKey.Key)
	if err != nil {
		return EncryptedSecret{}, fmt.Errorf("decode GitHub secret public key: %w", err)
	}
	if len(decoded) != 32 {
		return EncryptedSecret{}, fmt.Errorf("GitHub secret public key must be 32 bytes, got %d", len(decoded))
	}
	var recipient [32]byte
	copy(recipient[:], decoded)
	ciphertext, err := box.SealAnonymous(nil, plaintext, &recipient, rand.Reader)
	if err != nil {
		return EncryptedSecret{}, fmt.Errorf("encrypt GitHub secret: %w", err)
	}
	return EncryptedSecret{EncryptedValue: base64.StdEncoding.EncodeToString(ciphertext), KeyID: publicKey.KeyID}, nil
}
