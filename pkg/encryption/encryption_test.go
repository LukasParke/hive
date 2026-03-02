package encryption

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	plaintext := []byte("secret database password")
	ciphertext, err := Encrypt(plaintext)
	require.NoError(t, err)
	assert.NotEqual(t, plaintext, ciphertext)

	decrypted, err := Decrypt(ciphertext)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)
}

func TestEncryptDecryptEmpty(t *testing.T) {
	plaintext := []byte("")
	ciphertext, err := Encrypt(plaintext)
	require.NoError(t, err)

	decrypted, err := Decrypt(ciphertext)
	require.NoError(t, err)
	assert.Len(t, decrypted, 0)
}

func TestEncryptDecryptLargePayload(t *testing.T) {
	plaintext := bytes.Repeat([]byte("A"), 10000)
	ciphertext, err := Encrypt(plaintext)
	require.NoError(t, err)

	decrypted, err := Decrypt(ciphertext)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)
}

func TestEncryptProducesDifferentCiphertexts(t *testing.T) {
	plaintext := []byte("same input")
	ct1, err := Encrypt(plaintext)
	require.NoError(t, err)
	ct2, err := Encrypt(plaintext)
	require.NoError(t, err)

	assert.NotEqual(t, ct1, ct2, "different nonces should produce different ciphertexts")
}

func TestDecryptInvalidData(t *testing.T) {
	_, err := Decrypt([]byte("this is not valid ciphertext at all!"))
	assert.Error(t, err)
}

func TestDecryptTruncatedData(t *testing.T) {
	_, err := Decrypt([]byte{0x01, 0x02})
	assert.Error(t, err)
}

func TestDecryptEmptyInput(t *testing.T) {
	_, err := Decrypt([]byte{})
	assert.Error(t, err)
}

func TestDecryptTamperedCiphertext(t *testing.T) {
	plaintext := []byte("sensitive data")
	ciphertext, err := Encrypt(plaintext)
	require.NoError(t, err)

	ciphertext[len(ciphertext)-1] ^= 0xFF
	_, err = Decrypt(ciphertext)
	assert.Error(t, err)
}
