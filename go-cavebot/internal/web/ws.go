package web

import (
	"encoding/json"
	"sync"

	"github.com/gorilla/websocket"
)

type ConnectionManager struct {
	mu    sync.Mutex
	conns []*websocket.Conn
}

func NewConnectionManager() *ConnectionManager {
	return &ConnectionManager{}
}

func (cm *ConnectionManager) Add(conn *websocket.Conn) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.conns = append(cm.conns, conn)
}

func (cm *ConnectionManager) Remove(conn *websocket.Conn) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	for i, c := range cm.conns {
		if c == conn {
			cm.conns = append(cm.conns[:i], cm.conns[i+1:]...)
			return
		}
	}
}

func (cm *ConnectionManager) Broadcast(data interface{}) {
	msg, err := json.Marshal(data)
	if err != nil {
		return
	}
	cm.mu.Lock()
	defer cm.mu.Unlock()
	var dead []*websocket.Conn
	for _, c := range cm.conns {
		if err := c.WriteMessage(websocket.TextMessage, msg); err != nil {
			dead = append(dead, c)
		}
	}
	for _, c := range dead {
		for i, cc := range cm.conns {
			if cc == c {
				cm.conns = append(cm.conns[:i], cm.conns[i+1:]...)
				break
			}
		}
	}
}
