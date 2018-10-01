// Package binrpc implements the BINRPC protocol of Kamailio for invoking RPC functions.
// This package lets you talk to a Kamailio instance from Go code.
//
// The ctl module must be loaded: https://www.kamailio.org/docs/modules/stable/modules/ctl.html
//
// The BINRPC protocol is described in "src/modules/ctl/binrpc.h": https://github.com/kamailio/kamailio/blob/master/src/modules/ctl/binrpc.h
//
// Limits
//
// The current implementation handles only int, string, and structs containing int or string values. Other types will return an error.
//
// Usage
//
// High level functions:
//
// - WritePacket* to call an RPC function (a string like "tm.stats")
//
// - ReadPacket to read the response
//
//   package main
//
//   import (
//   	"fmt"
//   	"net"
//
//   	binrpc "github.com/florentchauveau/go-kamailio-binrpc"
//   )
//
//   func main() {
//   	conn, err := net.Dial("tcp", "localhost:2049")
//
//   	if err != nil {
//   		panic(err)
//   	}
//
//   	cookie, err := binrpc.WritePacketString(conn, "tm.stats")
//
//   	if err != nil {
//   		panic(err)
//   	}
//
//   	records, err := binrpc.ReadPacket(conn, cookie)
//
//   	if err != nil {
//   		panic(err)
//   	}
//
//   	fmt.Printf("records = %v", records)
//   }
//
package binrpc

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/rand"
)

// BinRPCMagic is a magic value at the start of every BINRPC packet.
// BinRPCVersion is the version implemented (currently 1).
const (
	BinRPCMagic   uint8 = 0xA
	BinRPCVersion uint8 = 0x1

	TypeInt    uint8 = 0x0
	TypeString uint8 = 0x1
	TypeDouble uint8 = 0x2
	TypeStruct uint8 = 0x3
	TypeArray  uint8 = 0x4
	TypeAVP    uint8 = 0x5
	TypeBytes  uint8 = 0x6
)

// Errors returned by the package
var (
	ErrBinarySize     = errors.New("cannot get binary size of value")
	ErrCookieMismatch = errors.New("cookie mismatch")
	ErrLengthTooBig   = errors.New("packet too big")
	ErrTypeConvert    = errors.New("cannot convert go type to binrpc type")
	ErrTypeNotImpl    = errors.New("type not implemented")
	ErrWrongType      = errors.New("wrong type requested")

	errEndOfStruct = errors.New("END_OF_STRUCT")
)

// Header is a struct containing values needed for parsing the payload and replying. It is not a binary representation of the actual header.
type Header struct {
	PayloadLength int
	Cookie        []byte
}

// Record represents a BINRPC type+size, and Go value. It is not a binary representation of a record.
// Type is the BINRPC type.
type Record struct {
	size int

	Type  uint8
	Value interface{}
}

// GetString returns the string value, or an error if the type is not a string.
func (record Record) GetString() (string, error) {
	if record.Type != TypeString {
		return "", ErrWrongType
	}

	return record.Value.(string), nil
}

// GetInt returns the int value, or an error if the type is not a int.
func (record Record) GetInt() (int, error) {
	if record.Type != TypeInt {
		return 0, ErrWrongType
	}

	return record.Value.(int), nil
}

// GetMap returns the map for a struct value, or an error if not a struct.
func (record *Record) GetMap() (map[string]Record, error) {
	if record.Type != TypeStruct {
		return nil, ErrWrongType
	}

	return record.Value.(map[string]Record), nil
}

// Encode is a low level function that encodes a record and writes it to w.
func (record *Record) Encode(w io.Writer) error {
	value := record.Value

	var sizeOfValue int

	switch record.Type {
	case TypeInt:
		var v int
		var ok bool

		if v, ok = value.(int); !ok {
			return errors.New("type error: expected type int")
		}

		// shortcut!
		if v == 0 {
			return binary.Write(w, binary.BigEndian, byte(0))
		}

		var size uint8

		size, value = getMinBinarySizeOfInt(v)
		sizeOfValue = int(size)
	case TypeString:
		if _, ok := value.(string); !ok {
			return errors.New("type error: expected type string")
		}

		// append null termination byte
		value = append([]byte(value.(string)), 0x00)

		if sizeOfValue = binary.Size(value); sizeOfValue == -1 {
			return ErrBinarySize
		}
	default:
		return fmt.Errorf("type error: type %d not implemented", record.Type)
	}

	var buffer bytes.Buffer

	if sizeOfValue < 8 {
		// this can fit in 3 bits
		header := byte(sizeOfValue<<4) | record.Type
		binary.Write(&buffer, binary.BigEndian, header)
		binary.Write(&buffer, binary.BigEndian, value)
	} else {
		sizeOfSize, sizeOfValueCasted := getMinBinarySizeOfInt(sizeOfValue)

		header := 1<<7 | sizeOfSize<<4 | record.Type

		binary.Write(&buffer, binary.BigEndian, header)
		binary.Write(&buffer, binary.BigEndian, sizeOfValueCasted)
		binary.Write(&buffer, binary.BigEndian, value)
	}

	if _, err := buffer.WriteTo(w); err != nil {
		return err
	}

	return nil
}

