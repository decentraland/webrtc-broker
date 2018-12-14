package ws

import (
	"net/http"
	"time"

	_websocket "github.com/gorilla/websocket"
)

type IWebsocket interface {
	SetWriteDeadline(t time.Time) error
	SetReadDeadline(t time.Time) error
	SetReadLimit(int64)
	SetPongHandler(h func(appData string) error)

	ReadMessage() ([]byte, error)

	WriteMessage(data []byte) error
	WritePingMessage() error
	WriteCloseMessage() error

	Close() error
}

type IUpgrader interface {
	Upgrade(w http.ResponseWriter, r *http.Request) (IWebsocket, error)
}

type Upgrader struct {
	upgrader _websocket.Upgrader
}

type websocket struct {
	conn *_websocket.Conn
}

func (ws *websocket) SetWriteDeadline(t time.Time) error {
	return ws.conn.SetWriteDeadline(t)
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
	return ws.conn.WriteMessage(_websocket.BinaryMessage, data)
}

func (ws *websocket) WritePingMessage() error {
	return ws.conn.WriteMessage(_websocket.PingMessage, []byte{})
}

func (ws *websocket) WriteCloseMessage() error {
	return ws.conn.WriteMessage(_websocket.CloseMessage, []byte{})
}

func (ws *websocket) Close() error {
	return ws.conn.Close()
}

func (upgrader *Upgrader) Upgrade(w http.ResponseWriter, r *http.Request) (IWebsocket, error) {
	conn, err := upgrader.upgrader.Upgrade(w, r, nil)

	if err != nil {
		return nil, err
	}

	return &websocket{conn: conn}, nil
}

func IsUnexpectedCloseError(err error) bool {
	return _websocket.IsUnexpectedCloseError(err, _websocket.CloseGoingAway, _websocket.CloseAbnormalClosure)
}

func Dial(url string) (IWebsocket, error) {
	conn, _, err := _websocket.DefaultDialer.Dial(url, nil)

	if err != nil {
		return &websocket{}, err
	}

	return &websocket{conn: conn}, nil
}

func MakeUpgrader() IUpgrader {
	upgrader := _websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     func(r *http.Request) bool { return true },
	}

	return &Upgrader{upgrader: upgrader}
}
