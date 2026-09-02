package mcproto

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"testing"
)

func handshake(host string, next int) []byte {
	var p bytes.Buffer
	_ = WriteVarInt(&p, 0)
	_ = WriteVarInt(&p, 767)
	_ = writeString(&p, host)
	_ = binary.Write(&p, binary.BigEndian, uint16(25565))
	_ = WriteVarInt(&p, next)
	return frame(p.Bytes())
}
func TestHandshake(t *testing.T) {
	h, err := ReadHandshake(bufio.NewReader(bytes.NewReader(handshake("alpha.mc.example.com", 2))))
	if err != nil {
		t.Fatal(err)
	}
	if h.Host != "alpha.mc.example.com" || h.NextState != 2 || !bytes.Equal(h.Frame, handshake("alpha.mc.example.com", 2)) {
		t.Fatalf("bad handshake: %+v", h)
	}
}
func TestMalformedHandshake(t *testing.T) {
	if _, err := ReadHandshake(bufio.NewReader(bytes.NewReader([]byte{0}))); err == nil {
		t.Fatal("accepted empty frame")
	}
}

func TestPipelinedStatusRequest(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()
	done := make(chan error, 1)
	go func() {
		defer server.Close()
		r := bufio.NewReader(server)
		h, err := ReadHandshake(r)
		if err == nil {
			err = WriteStatus(server, r, h.Protocol, "Hostpack", "sleeping", 0, 0)
		}
		done <- err
	}()
	request := append(handshake("alpha.mc.example.com", 1), []byte{1, 0}...)
	if _, err := client.Write(request); err != nil {
		t.Fatal(err)
	}
	if n, err := ReadVarInt(client); err != nil || n <= 1 {
		t.Fatalf("status frame length=%d err=%v", n, err)
	} else {
		payload := make([]byte, n)
		if _, err = io.ReadFull(client, payload); err != nil {
			t.Fatal(err)
		}
	}
	_ = client.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