// CreateRecord is a low level function that creates a Record from value v and fills the Type property automatically.
func CreateRecord(v interface{}) (*Record, error) {
	record := Record{
		Value: v,
	}

	switch v.(type) {
	case string:
		record.Type = TypeString
	case int:
		record.Type = TypeInt
	default:
		return nil, ErrTypeConvert
	}

	return &record, nil
}

// ReadHeader is a low level function that reads from r and returns a Header.
func ReadHeader(r io.Reader) (*Header, error) {
	buf := make([]byte, 2)

	if len, err := r.Read(buf); err != nil || len != 2 {
		return nil, fmt.Errorf("cannot read header: err=%v, len=%d/2", err, len)
	}

	if magic := buf[0] >> 4; magic != BinRPCMagic {
		return nil, fmt.Errorf("magic field did not match, expected %X, got %X", BinRPCMagic, magic)
	}

	if version := buf[0] & 0x0F; version != BinRPCVersion {
		return nil, fmt.Errorf("version did not match, expected %d, got %d", BinRPCVersion, version)
	}

	sizeOfLength := buf[1]&0x0C>>2 + 1
	sizeOfCookie := buf[1]&0x3 + 1

	buf = make([]byte, sizeOfLength)

	if len, err := r.Read(buf); err != nil || len != int(sizeOfLength) {
		return nil, fmt.Errorf("cannot read total length, err=%v, len=%d/%d", err, len, sizeOfLength)
	}

	header := Header{}

	for _, b := range buf {
		header.PayloadLength = header.PayloadLength<<8 + int(b)
	}

	header.Cookie = make([]byte, sizeOfCookie)

	if len, err := r.Read(header.Cookie); err != nil || len != int(sizeOfCookie) {
		return nil, fmt.Errorf("cannot read total length, err=%v, len=%d/%d", err, len, sizeOfCookie)
	}

	return &header, nil
}

// ReadRecord is a low level function that reads from r and returns a Record or an error if one occurred.
func ReadRecord(r io.Reader) (*Record, error) {
	record := Record{}

	buf := make([]byte, 1)

	if len, err := r.Read(buf); err != nil || len != 1 {
		return nil, fmt.Errorf("cannot read record header: err=%v, len=%d/1", err, len)
	}

	flag := buf[0] >> 7
	size := int(buf[0] >> 4 & 0x7)

	record.size = 1 + size
	record.Type = buf[0] & 0x0F

	if flag == 1 && size == 0 && record.Type == TypeStruct {
		// this marks the end of a struct
		return nil, errEndOfStruct
	}

	if flag == 1 {
		buf = make([]byte, size)

		if len, err := r.Read(buf); err != nil || len != size {
			return nil, fmt.Errorf("cannot read record size: err=%v, len=%d/%d", err, len, size)
		}

		size = 0
		for _, b := range buf {
			size = size<<8 + int(b)
		}

		record.size += size
	}

	if size == 0 {
		buf = nil
	} else {
		buf = make([]byte, size)

		if len, err := r.Read(buf); err != nil || len != size {
			return nil, fmt.Errorf("cannot read record value: err=%v, len=%d/%d", err, len, size)
		}
	}

	switch record.Type {
	case TypeAVP:
		fallthrough
	case TypeString:
		if size == 0 {
			record.Value = ""
			break
		}

		// skip the null byte
		record.Value = string(buf[0 : len(buf)-1])
	case TypeInt:
		record.Value = int(0)

		if size == 0 {
			break
		}

		for _, b := range buf {
			record.Value = record.Value.(int)<<8 + int(b)
		}
	case TypeStruct:
		avpMap := make(map[string]Record)

		for {
			avpName, err := ReadRecord(r)

			if err == errEndOfStruct {
				record.size++
				break
			} else if err != nil {
				return nil, err
			}

			if avpName.Type != TypeAVP {
				return nil, fmt.Errorf("struct contains something else than avp: %d", avpName.Type)
			}

			record.size += avpName.size

			avpValue, err := ReadRecord(r)

			if err != nil {
				return nil, err
			}

			avpMap[avpName.Value.(string)] = *avpValue

			record.size += avpValue.size
		}

		record.Value = avpMap
	default:
		return nil, ErrTypeNotImpl
	}

	return &record, nil
}

