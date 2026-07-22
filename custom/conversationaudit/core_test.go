package conversationaudit

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEncryptDecryptRejectsModifiedAAD(t *testing.T) {
	runtimeState.Lock()
	previousKeys := runtimeState.keys
	runtimeState.keys = map[string][]byte{"v1": []byte("12345678901234567890123456789012")}
	runtimeState.Unlock()
	t.Cleanup(func() {
		runtimeState.Lock()
		runtimeState.keys = previousKeys
		runtimeState.Unlock()
	})

	nonce, ciphertext, err := encrypt("v1", "confidential conversation", "request-1")
	require.NoError(t, err)
	plaintext, err := decrypt("v1", nonce, ciphertext, "request-1")
	require.NoError(t, err)
	require.Equal(t, "confidential conversation", plaintext)
	_, err = decrypt("v1", nonce, ciphertext, "different-request")
	require.Error(t, err)
}
