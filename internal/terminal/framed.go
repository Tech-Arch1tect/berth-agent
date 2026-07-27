package terminal

import (
	"encoding/json"
	"time"

	"github.com/gorilla/websocket"
	"github.com/tech-arch1tect/berth-agent/internal/agentsign"
)

type framedConn struct {
	conn    *websocket.Conn
	frames  *agentsign.FrameWriter
	unframe *agentsign.FrameReader
}

func (f *framedConn) ReadJSON(target any) error {
	_, framed, err := f.conn.ReadMessage()
	if err != nil {
		return err
	}
	_, payload, err := f.unframe.UnwrapTyped(framed)
	if err != nil {
		return err
	}
	return json.Unmarshal(payload, target)
}

func (f *framedConn) WriteJSON(value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return f.conn.WriteMessage(websocket.BinaryMessage, f.frames.WrapTyped(byte(websocket.TextMessage), payload))
}

func (f *framedConn) WriteMessage(messageType int, data []byte) error {
	return f.conn.WriteMessage(messageType, data)
}

func (f *framedConn) SetWriteDeadline(deadline time.Time) error {
	return f.conn.SetWriteDeadline(deadline)
}

func (f *framedConn) SetReadDeadline(deadline time.Time) error {
	return f.conn.SetReadDeadline(deadline)
}

func (f *framedConn) SetPongHandler(handler func(string) error) {
	f.conn.SetPongHandler(handler)
}

func (f *framedConn) Close() error {
	return f.conn.Close()
}
