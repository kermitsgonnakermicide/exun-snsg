// Package gameimg implements the cast-table layout used by NX arcade boards.
//
// Every board image starts with an "NXARCADE" header and carries the machine
// ID plus a CRC-protected table of playable characters. The CLI treats this as
// one of two possible image dialects; the other is plain C++ source recovered
// from a developer EEPROM.
package gameimg

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"strings"
)

const (
	Magic        = "NXARCADE"
	formatVer    = 1
	headerBytes  = 44
	entryTag     = 'C'
	maxNameBytes = 48
	maxClassLen  = 32
)

var ErrNotGameImage = errors.New("not an NX arcade image")

type Character struct {
	Name   string
	Class  string
	Glyph  byte
	X      uint16
	Y      uint16
	HP     uint16
	Flags  uint16
	Offset int
	Length int
}

type Image struct {
	Machine string
	Build   string
	Version uint32
	Cast    []Character
}

func LooksLike(blob []byte) bool {
	return len(blob) >= len(Magic) && string(blob[:len(Magic)]) == Magic
}

func Decode(blob []byte) (Image, error) {
	if !LooksLike(blob) {
		return Image{}, ErrNotGameImage
	}
	if len(blob) < headerBytes {
		return Image{}, fmt.Errorf("arcade image truncated: header needs %d bytes, got %d", headerBytes, len(blob))
	}

	version := binary.LittleEndian.Uint32(blob[8:12])
	if version != formatVer {
		return Image{}, fmt.Errorf("arcade image version %d is not supported", version)
	}

	castLen := binary.LittleEndian.Uint32(blob[12:16])
	if uint64(castLen) > uint64(len(blob)-headerBytes) {
		return Image{}, fmt.Errorf("cast table claims %d bytes, image only holds %d", castLen, len(blob)-headerBytes)
	}

	machine, err := textField(blob[16:32], "machine id")
	if err != nil {
		return Image{}, err
	}
	if machine == "" {
		return Image{}, errors.New("arcade image has no machine id")
	}
	build, err := textField(blob[32:40], "build stamp")
	if err != nil {
		return Image{}, err
	}

	payload := blob[headerBytes : headerBytes+int(castLen)]
	storedCastCRC := binary.LittleEndian.Uint32(blob[40:44])
	if got := crc32.ChecksumIEEE(payload); got != storedCastCRC {
		return Image{}, fmt.Errorf("cast checksum mismatch: stored %08x, computed %08x", storedCastCRC, got)
	}

	img := Image{Machine: machine, Build: build, Version: version}
	seen := make(map[string]int)
	for pos := 0; pos < len(payload); {
		entryStart := pos
		if len(payload)-pos < 17 {
			return Image{}, fmt.Errorf("cast entry at 0x%04x is truncated", entryStart)
		}
		if payload[pos] != entryTag {
			return Image{}, fmt.Errorf("unknown cast tag %#02x at 0x%04x", payload[pos], entryStart)
		}
		pos++

		nameLen := int(payload[pos])
		pos++
		if nameLen < 1 || nameLen > maxNameBytes || pos+nameLen > len(payload) {
			return Image{}, fmt.Errorf("bad name length in cast entry at 0x%04x", entryStart)
		}
		name := payload[pos : pos+nameLen]
		pos += nameLen

		classLen := int(payload[pos])
		pos++
		if classLen > maxClassLen || pos+classLen > len(payload) {
			return Image{}, fmt.Errorf("bad class length in cast entry at 0x%04x", entryStart)
		}
		class := payload[pos : pos+classLen]
		pos += classLen

		if pos+13 > len(payload) {
			return Image{}, fmt.Errorf("cast entry at 0x%04x is truncated", entryStart)
		}
		glyph := payload[pos]
		pos++
		x := binary.LittleEndian.Uint16(payload[pos:])
		pos += 2
		y := binary.LittleEndian.Uint16(payload[pos:])
		pos += 2
		hp := binary.LittleEndian.Uint16(payload[pos:])
		pos += 2
		flags := binary.LittleEndian.Uint16(payload[pos:])
		pos += 2

		storedEntryCRC := binary.LittleEndian.Uint32(payload[pos:])
		pos += 4
		if got := crc32.ChecksumIEEE(payload[entryStart : pos-4]); got != storedEntryCRC {
			return Image{}, fmt.Errorf("cast entry %q checksum mismatch at 0x%04x", string(name), entryStart)
		}

		title, err := printable(name, "character name")
		if err != nil {
			return Image{}, err
		}
		if title == "" {
			return Image{}, fmt.Errorf("empty character name at 0x%04x", entryStart)
		}
		klass, err := printable(class, "character class")
		if err != nil {
			return Image{}, err
		}
		if glyph < 0x20 || glyph > 0x7e {
			return Image{}, fmt.Errorf("character %q has non-printable glyph %#02x", title, glyph)
		}
		if first, dup := seen[title]; dup {
			return Image{}, fmt.Errorf("character %q repeats (first at 0x%04x)", title, first)
		}
		seen[title] = entryStart

		img.Cast = append(img.Cast, Character{
			Name:   title,
			Class:  klass,
			Glyph:  glyph,
			X:      x,
			Y:      y,
			HP:     hp,
			Flags:  flags,
			Offset: headerBytes + entryStart,
			Length: pos - entryStart,
		})
	}

	return img, nil
}

