package main

import (
	"bytes"
	"testing"
)

func TestSettingsEncryptionRoundTrip(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	first, err := encryptSecret(key, "personal-token")
	if err != nil {
		t.Fatal(err)
	}
	second, err := encryptSecret(key, "personal-token")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("AES-GCM ciphertext must use a fresh nonce")
	}
	plaintext, err := decryptSecret(key, first)
	if err != nil {
		t.Fatal(err)
	}
	if plaintext != "personal-token" {
		t.Fatalf("plaintext = %q", plaintext)
	}
}
