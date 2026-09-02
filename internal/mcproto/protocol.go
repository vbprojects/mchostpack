package mcproto

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"time"
)

const maxPacket = 2 << 20

type Handshake struct {
	Protocol  int
	Host      string
	Port      uint16
	NextState int
	Frame     []byte
}

func ReadVarInt(r io.Reader) (int, error) {
	var value int
	var pos uint
	var b [1]byte
	for {
		if pos >= 35 {
			return 0, fmt.Errorf("varint too long")
		}
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return 0, err
		}
		value |= int(b[0]&0x7f) << pos
		if b[0]&0x80 == 0 {
			return value, nil
		}
		pos += 7
	}
}
func WriteVarInt(w io.Writer, v int) error {
	u := uint32(v)
	for {
		b := byte(u & 0x7f)
		u >>= 7
		if u != 0 {
			b |= 0x80
		}
		if _, err := w.Write([]byte{b}); err != nil {
			return err
		}
		if u == 0 {
			return nil
		}
	}
}
func writeString(w io.Writer, s string) error {
	if err := WriteVarInt(w, len(s)); err != nil {
		return err
	}
	_, err := io.WriteString(w, s)
	return err
}
func readString(r io.Reader) (string, error) {
	n, err := ReadVarInt(r)
	if err != nil {
		return "", err
	}
	if n < 0 || n > 32767 {
		return "", fmt.Errorf("invalid string length %d", n)
	}
	b := make([]byte, n)
	_, err = io.ReadFull(r, b)
	return string(b), err
}
func frame(payload []byte) []byte {
	var b bytes.Buffer
	_ = WriteVarInt(&b, len(payload))
	b.Write(payload)
	return b.Bytes()
}

func ReadHandshake(r *bufio.Reader) (Handshake, error) {
	n, err := ReadVarInt(r)
	if err != nil {
		return Handshake{}, err
	}
	if n <= 0 || n > maxPacket {
		return Handshake{}, fmt.Errorf("invalid packet length %d", n)
	}
	payload := make([]byte, n)
	if _, err = io.ReadFull(r, payload); err != nil {
		return Handshake{}, err
	}
	pr := bytes.NewReader(payload)
	id, err := ReadVarInt(pr)
	if err != nil || id != 0 {
		return Handshake{}, fmt.Errorf("expected handshake packet")
	}
	proto, err := ReadVarInt(pr)
	if err != nil {
		return Handshake{}, err
	}
	host, err := readString(pr)
	if err != nil {
		return Handshake{}, err
	}
	var port uint16
	if err = binary.Read(pr, binary.BigEndian, &port); err != nil {
		return Handshake{}, err
	}
	next, err := ReadVarInt(pr)
	if err != nil {
		return Handshake{}, err
	}
	if next != 1 && next != 2 {
		return Handshake{}, fmt.Errorf("unsupported next state %d", next)
	}
	return Handshake{Protocol: proto, Host: host, Port: port, NextState: next, Frame: frame(payload)}, nil
}

func WriteStatus(conn net.Conn, r *bufio.Reader, protocol int, versionName, motd string, online, max int) error {
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	n, err := ReadVarInt(r)
	if err != nil {
		return err
	}
	if n < 1 || n > maxPacket {
		return fmt.Errorf("invalid status request")
	}
	req := make([]byte, n)
	if _, err = io.ReadFull(r, req); err != nil {
		return err
	}
	id, err := ReadVarInt(bytes.NewReader(req))
	if err != nil || id != 0 {
		return fmt.Errorf("expected status request")
	}
	body, _ := json.Marshal(map[string]any{"version": map[string]any{"name": versionName, "protocol": protocol}, "players": map[string]any{"max": max, "online": online}, "description": map[string]string{"text": motd}})
	var payload bytes.Buffer
	_ = WriteVarInt(&payload, 0)
	_ = writeString(&payload, string(body))
	if _, err = conn.Write(frame(payload.Bytes())); err != nil {
		return err
	}
	// Echo a ping when present. A client may close immediately after the response.
	_ = conn.SetReadDeadline(time.Now().Add(750 * time.Millisecond))
	pingLen, e := ReadVarInt(r)
	if e != nil {
		return nil
	}
	if pingLen < 1 || pingLen > 64 {
		return nil
	}
	ping := make([]byte, pingLen)
	if _, e = io.ReadFull(r, ping); e == nil {
		_, e = conn.Write(frame(ping))
	}
	return e
}

func WriteLoginDisconnect(conn net.Conn, message string) error {
	body, _ := json.Marshal(map[string]string{"text": message})
	var payload bytes.Buffer
	_ = WriteVarInt(&payload, 0)
	_ = writeString(&payload, string(body))
	_, err := conn.Write(frame(payload.Bytes()))
	return err
}

func QueryStatus(ctxDeadline time.Time, address, host string) (int, error) {
	d := net.Dialer{}
	conn, err := d.Dial("tcp", address)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(ctxDeadline)
	var p bytes.Buffer
	_ = WriteVarInt(&p, 0)
	_ = WriteVarInt(&p, 767)
	_ = writeString(&p, host)
	_ = binary.Write(&p, binary.BigEndian, uint16(25565))
	_ = WriteVarInt(&p, 1)
	if _, err = conn.Write(frame(p.Bytes())); err != nil {
		return 0, err
	}
	if _, err = conn.Write([]byte{1, 0}); err != nil {
		return 0, err
	}
	r := bufio.NewReader(conn)
	n, err := ReadVarInt(r)
	if err != nil {
		return 0, err
	}
	if n <= 0 || n > maxPacket {
		return 0, fmt.Errorf("invalid response")
	}
	b := make([]byte, n)
	if _, err = io.ReadFull(r, b); err != nil {
		return 0, err
	}
	br := bytes.NewReader(b)
	id, err := ReadVarInt(br)
	if err != nil || id != 0 {
		return 0, fmt.Errorf("invalid response packet")
	}
	s, err := readString(br)
	if err != nil {
		return 0, err
	}
	var result struct {
		Players struct {
			Online int `json:"online"`
		} `json:"players"`
	}
	if err = json.Unmarshal([]byte(s), &result); err != nil {
		return 0, err
	}
	return result.Players.Online, nil
}
