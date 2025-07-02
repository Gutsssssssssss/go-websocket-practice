# 전체 흐름

## 0. 서버 초기화
* hub := newHub() → 클라이언트 등록/해제/브로드캐스트를 담당하는 구조 생성

* go hub.run() → 클라이언트 이벤트와 메시지를 처리하는 메인 고루틴 실행

## 1. 클라이언트 접속 요청: /ws
### 1‑1. WebSocket 업그레이드
* HTTP 요청을 websocket.Upgrader.Upgrade(w, r, nil) 를 통해 WebSocket 프로토콜로 전환

* TCP 연결을 유지하는 *websocket.Conn 객체 확보

### 1‑2. Client 생성 및 Hub에 등록
* client := &Client{hub, conn, make(chan []byte, 256)}

* hub.register <- client 로 등록 요청 전송

* → hub.run() 안에서 clients 맵에 추가됨

### 1‑3. 클라이언트별 고루틴 두 개 생성
* go client.readPump() (클라이언트 → 서버)

* go client.writePump() (서버 → 클라이언트)

## 2. 클라이언트 → 서버 메시지 흐름: readPump
### 2‑1. 무한 루프에서 메시지 읽기
* conn.ReadMessage() 로 WebSocket 수신 대기

* 수신된 메시지는 공백·개행 정리 후 처리

### 2‑2. Hub에 메시지 브로드캐스트 요청
* hub.broadcast <- message 전송

## 3. Hub에서 메시지 fan-out
### 3‑1. broadcast 채널 수신
* case msg := <-h.broadcast 로 수신 이벤트 감지

### 3‑2. 모든 클라이언트에게 메시지 전송
* for client := range h.clients

* client.send <- msg → 클라이언트 고루틴의 writePump()로 전달

* 버퍼가 꽉 찬 경우 default: 로 해당 클라이언트를 강제 해제하여 전체 병목 방지

## 4. 서버 → 클라이언트 메시지 전송: writePump
### 4‑1. send 채널 감시
* select 루프에서 message := <-c.send 감지

### 4‑2. WebSocket Writer로 메시지 전송
* conn.NextWriter(websocket.TextMessage)

* 메시지를 쓰고, 채널에 대기 중인 추가 메시지도 하나의 프레임으로 묶음

* writer.Close() 로 전송 완료

### 4‑3. Ping 메시지 주기적 전송 (keepalive)
* ticker.C 기반 Ping 전송