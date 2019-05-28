package authentication

import (
	"fmt"
	"net/url"

	"github.com/decentraland/webrtc-broker/pkg/protocol"
)

// ServerAuthenticator is the communication server authentication mechanism
type ServerAuthenticator interface {
	AuthenticateFromMessage(role protocol.Role, bytes []byte) (bool, error)
	GenerateServerAuthMessage() (*protocol.AuthMessage, error)
	GenerateServerConnectURL(coordinatorURL string) (string, error)
}

// CoordinatorAuthenticator is the coordiantor authentication mechanism
type CoordinatorAuthenticator interface {
	AuthenticateFromURL(role protocol.Role, qs url.Values) (bool, error)
}

// ClientAuthenticator is the client authentication mechanism, used for simulation only
type ClientAuthenticator interface {
	GenerateClientAuthMessage() (*protocol.AuthMessage, error)
	GenerateClientConnectURL(coordinatorURL string) (string, error)
}

// NoopAuthenticator is a Server|Coordinator|Client authenticator that does nothing
type NoopAuthenticator struct{}

// AuthenticateFromMessage always return true
func (a *NoopAuthenticator) AuthenticateFromMessage(role protocol.Role, bytes []byte) (bool, error) {
	return true, nil
}

// AuthenticateFromURL always return true
func (a *NoopAuthenticator) AuthenticateFromURL(role protocol.Role, qs url.Values) (bool, error) {
	return true, nil
}

// GenerateServerAuthMessage generates server empty auth message
func (a *NoopAuthenticator) GenerateServerAuthMessage() (*protocol.AuthMessage, error) {
	m := &protocol.AuthMessage{
		Type: protocol.MessageType_AUTH,
		Role: protocol.Role_COMMUNICATION_SERVER,
	}
	return m, nil
}

// GenerateClientAuthMessage generates client empty auth message
func (a *NoopAuthenticator) GenerateClientAuthMessage() (*protocol.AuthMessage, error) {
	m := &protocol.AuthMessage{
		Type: protocol.MessageType_AUTH,
		Role: protocol.Role_CLIENT,
	}
	return m, nil
}

// GenerateServerConnectURL generates CoordinatorURL with no parameters
func (a *NoopAuthenticator) GenerateServerConnectURL(coordinatorURL string) (string, error) {
	u := fmt.Sprintf("%s?method=noop", coordinatorURL)
	return u, nil
}

// GenerateClientConnectURL generates CoordinatorURL with no parameters
func (a *NoopAuthenticator) GenerateClientConnectURL(coordinatorURL string) (string, error) {
	u := fmt.Sprintf("%s?method=noop", coordinatorURL)
	return u, nil
}