// ReadPacket reads from r and returns records, or an error if one occurred.
// If expectedCookie is not nil, it verifies the cookie.
func ReadPacket(r io.Reader, expectedCookie []byte) ([]Record, error) {
	bufreader := bufio.NewReader(r)
	header, err := ReadHeader(bufreader)

	if err != nil {
		return nil, err
	}

	if expectedCookie != nil && bytes.Compare(expectedCookie, header.Cookie) != 0 {
		return nil, ErrCookieMismatch
	}

	payloadBytes := make([]byte, header.PayloadLength)

	if _, err := io.ReadFull(bufreader, payloadBytes); err != nil {
		return nil, err
	}

	read := 0
	payload := bytes.NewReader(payloadBytes)
	records := []Record{}

	for read < header.PayloadLength {
		record, err := ReadRecord(payload)

		if err != nil {
			return nil, err
		}

		records = append(records, *record)
		read += record.size
	}

	return records, err
}

// WritePacket creates a BINRPC packet (header and payload) containing values v, and writes it to w.
// It returns the cookie generated, or an error if one occurred.
func WritePacket(w io.Writer, values []interface{}) ([]byte, error) {
	var header bytes.Buffer
	var payload bytes.Buffer

	for _, v := range values {
		record, err := CreateRecord(v)

		if err != nil {
			return nil, err
		}

		if err = record.Encode(&payload); err != nil {
			return nil, err
		}
	}

	cookie := uint32(rand.Int63())

	sizeOfLength, totalLength := getMinBinarySizeOfInt(payload.Len())
	sizeOfCookie := binary.Size(cookie)

	// the totalLength cannot be larger than 4 bytes
	// because we have 2 bits to write its "length-1"
	// so "4" is the largest length that we can write
	if sizeOfLength > 4 {
		return nil, ErrLengthTooBig
	}

	header.WriteByte(BinRPCMagic<<4 | BinRPCVersion)
	header.WriteByte((sizeOfLength-1)<<2 | byte(sizeOfCookie-1))

	binary.Write(&header, binary.BigEndian, totalLength)
	binary.Write(&header, binary.BigEndian, cookie)

	writer := bufio.NewWriter(w)

	if _, err := writer.Write(header.Bytes()); err != nil {
		return nil, fmt.Errorf("cannot write header: err=%v", err)
	}
	if _, err := writer.Write(payload.Bytes()); err != nil {
		return nil, fmt.Errorf("cannot write payload: err=%v", err)
	}
	if err := writer.Flush(); err != nil {
		return nil, err
	}

	cookieBytes := make([]byte, sizeOfCookie)
	binary.BigEndian.PutUint32(cookieBytes, cookie)

	return cookieBytes, nil
}

// WritePacketValue creates a BINRPC packet (header and payload) containing value v, and writes it to w.
// It returns the cookie generated, or an error if one occurred.
func WritePacketValue(w io.Writer, v interface{}) ([]byte, error) {
	return WritePacket(w, []interface{}{v})
}

// WritePacketString creates a BINRPC packet (header and payload) containing string s, and writes it to w.
// It returns the cookie generated, or an error if one occurred.
func WritePacketString(w io.Writer, s string) ([]byte, error) {
	return WritePacket(w, []interface{}{s})
}

// WritePacketStrings creates a BINRPC packet (header and payload) containing strings s, and writes it to w.
// It returns the cookie generated, or an error if one occurred.
func WritePacketStrings(w io.Writer, s []string) ([]byte, error) {
	args := make([]interface{}, len(s))

	for i, v := range s {
		args[i] = v
	}

	return WritePacket(w, args)
}

// getMinBinarySizeOfInt returns the minimum size in bytes to store a
// signed integer, and the casted value of minimum size.
// this is needed because binary.Write requires fixed-size values
// so an int does not work.
func getMinBinarySizeOfInt(n int) (uint8, interface{}) {
	if n >= -128 && n <= 127 {
		return 1, int8(n)
	} else if n >= -32768 && n <= 32767 {
		return 2, int16(n)
	} else if n >= -2147483648 && n <= 2147483647 {
		return 4, int32(n)
	}

	return 8, int64(n)
}
