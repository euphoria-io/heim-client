package client

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"euphoria.io/heim/proto"
	"euphoria.io/scope"

	"github.com/gorilla/websocket"
)

type State byte

const (
	Hello State = iota + 1
	Snapshot
	Joined
	Bounced
	Disconnected

	DefaultKeepAlive = time.Minute
)

type Client struct {
	ctx scope.Context
	c   *websocket.Conn
	f   *FeatureSet
	l   *log.Logger
	m   sync.Mutex
	s   State

	counter     uint64
	outstanding map[string]chan *proto.Packet
}

func (hc *Client) Add(handler interface{}) *Client {
	hc.f.Add(handler)
	return hc
}

func (hc *Client) run() {
	defer hc.Close()

	ch := make(chan *proto.Packet, 10)
	go hc.readMessages(ch)

	disconnected := false

	defer func() {
		if !disconnected {
			event := &proto.DisconnectEvent{}
			if err := hc.ctx.Err(); err != nil {
				event.Reason = err.Error()
			} else {
				event.Reason = "connection closed"
			}
			packet, err := proto.MakeEvent(event)
			if err != nil {
				hc.l.Printf("error making synthetic disconnect-event: %s", err)
				return
			}
			if err := hc.f.Handle(packet); err != nil && err != errNotHandled {
				hc.l.Printf("error handling synthetic disconnect-event: %s", err)
			}
		}
	}()

	for {
		select {
		case <-hc.ctx.Done():
			return

		case packet := <-ch:
			if packet.Type == proto.DisconnectEventType {
				disconnected = true
			}

			hc.m.Lock()
			replyCh, ok := hc.outstanding[packet.ID]
			delete(hc.outstanding, packet.ID)
			hc.m.Unlock()

			if ok {
				replyCh <- packet
				continue
			}

			if err := hc.f.Handle(packet); err != nil {
				if err == errNotHandled {
					hc.l.Printf("ignoring packet of type %s", packet.Type)
					hc.l.Printf("  %s", string(packet.Data))
				} else {
					hc.l.Printf("handle packet error: %s", err)
					hc.ctx.Terminate(err)
					return
				}
			}
		}
	}
}

func (hc *Client) readMessages(ch chan<- *proto.Packet) {
	for {
		select {
		case <-hc.ctx.Done():
			return
		default:
			packet := &proto.Packet{}
			if err := hc.c.ReadJSON(packet); err != nil {
				hc.l.Printf("read json error: %s", err)
				hc.ctx.Terminate(err)
				return
			}
			ch <- packet
		}
	}
}

func (hc *Client) PingEvent(event *proto.PingEvent) error {
	_, err := hc.Send(proto.PingReplyType, &proto.PingReply{UnixTime: event.UnixTime})
	return err
}

func (hc *Client) SnapshotEvent(event *proto.SnapshotEvent) error {
	hc.s = Joined
	return nil
}

func (hc *Client) nextID() uint64 { return atomic.AddUint64(&hc.counter, 1) }

func (hc *Client) Send(packetType proto.PacketType, payload interface{}) (interface{}, error) {
	ch, err := hc.AsyncSend(packetType, payload)
	if err != nil {
		return nil, err
	}

	packet := <-ch
	if packet.Error != "" {
		return nil, errors.New(packet.Error)
	}

	payload, err = packet.Payload()
	if err != nil {
		return nil, err
	}

	return payload, nil
}

func (hc *Client) AsyncSend(packetType proto.PacketType, payload interface{}) (<-chan *proto.Packet, error) {
	packet := &proto.Packet{}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	if err := packet.Data.UnmarshalJSON(payloadBytes); err != nil {
		return nil, err
	}
	packet.ID = fmt.Sprintf("%d", hc.nextID())
	packet.Type = packetType

	ch := make(chan *proto.Packet, 1)
	hc.m.Lock()
	hc.outstanding[packet.ID] = ch
	hc.m.Unlock()

	packetBytes, err := packet.Encode()
	if err != nil {
		return nil, err
	}

	hc.l.Printf("Sending packet: %s", string(packetBytes))

	if err := hc.c.WriteMessage(websocket.TextMessage, packetBytes); err != nil {
		return nil, err
	}

	return ch, nil
}

func (hc *Client) Close() error {
	hc.l.Printf("closing connection")
	return hc.c.Close()
}

func DialRoom(ctx scope.Context, baseUrl, room string, options ...interface{}) (*Client, error) {
	var cj http.CookieJar
	for _, opt := range options {
		switch v := opt.(type) {
		case http.CookieJar:
			cj = v
		default:
			return nil, fmt.Errorf("invalid option of type %T", opt)
		}
	}

	roomUrl := fmt.Sprintf("%s/room/%s", baseUrl, room)
	fail := func(err error) error {
		return fmt.Errorf("dial %s: %s", roomUrl, err)
	}

	u, err := url.Parse(roomUrl)
	if err != nil {
		return nil, fail(err)
	}

	logger := log.New(os.Stdout, fmt.Sprintf("[%s] ", u.Host), log.LstdFlags)
	logger.Printf("connecting to room at %s", u)

	dialer := websocket.Dialer{}

	if u.Scheme == "https" {
		u.Scheme = "wss"
		serverName := u.Host
		if idx := strings.IndexRune(serverName, ':'); idx >= 0 {
			serverName = serverName[:idx]
		}
		logger.Printf("tls dialing %s (SNI: %s)", u.Host, serverName)
		dialer.TLSClientConfig = &tls.Config{
			ServerName: serverName,
		}
	} else {
		logger.Printf("tcp dialing %s", u.Host)
		u.Scheme = "ws"
	}

	u.Path = u.Path + "/ws"

	hs := http.Header{}
	hs.Set("Origin", roomUrl)
	if cj != nil {
		for _, cookie := range cj.Cookies(u) {
			hs.Set("Cookie", cookie.String())
		}
	}

	logger.Printf("opening websocket: %s", u.String())
	ws, resp, err := dialer.Dial(u.String(), hs)
	if err != nil {
		logger.Printf("websocket handshake failed: %s", err)
		if resp != nil {
			bs, _ := ioutil.ReadAll(resp.Body)
			logger.Printf("response was:\n%s\n", string(bs))
		}
		return nil, fail(err)
	}

	if cj != nil {
		cj.SetCookies(u, resp.Cookies())
	}

	hc := &Client{
		ctx:         ctx.Fork(),
		c:           ws,
		f:           &FeatureSet{},
		l:           logger,
		s:           Snapshot, // TODO: change initial state to Hello
		outstanding: map[string]chan *proto.Packet{},
	}
	hc.Add(hc)
	go hc.run()
	return hc, nil
}
