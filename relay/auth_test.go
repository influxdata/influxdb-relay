package relay

import (
	"testing"
)

func newMockHTTPOutputConfig(user string, pass string) *HTTPOutputConfig {
	return &HTTPOutputConfig{
		Name:                "test-backend",
		Location:            "localhost:8086",
		Timeout:             "10s",
		BufferSizeMB:        0,
		MaxBatchKB:          512,
		MaxDelayInterval:    "10",
		SkipTLSVerification: false,
		HTTPBasicAuthUser:   user,
		HTTPBasicAuthPass:   pass,
	}
}

func TestGetAuthorizationString(t *testing.T) {
	for _, tbl := range []struct {
		cfg  *HTTPOutputConfig
		auth string // Client auth header
		exp  string // expected result

	}{
		{
			cfg:  newMockHTTPOutputConfig("", ""),
			auth: "",
			exp:  "",
		},
		{
			cfg:  newMockHTTPOutputConfig("", ""),
			auth: "client-header",
			exp:  "client-header",
		},
		{
			cfg:  newMockHTTPOutputConfig("admin", "password"),
			auth: "client-header-present-but-not-preferred",
			exp:  "Basic YWRtaW46cGFzc3dvcmQ=", // admin:password
		},
	} {
		test_auth := NewHTTPAuth(tbl.cfg)
		res := test_auth.GetAuthorizationString(tbl.auth)
		if tbl.exp != res {
			t.Errorf("Expected %s, got %s", tbl.exp, res)
		}
	}
}
