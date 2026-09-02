package rcon

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"
)

type Client struct {
	Address, Password string
	Timeout           time.Duration
}

func (c Client) Command(ctx context.Context, command string) (string, error) {
	timeout := c.Timeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", c.Address)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	if err = writePacket(conn, 1, 3, c.Password); err != nil {
		return "", err
	}
	id, _, _, err := readPacket(conn)
	if err != nil {
		return "", err
	}
	if id == -1 {
		return "", fmt.Errorf("rcon authentication failed")
	}
	if err = writePacket(conn, 2, 2, command); err != nil {
		return "", err
	}
	id, _, body, err := readPacket(conn)
	if err != nil {
		return "", err
	}
	if id != 2 {
		return "", fmt.Errorf("unexpected rcon response id %d", id)
	}
	return body, nil
}
func writePacket(w io.Writer, id, kind int32, body string) error {
	var p bytes.Buffer
	_ = binary.Write(&p, binary.LittleEndian, id)
	_ = binary.Write(&p, binary.LittleEndian, kind)
	p.WriteString(body)
	p.Write([]byte{0, 0})
	if err := binary.Write(w, binary.LittleEndian, int32(p.Len())); err != nil {
		return err
	}
	_, err := w.Write(p.Bytes())
	return err
}
func readPacket(r io.Reader) (int32, int32, string, error) {
	var n int32
	if err := binary.Read(r, binary.LittleEndian, &n); err != nil {
		return 0, 0, "", err
	}
	if n < 10 || n > 4<<20 {
		return 0, 0, "", fmt.Errorf("invalid rcon packet length %d", n)
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return 0, 0, "", err
	}
	id := int32(binary.LittleEndian.Uint32(b[0:4]))
	kind := int32(binary.LittleEndian.Uint32(b[4:8]))
	return id, kind, string(bytes.TrimRight(b[8:], "\x00")), nil
}
