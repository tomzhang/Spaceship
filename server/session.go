package server

import (
	"bytes"
	"github.com/golang/protobuf/jsonpb"
	"github.com/gorilla/websocket"
	"github.com/pkg/errors"
	"github.com/satori/go.uuid"
	"go.uber.org/atomic"
	"log"
	"net"
	"spaceship/socketapi"
	"sync"
	"time"
)

type session struct {
	sync.Mutex
	id uuid.UUID
	userID     string
	username   string
	expiry     int64
	clientIP   string
	clientPort string

	pingPeriodTime time.Duration
	pongWaitTime time.Duration
	writeWaitTime time.Duration

	sessionHolder *SessionHolder
	config *Config
	conn *websocket.Conn

	jsonProtoMarshler *jsonpb.Marshaler
	jsonProtoUnmarshler *jsonpb.Unmarshaler

	receivedMsgDecrement int
	pingTimer *time.Timer
	pingTimerCas *atomic.Uint32

	gameHolder *GameHolder
	outgoingCh chan []byte

	closed bool
}

func NewSession(userID string, username string, expiry int64, clientIP string, clientPort string, conn *websocket.Conn, config *Config, sessionHolder *SessionHolder, gameHolder *GameHolder, jsonProtoMarshler *jsonpb.Marshaler, jsonProtoUnmarshler *jsonpb.Unmarshaler) Session {

	sessionID := uuid.Must(uuid.NewV4(), nil)

	return &session{
		id: sessionID,
		userID: userID,
		username: username,
		expiry: expiry,
		clientIP: clientIP,
		clientPort: clientPort,

		pingPeriodTime: time.Duration(config.SocketConfig.PingPeriodTime) * time.Millisecond,
		pongWaitTime: time.Duration(config.SocketConfig.PongWaitTime) * time.Millisecond,
		writeWaitTime: time.Duration(config.SocketConfig.WriteWaitTime) * time.Millisecond,

		config: config,
		conn: conn,
		sessionHolder: sessionHolder,

		jsonProtoMarshler: jsonProtoMarshler,
		jsonProtoUnmarshler: jsonProtoUnmarshler,

		receivedMsgDecrement: config.SocketConfig.ReceivedMessageDecrementCount,
		pingTimer: time.NewTimer(time.Duration(config.SocketConfig.PingPeriodTime) * time.Millisecond),
		pingTimerCas: atomic.NewUint32(1),

		gameHolder: gameHolder,
		outgoingCh: make(chan []byte, config.SocketConfig.OutgoingQueueSize),

		closed: false,
	}

}

func (s *session) ID() uuid.UUID {
	return s.id
}

func (s *session) UserID() string {
	return s.userID
}

func (s *session) ClientIP() string {
	return s.clientIP
}

func (s *session) ClientPort() string {
	return s.clientPort
}

func (s *session) Username() string {
	return s.username
}

func (s *session) SetUsername(username string) {
	s.username = username
}

func (s *session) Expiry() int64 {
	return s.expiry
}

//func (s *session) Consume(processRequest func(session Session, envelope *rtapi.Envelope) bool) {
func (s *session) Consume(handlerFunc func(session Session, envelope *socketapi.Envelope) bool) {
	defer s.Close()
	s.conn.SetReadLimit(4096)
	if err := s.conn.SetReadDeadline(time.Now().Add(s.pongWaitTime)); err != nil {
		log.Println("Error occured while trying to set read deadline", errors.WithStack(err))
		return
	}
	s.conn.SetPongHandler(func(string) error {
		log.Println("pong received")
		s.resetPingTimer()
		return nil
	})

	go s.processOutgoing()

	for {
		_, data, err := s.conn.ReadMessage()

		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseNoStatusReceived) {
				log.Println("Socket connection was closed id: " + s.ID().String())
			}else if e, ok := err.(*net.OpError); !ok || e.Err.Error() != "use of closed network connection" {
				log.Println("Socket connection was closed id: " + s.ID().String())
			}else{
				log.Println("Error occured while reading message on socket connection", errors.WithStack(err))
			}
			//Even if connection was closed or error occured we should break the loop
			break
		}

		s.receivedMsgDecrement--
		if s.receivedMsgDecrement < 1 {
			s.receivedMsgDecrement = s.config.SocketConfig.ReceivedMessageDecrementCount
			if !s.resetPingTimer(){
				// We couldn't reset ping timer so there should be an error we need to close the loop
				return
			}
		}

		request := &socketapi.Envelope{}

		//TODO: we can also handle proto messages
		err = s.jsonProtoUnmarshler.Unmarshal(bytes.NewReader(data), request)

		if err != nil {
			log.Println("Read message error", errors.WithStack(err))
			//break
		}

		if !handlerFunc(s, request) {
			break
		}

	}

}

