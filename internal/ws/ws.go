// Package ws contains websocket operations
package ws

import (
	"net/http"
	"time"

	_websocket "github.com/gorilla/websocket"
)

const (
	writeWait = 10 * time.Second
)

// IWebsocket represents a websocket
type IWebsocket interface {
	SetReadDeadline(t time.Time) error
	SetReadLimit(int64)
	SetPongHandler(h func(appData string) error)

	ReadMessage() ([]byte, error)

	WriteMessage(data []byte) error
	WritePingMessage() error
	WriteCloseMessage() error

	Close() error
}

// IUpgrader interface to encapsulate the websocket upgrade procedure
type IUpgrader interface {
	Upgrade(w http.ResponseWriter, r *http.Request) (IWebsocket, error)
}

// Upgrader is the default upgrader
type Upgrader struct {
	upgrader _websocket.Upgrader
}

type websocket struct {
	conn *_websocket.Conn
}

func (ws *websocket) SetReadDeadline(t time.Time) error {
	return ws.conn.SetReadDeadline(t)
}

func (ws *websocket) SetReadLimit(l int64) {
	ws.conn.SetReadLimit(l)
}

func (ws *websocket) SetPongHandler(h func(appData string) error) {
	ws.conn.SetPongHandler(h)
}

func (ws *websocket) ReadMessage() (p []byte, err error) {
	_, bytes, err := ws.conn.ReadMessage()
	return bytes, err
}

func (ws *websocket) WriteMessage(data []byte) error {
	if err := ws.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
		return err
	}

	return ws.conn.WriteMessage(_websocket.BinaryMessage, data)
}

func (ws *websocket) WritePingMessage() error {
	if err := ws.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
		return err
	}

	return ws.conn.WriteMessage(_websocket.PingMessage, []byte{})
}

func (ws *websocket) WriteCloseMessage() error {
	if err := ws.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
		return err
	}

	return ws.conn.WriteMessage(_websocket.CloseMessage, []byte{})
}

func (ws *websocket) Close() error {
	return ws.conn.Close()
}

// IsUnexpectedCloseError returns true if the error is not a normal ws close error
func IsUnexpectedCloseError(err error) bool {
	return _websocket.IsUnexpectedCloseError(err, _websocket.CloseGoingAway, _websocket.CloseAbnormalClosure)
}

// Dial open a websocket connection to the given url
func Dial(url string) (IWebsocket, error) {
	conn, resp, err := _websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		return &websocket{}, err
	}

	if err := resp.Body.Close(); err != nil {
		return &websocket{}, err
	}

	return &websocket{conn: conn}, nil
}

// MakeUpgrader creates default upgrader
func MakeUpgrader() IUpgrader {
	upgrader := _websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     func(r *http.Request) bool { return true },
	}

	return &Upgrader{upgrader: upgrader}
}

// Upgrade upgrades a websocket HTTP request into ws protocol
func (upgrader *Upgrader) Upgrade(w http.ResponseWriter, r *http.Request) (IWebsocket, error) {
	conn, err := upgrader.upgrader.Upgrade(w, r, nil)

	if err != nil {
		return nil, err
	}

	return &websocket{conn: conn}, nil
}
