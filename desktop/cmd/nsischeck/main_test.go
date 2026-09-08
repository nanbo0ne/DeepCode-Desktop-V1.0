package main

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"testing"
)

func TestVerifyNSIS(t *testing.T) {
	file := make([]byte, 1200)
	header := file[1024:1052]
	binary.LittleEndian.PutUint32(header[0:], 8)
	binary.LittleEndian.PutUint32(header[4:], 0xdeadbeef)
	copy(header[8:], "NullsoftInst")
	binary.LittleEndian.PutUint32(header[24:], uint32(len(file)-1024))
	binary.LittleEndian.PutUint32(file[len(file)-4:], crc32.ChecksumIEEE(file[512:len(file)-4]))
	if _, err := verify(bytes.NewReader(file), int64(len(file))); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func([]byte) []byte
	}{
		{"corruption", func(b []byte) []byte { b[900] ^= 1; return b }},
		{"truncation", func(b []byte) []byte { return b[:len(b)-1] }},
		{"disabled", func(b []byte) []byte { b[1024] = 4; return b }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := tc.change(append([]byte(nil), file...))
			if _, err := verify(bytes.NewReader(b), int64(len(b))); err == nil {
				t.Fatal("invalid installer accepted")
			}
		})
	}
}
