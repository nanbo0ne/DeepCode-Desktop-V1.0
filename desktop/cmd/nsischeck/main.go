// nsischeck verifies the embedded NSIS CRC without executing the installer.
package main

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"os"
)

// NSIS firstheader and CRC extent follow Source/exehead/fileform.{h,c}.
// The first 512 bytes are excluded; trailing Authenticode data is outside CRC.
func verify(r io.ReaderAt, size int64) (uint32, error) {
	var header [28]byte
	for offset := int64(512); offset+28 <= size; offset += 512 {
		if _, err := r.ReadAt(header[:], offset); err != nil {
			return 0, err
		}
		u32 := func(at int) uint32 { return binary.LittleEndian.Uint32(header[at:]) }
		if u32(0)&^15 != 0 || u32(4) != 0xdeadbeef || string(header[8:20]) != "NullsoftInst" {
			continue
		}
		if u32(0)&4 != 0 && u32(0)&8 == 0 {
			return 0, fmt.Errorf("installer disables its CRC")
		}
		length := int64(u32(24))
		end := offset + length - 4
		if length < 32 || end+4 > size {
			return 0, fmt.Errorf("truncated NSIS payload")
		}
		var expected [4]byte
		if _, err := r.ReadAt(expected[:], end); err != nil {
			return 0, err
		}
		hash := crc32.NewIEEE()
		if _, err := io.Copy(hash, io.NewSectionReader(r, 512, end-512)); err != nil {
			return 0, err
		}
		got, want := hash.Sum32(), binary.LittleEndian.Uint32(expected[:])
		if got != want {
			return 0, fmt.Errorf("NSIS CRC mismatch: got %08x, expected %08x", got, want)
		}
		return got, nil
	}
	return 0, fmt.Errorf("NSIS firstheader not found")
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: nsischeck <installer.exe>")
		os.Exit(2)
	}
	f, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	crc, err := verify(f, info.Size())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("NSIS CRC verified: %08x (%d bytes)\n", crc, info.Size())
}
