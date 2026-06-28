package account

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProfileKeychainMapsAccountKey(t *testing.T) {
	fake := &fakeKeychain{data: map[string][]byte{}}
	kc := ProfileKeychain{Base: fake, ProfileID: "alice"}

	// ipatool writes under the fixed key "account".
	require.NoError(t, kc.Set("account", []byte("payload")))

	got, err := kc.Get("account")
	require.NoError(t, err)
	require.Equal(t, []byte("payload"), got)

	// Underlying store sees the namespaced key, proving isolation.
	require.Equal(t, []byte("payload"), fake.data["profiles/alice/account"])

	// A second profile does not collide.
	bob := &fakeKeychain{data: map[string][]byte{}}
	kc2 := ProfileKeychain{Base: bob, ProfileID: "bob"}
	require.NoError(t, kc2.Set("account", []byte("bob-payload")))
	require.Equal(t, []byte("bob-payload"), bob.data["profiles/bob/account"])
	require.NotContains(t, fake.data, "profiles/bob/account")
}

// fakeKeychain is a minimal in-memory implementation of ipatool's
// keychain.Keychain for tests. Satisfies ipakeychain.Keychain.
type fakeKeychain struct {
	data map[string][]byte
}

func (f *fakeKeychain) Get(key string) ([]byte, error) { return f.data[key], nil }

func (f *fakeKeychain) Set(key string, d []byte) error {
	f.data[key] = d
	return nil
}

func (f *fakeKeychain) Remove(key string) error {
	delete(f.data, key)
	return nil
}
