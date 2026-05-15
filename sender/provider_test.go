package sender

import "testing"

func TestGetProvider_Unknown(t *testing.T) {
	provider := GetProvider("this-does-not-exist")

	if provider != nil {
		t.Fatal("expected nil for unknown provider")
	}
}