func (s *session) resetPingTimer() bool {

	if !s.pingTimerCas.CAS(1, 0) {
		return true
	}
	defer s.pingTimerCas.CAS(0, 1)

	s.Lock()
	if s.closed {
		s.Unlock()
		return false
	}

	if !s.pingTimer.Stop() {
		select {
		case <-s.pingTimer.C:
		default:
		}
	}

	s.pingTimer.Reset(s.pingPeriodTime)
	err := s.conn.SetReadDeadline(time.Now().Add(s.pongWaitTime))
	s.Unlock()
	if err != nil {
		log.Println("Error while trying to set read deadline on socket connection", errors.WithStack(err))
		s.Close()
		return false
	}
	return true
}

func (s *session) processOutgoing() {
	defer s.Close()
	for {
		select {
		case <-s.pingTimer.C:
			if !s.pingNow() {
				return
			}
			break
		case payload := <-s.outgoingCh:
			s.Lock()

			if s.closed {
				s.Unlock()
				return
			}

			// Process the outgoing message queue.
			s.conn.SetWriteDeadline(time.Now().Add(10*time.Second))
			if err := s.conn.WriteMessage(websocket.TextMessage, payload); err != nil {
				s.Unlock()
				log.Println("Could not write message", errors.WithStack(err))
				return
			}
			s.Unlock()
		}
	}

}

func (s *session) pingNow() bool {
	s.Lock()
	if s.closed {
		s.Unlock()
		return false
	}
	if err := s.conn.SetWriteDeadline(time.Now().Add(10*time.Second)); err != nil {
		s.Unlock()
		log.Println("Could not set write deadline to ping", err)
		return false
	}
	err := s.conn.WriteMessage(websocket.PingMessage, []byte{})
	s.Unlock()
	if err != nil {
		log.Println("Could not send ping", err)
		return false
	}

	return true
}


func (s *session) Send(isStream bool, mode uint8, envelope *socketapi.Envelope) error {
	var payload []byte
	var err error
	var buf bytes.Buffer
	//TODO: sessions will support proto and json. it should be handled in here too
	if err = s.jsonProtoMarshler.Marshal(&buf, envelope); err == nil {
		payload = buf.Bytes()
	}
	if err != nil {
		log.Print("Could not marshal envelope", errors.WithStack(err))
		return err
	}

	return s.SendBytes(isStream, mode, []byte(payload))
}

func (s *session) SendBytes(isStream bool, mode uint8, payload []byte) error {
	s.Lock()
	if s.closed {
		s.Unlock()
		return nil
	}

	if isStream {
		s.outgoingCh <- payload
		s.Unlock()
		return nil
	}

	// By default attempt to queue messages and observe failures.
	select {
	case s.outgoingCh <- payload:
		s.Unlock()
		return nil
	default:
		// The outgoing queue is full, likely because the remote client can't keep up.
		// Terminate the connection immediately because the only alternative that doesn't block the server is
		// to start dropping messages, which might cause unexpected behaviour.
		s.Unlock()
		log.Println("Could not write message, session outgoing queue full")
		s.Close()
		return errors.New("outgoing queue full")
	}
}

func (s *session) Close() {

	s.Lock()
	if s.closed {
		s.Unlock()
		return
	}
	s.closed = true
	s.Unlock()

	s.sessionHolder.remove(s.id)
	s.pingTimer.Stop()
	close(s.outgoingCh)

	if err := s.conn.WriteControl(websocket.CloseMessage, []byte{}, time.Now().Add(s.writeWaitTime)); err != nil {
		log.Println("Couldn't send close message to client")
	}

	if err := s.conn.Close(); err != nil {
		log.Println("Couldn't close socket connection for session id: " + s.id.String())
	}

}