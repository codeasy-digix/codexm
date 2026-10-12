package accounts

import "testing"

func TestLoginURLRestrictsOfficialHTTPSOrigins(t *testing.T) {
	for _, value := range []string{"https://auth.openai.com/authorize", "https://auth.openai.com:443/codex/device", "https://auth0.openai.com/authorize", "https://chatgpt.com/auth/login"} {
		if !officialAuthURL(value) {
			t.Fatal("official HTTPS authentication URL rejected")
		}
	}
	for _, value := range []string{"https://auth.openai.com:1234/authorize", "http://auth.openai.com/", "javascript:alert(1)", "https://auth.openai.com.evil.test/", "https://user:secret@auth.openai.com/", "https://example.com/"} {
		if officialAuthURL(value) {
			t.Fatal("untrusted authentication origin accepted")
		}
	}
}
