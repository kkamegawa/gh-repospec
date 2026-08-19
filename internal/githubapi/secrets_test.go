package githubapi

import (
	"crypto/rand"
	"encoding/base64"
	"testing"

	"golang.org/x/crypto/nacl/box"
)

func TestEncryptSecretRoundTripsWithTheGitHubPublicKey(t *testing.T) {
	public, private, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	plaintext := []byte("do-not-log-this-secret")

	encrypted, err := EncryptSecret(PublicKey{
		KeyID: "key-id",
		Key:   base64.StdEncoding.EncodeToString(public[:]),
	}, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	if encrypted.KeyID != "key-id" || encrypted.EncryptedValue == "" {
		t.Fatalf("encrypted secret metadata = %#v", encrypted)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(encrypted.EncryptedValue)
	if err != nil {
		t.Fatal(err)
	}
	decrypted, ok := box.OpenAnonymous(nil, ciphertext, public, private)
	if !ok || string(decrypted) != string(plaintext) {
		t.Fatalf("decrypted secret = %q, ok=%t", decrypted, ok)
	}
}

func TestEncryptSecretRejectsIncompleteOrMalformedKeys(t *testing.T) {
	t.Parallel()
	for name, publicKey := range map[string]PublicKey{
		"missing key ID":   {Key: base64.StdEncoding.EncodeToString(make([]byte, 32))},
		"invalid base64":   {KeyID: "key", Key: "%"},
		"incorrect length": {KeyID: "key", Key: base64.StdEncoding.EncodeToString(make([]byte, 31))},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := EncryptSecret(publicKey, []byte("secret")); err == nil {
				t.Fatal("EncryptSecret() unexpectedly accepted an invalid key")
			}
		})
	}
}
