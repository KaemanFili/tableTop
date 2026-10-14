package handlers

import (
	"github.com/gorilla/websocket"
	"sync"
)

// One writer owns each socket. Board producers never wait for network I/O.
// Drawing messages retain order; replaceable position snapshots keep only the latest.
type socketWriter struct {
	conn    *websocket.Conn
	pending chan []byte
	done    chan struct{}
	once    sync.Once
	latest  bool
}

func newSocketWriter(conn *websocket.Conn, initial [][]byte, latest bool) *socketWriter {
	capacity := 128
	if latest {
		capacity = 1
	}
	writer := &socketWriter{conn: conn, pending: make(chan []byte, capacity), done: make(chan struct{}), latest: latest}
	go writer.run(initial)
	return writer
}

func (writer *socketWriter) stop() {
	writer.once.Do(func() { close(writer.done); writer.conn.Close() })
}

// Callers serialize producers with the board or position mutex.
func (writer *socketWriter) send(data []byte) bool {
	select {
	case <-writer.done:
		return false
	default:
	}
	if writer.latest {
		select {
		case <-writer.pending:
		default:
		}
	}
	select {
	case writer.pending <- data:
		return true
	default:
		// Never drop an ordered drawing message. Disconnect a lagging client
		// so it can reconnect and load a complete board instead.
		writer.stop()
		return false
	}
}

func (writer *socketWriter) run(initial [][]byte) {
	defer writer.stop()
	for _, data := range initial {
		select {
		case <-writer.done:
			return
		default:
		}
		if writeSocket(writer.conn, data) != nil {
			return
		}
	}
	for {
		select {
		case <-writer.done:
			return
		case data := <-writer.pending:
			if writeSocket(writer.conn, data) != nil {
				return
			}
		}
	}
}
