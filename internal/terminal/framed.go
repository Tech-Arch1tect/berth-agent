package terminal

import (
	"context"
	"encoding/json"

	"github.com/coder/websocket"
	"github.com/tech-arch1tect/berth-agent/internal/agentsign"
)

type framedConn struct {
	conn    *websocket.Conn
	ctx     context.Context
	frames  *agentsign.FrameWriter
	unframe *agentsign.FrameReader
}

func (f *framedConn) ReadJSON(target any) error {
	_, framed, err := f.conn.Read(f.ctx)
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

	writeCtx, cancel := context.WithTimeout(f.ctx, terminalWriteWait)
	defer cancel()
	return f.conn.Write(writeCtx, websocket.MessageBinary, f.frames.WrapTyped(byte(websocket.MessageText), payload))
}

func (f *framedConn) Ping() error {
	pingCtx, cancel := context.WithTimeout(f.ctx, terminalPongWait)
	defer cancel()
	return f.conn.Ping(pingCtx)
}

func (f *framedConn) Close() error {
	return f.conn.Close(websocket.StatusNormalClosure, "")
}

func (f *framedConn) CloseWithError() {
	_ = f.conn.Close(websocket.StatusInternalError, "terminal ended")
}
