package relay

import (
	"encoding/base64"
	"fmt"
)

type HTTPAuth interface {
	GetAuthorizationString(string) string
}

type httpAuth struct {
	user string
	pass string
}

func (a *httpAuth) GetAuthorizationString(passthru string) string {
	if a.user != "" && a.pass != "" {
		return fmt.Sprintf("Basic %s", base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", a.user, a.pass))))
	}
	return passthru
}

func NewHTTPAuth(cfg *HTTPOutputConfig) *httpAuth {
	return &httpAuth{
		user: cfg.HTTPBasicAuthUser,
		pass: cfg.HTTPBasicAuthPass,
	}
}
