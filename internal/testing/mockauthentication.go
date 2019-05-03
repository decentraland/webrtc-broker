package testing

import (
	"net/url"

	protocol "github.com/decentraland/webrtc-broker/pkg/protocol"
)

type MockAuthenticator struct {
	Authenticate_        func(role protocol.Role, bytes []byte) (bool, error)
	AuthenticateQs_      func(role protocol.Role, qs url.Values) (bool, error)
	GenerateAuthMessage_ func(role protocol.Role) (*protocol.AuthMessage, error)
	GenerateAuthURL_     func(baseURL string, role protocol.Role) (string, error)
}

func (a *MockAuthenticator) Authenticate(role protocol.Role, bytes []byte) (bool, error) {
	return a.Authenticate_(role, bytes)
}

func (a *MockAuthenticator) AuthenticateQs(role protocol.Role, qs url.Values) (bool, error) {
	return a.AuthenticateQs_(role, qs)
}

func (a *MockAuthenticator) GenerateAuthMessage(role protocol.Role) (*protocol.AuthMessage, error) {
	return a.GenerateAuthMessage_(role)
}

func (a *MockAuthenticator) GenerateAuthURL(baseURL string, role protocol.Role) (string, error) {
	return a.GenerateAuthURL_(baseURL, role)
}

func MakeWithAuthResponse(isValid bool) *MockAuthenticator {
	return &MockAuthenticator{
		Authenticate_: func(role protocol.Role, bytes []byte) (bool, error) {
			return isValid, nil
		},
		AuthenticateQs_: func(role protocol.Role, qs url.Values) (bool, error) {
			return isValid, nil
		},
	}
}
