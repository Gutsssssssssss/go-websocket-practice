package main

import (
	"bytes"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

const (

	// Time allowed to write a message to the peer
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer
	maxMessageSize = 512
)

var (
	newline = []byte{'\n'}
	space   = []byte{' '}
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

type Client struct {
	hub *Hub

	// The websocket connection
	conn *websocket.Conn

	// Buffered channel of outbound messages
	send chan []byte
}

// 무한루프에서 프레임을 읽고, 브로드캐스트
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()
	c.conn.SetReadLimit(maxMessageSize)                                                                        // 한번 읽을 때 최대 크기 제한
	c.conn.SetReadDeadline(time.Now().Add(pongWait))                                                           // 다음 읽기 작업이 pongWait 안에 완료되어야 한다.
	c.conn.SetPongHandler(func(string) error { c.conn.SetReadDeadline(time.Now().Add(pongWait)); return nil }) // 클라이언트로부터 pong이 도착하면 실행할 콜백 함수
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("error: %v", err)
			}
			break
		}
		message = bytes.TrimSpace(bytes.ReplaceAll(message, newline, space))
		c.hub.broadcast <- message
	}
}

// broadcast 받은 message를 write
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait)) // 10초 안에 송신이 완료되지 않으면 안됨
			if !ok {
				// The hub closed the channel
				c.conn.WriteMessage(websocket.CloseMessage, []byte{}) // 한 번에 단일 프레임 전송
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage) // 큐에 쌓인 여러 메시지를 하나의 Writer로 묶어 보낼 때 사용
			if err != nil {
				return
			}
			w.Write(message)

			n := len(c.send) // 버퍼에 남은 메시지를 모아서 보내기 때문에 sys-call 줄일 수 있다.
			for range n {
				w.Write(newline)
				w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}
		case <-ticker.C: // TCP 연결 유지 확인, 54초마다 ping 보냄
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// serveWs handles websocket request from the peer
func serveWs(hub *Hub, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}
	client := &Client{hub: hub, conn: conn, send: make(chan []byte, 256)}
	client.hub.register <- client

	// conn.read, write 작업은 블로킹 작업, 그렇기 때문에 timeout을 통해 무한 대기를 방지
	go client.writePump()
	go client.readPump()
}