func Encode(img Image) ([]byte, error) {
	if img.Machine == "" {
		return nil, errors.New("arcade image needs a machine id")
	}
	if err := checkText(img.Machine, 16, "machine id"); err != nil {
		return nil, err
	}
	if err := checkText(img.Build, 8, "build stamp"); err != nil {
		return nil, err
	}
	if img.Version == 0 {
		img.Version = formatVer
	}
	if img.Version != formatVer {
		return nil, fmt.Errorf("cannot write arcade image version %d", img.Version)
	}

	seen := make(map[string]bool, len(img.Cast))
	var cast bytes.Buffer
	for _, ch := range img.Cast {
		if ch.Name == "" || len(ch.Name) > maxNameBytes {
			return nil, fmt.Errorf("character name %q does not fit (1..%d bytes)", ch.Name, maxNameBytes)
		}
		if err := checkText(ch.Name, maxNameBytes, "character name"); err != nil {
			return nil, err
		}
		if len(ch.Class) > maxClassLen {
			return nil, fmt.Errorf("character %q class is longer than %d bytes", ch.Name, maxClassLen)
		}
		if err := checkText(ch.Class, maxClassLen, "character class"); err != nil {
			return nil, err
		}
		if ch.Glyph < 0x20 || ch.Glyph > 0x7e {
			return nil, fmt.Errorf("character %q glyph %#02x is not printable", ch.Name, ch.Glyph)
		}
		if seen[ch.Name] {
			return nil, fmt.Errorf("character %q appears twice", ch.Name)
		}
		seen[ch.Name] = true

		entryStart := cast.Len()
		cast.WriteByte(entryTag)
		cast.WriteByte(byte(len(ch.Name)))
		cast.WriteString(ch.Name)
		cast.WriteByte(byte(len(ch.Class)))
		cast.WriteString(ch.Class)
		cast.WriteByte(ch.Glyph)
		writeU16(&cast, ch.X)
		writeU16(&cast, ch.Y)
		writeU16(&cast, ch.HP)
		writeU16(&cast, ch.Flags)
		sum := crc32.ChecksumIEEE(cast.Bytes()[entryStart:])
		_ = binary.Write(&cast, binary.LittleEndian, sum)
	}

	var out bytes.Buffer
	out.WriteString(Magic)
	writeU32(&out, img.Version)
	writeU32(&out, uint32(cast.Len()))
	out.Write(padded(img.Machine, 16))
	out.Write(padded(img.Build, 8))
	writeU32(&out, crc32.ChecksumIEEE(cast.Bytes()))
	out.Write(cast.Bytes())
	return out.Bytes(), nil
}

func writeU16(dst *bytes.Buffer, v uint16) {
	_ = binary.Write(dst, binary.LittleEndian, v)
}

func writeU32(dst *bytes.Buffer, v uint32) {
	_ = binary.Write(dst, binary.LittleEndian, v)
}

func padded(s string, size int) []byte {
	buf := make([]byte, size)
	copy(buf, s)
	return buf
}

func textField(field []byte, label string) (string, error) {
	end := len(field)
	for end > 0 && (field[end-1] == 0 || field[end-1] == ' ') {
		end--
	}
	for i := 0; i < end; i++ {
		if field[i] == 0 {
			return "", fmt.Errorf("%s contains an embedded NUL", label)
		}
	}
	value, err := printable(field[:end], label)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(value), nil
}

func printable(field []byte, label string) (string, error) {
	for _, b := range field {
		if b < 0x20 || b > 0x7e {
			return "", fmt.Errorf("%s contains non-printable byte %#02x", label, b)
		}
	}
	return string(field), nil
}

func checkText(value string, max int, label string) error {
	if len(value) > max {
		return fmt.Errorf("%s is longer than %d bytes", label, max)
	}
	for i := 0; i < len(value); i++ {
		if value[i] < 0x20 || value[i] > 0x7e {
			return fmt.Errorf("%s contains non-printable byte %#02x", label, value[i])
		}
	}
	return nil
}
