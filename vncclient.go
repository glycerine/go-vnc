/*
Package vnc implements a VNC client.

References:
  [PROTOCOL]: http://tools.ietf.org/html/rfc6143
*/
package vnc

import (
	"encoding/binary"
	"fmt"
	"net"

	"golang.org/x/net/context"
)

// Connect negotiates a connection to a VNC server.
func Connect(ctx context.Context, c net.Conn, cfg *ClientConfig) (*ClientConn, error) {
	conn := &ClientConn{
		c:      c,
		config: cfg,
	}

	if err := conn.protocolVersionHandshake(); err != nil {
		log.Println("protocolVersionHandshake()")
		conn.Close()
		return nil, err
	}
	if err := conn.securityHandshake(); err != nil {
		log.Println("securityHandshake()")
		conn.Close()
		return nil, err
	}
	if err := conn.securityResultHandshake(); err != nil {
		log.Println("securityResultHandshake()")
		conn.Close()
		return nil, err
	}
	if err := conn.clientInit(); err != nil {
		log.Println("clientInit()")
		conn.Close()
		return nil, err
	}
	if err := conn.serverInit(); err != nil {
		log.Println("serverInit()")
		conn.Close()
		return nil, err
	}

	return conn, nil
}

// A ClientConfig structure is used to configure a ClientConn. After
// one has been passed to initialize a connection, it must not be modified.
type ClientConfig struct {
	// A slice of ClientAuth methods. Only the first instance that is
	// suitable by the server will be used to authenticate.
	Auth []ClientAuth

	// Password for servers that require authentication.
	Password string

	// Exclusive determines whether the connection is shared with other
	// clients. If true, then all other clients connected will be
	// disconnected when a connection is established to the VNC server.
	Exclusive bool

	// The channel that all messages received from the server will be
	// sent on. If the channel blocks, then the goroutine reading data
	// from the VNC server may block indefinitely. It is up to the user
	// of the library to ensure that this channel is properly read.
	// If this is not set, then all messages will be discarded.
	ServerMessageCh chan ServerMessage

	// A slice of supported messages that can be read from the server.
	// This only needs to contain NEW server messages, and doesn't
	// need to explicitly contain the RFC-required messages.
	ServerMessages []ServerMessage
}

// NewClientConfig returns a populated ClientConfig.
func NewClientConfig(p string) *ClientConfig {
	return &ClientConfig{
		Auth: []ClientAuth{
			&ClientAuthNone{},
			&ClientAuthVNC{p},
		},
		Password: p,
		ServerMessages: []ServerMessage{
			NewFramebufferUpdateMessage(nil),
			&SetColorMapEntriesMessage{},
			&BellMessage{},
			&ServerCutTextMessage{},
		},
	}
}

// The ClientConn type holds client connection information.
type ClientConn struct {
	c               net.Conn
	config          *ClientConfig
	protocolVersion string

	// If the pixel format uses a color map, then this is the color
	// map that is used. This should not be modified directly, since
	// the data comes from the server.
	colorMap [256]Color

	// Name associated with the desktop, sent from the server.
	desktopName string

	// Encodings supported by the client. This should not be modified
	// directly. Instead, SetEncodings should be used.
	encodings []Encoding

	// Height of the frame buffer in pixels, sent from the server.
	fbHeight uint16

	// Width of the frame buffer in pixels, sent from the server.
	fbWidth uint16

	// The pixel format associated with the connection. This shouldn't
	// be modified. If you wish to set a new pixel format, use the
	// SetPixelFormat method.
	pixelFormat PixelFormat
}

// Close a connection to a VNC server.
func (c *ClientConn) Close() error {
	fmt.Println("VNC Client connection closed.")
	return c.c.Close()
}

// DesktopName returns the server provided desktop name.
func (c *ClientConn) DesktopName() string {
	return c.desktopName
}

// Encodings returns the server provided encodings.
func (c *ClientConn) Encodings() []Encoding {
	return c.encodings
}

// FramebufferHeight returns the server provided framebuffer height.
func (c *ClientConn) FramebufferHeight() uint16 {
	return c.fbHeight
}

// FramebufferWidth returns the server provided framebuffer width.
func (c *ClientConn) FramebufferWidth() uint16 {
	return c.fbWidth
}

// ListenAndHandle listens to a VNC server and handles server messages.
func (c *ClientConn) ListenAndHandle() error {
	defer c.Close()

	if c.config.ServerMessages == nil {
		return NewVNCError("Client config error: ServerMessages undefined")
	}
	serverMessages := make(map[uint8]ServerMessage)
	for _, m := range c.config.ServerMessages {
		serverMessages[m.Type()] = m
	}

	for {
		var messageType uint8
		if err := binary.Read(c.c, binary.BigEndian, &messageType); err != nil {
			fmt.Println("error: reading from server")
			break
		}

		msg, ok := serverMessages[messageType]
		if !ok {
			// Unsupported message type! Bad!
			fmt.Printf("error: unsupported message type")
			break
		}

		parsedMsg, err := msg.Read(c, c.c)
		if err != nil {
			fmt.Println("error: parsing message")
			break
		}

		if c.config.ServerMessageCh == nil {
			fmt.Println("ignoring message; no server message channel")
			continue
		}

		c.config.ServerMessageCh <- parsedMsg
	}

	return nil
}
