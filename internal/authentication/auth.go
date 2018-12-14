package authentication

import (
	"errors"
	"fmt"
	"net/url"

	protocol "github.com/decentraland/communications-server-go/pkg/protocol"
)

var UnsupportedAuthMethod = errors.New("Unsupported authentication method")

type Authenticator interface {
	Authenticate(role protocol.Role, bytes []byte) (bool, error)
	AuthenticateQs(role protocol.Role, qs url.Values) (bool, error)
	GenerateAuthMessage(role protocol.Role) (*protocol.AuthMessage, error)
	GenerateAuthURL(baseUrl string, role protocol.Role) (string, error)
}

type Authentication struct {
	methods map[string]Authenticator
}

type NoopAuthenticator struct{}

func (a *NoopAuthenticator) Authenticate(role protocol.Role, bytes []byte) (bool, error) {
	return true, nil
}

func (a *NoopAuthenticator) AuthenticateQs(role protocol.Role, qs url.Values) (bool, error) {
	return true, nil
}

func (a *NoopAuthenticator) GenerateAuthMessage(role protocol.Role) (*protocol.AuthMessage, error) {
	m := &protocol.AuthMessage{
		Type:   protocol.MessageType_AUTH,
		Role:   role,
		Method: "noop",
	}
	return m, nil
}

func (a *NoopAuthenticator) GenerateAuthURL(baseUrl string, role protocol.Role) (string, error) {
	u := fmt.Sprintf("%s?method=noop", baseUrl)
	return u, nil
}

func Make() Authentication {
	auth := Authentication{methods: make(map[string]Authenticator)}
	return auth
}

func (auth *Authentication) AddOrUpdateAuthenticator(method string, authenticator Authenticator) {
	auth.methods[method] = authenticator
}

func (auth *Authentication) Authenticate(method string, role protocol.Role, bytes []byte) (bool, error) {
	authenticator := auth.methods[method]

	if authenticator == nil {
		return false, UnsupportedAuthMethod
	}

	return authenticator.Authenticate(role, bytes)
}

func (auth *Authentication) AuthenticateQs(method string, role protocol.Role, qs url.Values) (bool, error) {
	authenticator := auth.methods[method]

	if authenticator == nil {
		return false, UnsupportedAuthMethod
	}

	return authenticator.AuthenticateQs(role, qs)
}

func (auth *Authentication) GenerateAuthMessage(method string, role protocol.Role) (*protocol.AuthMessage, error) {
	authenticator := auth.methods[method]

	if authenticator == nil {
		return nil, UnsupportedAuthMethod
	}

	return authenticator.GenerateAuthMessage(role)
}

func (auth *Authentication) GenerateAuthURL(method string, baseUrl string, role protocol.Role) (string, error) {
	authenticator := auth.methods[method]

	if authenticator == nil {
		return "", UnsupportedAuthMethod
	}

	return authenticator.GenerateAuthURL(baseUrl, role)
}
