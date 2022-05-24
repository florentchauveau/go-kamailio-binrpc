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
// - WritePacket to call an RPC function (a string like "tm.stats")
//
// - ReadPacket to read the response
//
//   package main
//
//   import (
//   	"fmt"
//   	"net"
//
//   	binrpc "github.com/florentchauveau/go-kamailio-binrpc/v3"
//   )
//
//   func main() {
//   	conn, err := net.Dial("tcp", "localhost:2049")
//
//   	if err != nil {
//   		panic(err)
//   	}
//
//   	cookie, err := binrpc.WritePacket(conn, "tm.stats")
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
	"errors"
	"fmt"
	"io"
	"math/rand"
	"strconv"
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

	// the totalLength cannot be larger than 4 bytes
	// because we have 2 bits to write its "length-1"
	// so "4" is the largest length that we can write
	MaxSizeOfLength = 4
)

// internal error used to detect the end of a struct
var errEndOfStruct = errors.New("END_OF_STRUCT")

// Header is a struct containing values needed for parsing the payload and replying. It is not a binary representation of the actual header.
type Header struct {
	PayloadLength int
	Cookie        uint32
}

// ValidTypes is an interface of types that can be used in a Record.
type ValidTypes interface {
	int | string | float64
}

// Record represents a BINRPC type+size, and Go value. It is not a binary representation of a record.
// Type is the BINRPC type.
type Record struct {
	size int

	Type  uint8
	Value any
}

// StructItem represents an item in a BINRPC struct. Because BINRPC structs may contain the same key multiple times,
// structs are handled with arrays of StructItem.
type StructItem struct {
	Key   string
	Value Record
}

// String returns the string value, or an error if the type is not a string.
func (record Record) String() (string, error) {
	if record.Type != TypeString {
		return "", fmt.Errorf("type error: expected type string (%d), got %d", TypeString, record.Type)
	}

	return record.Value.(string), nil
}

// Int returns the int value, or an error if the type is not a int.
func (record Record) Int() (int, error) {
	if record.Type != TypeInt {
		return 0, fmt.Errorf("type error: expected type int (%d), got %d", TypeInt, record.Type)
	}

	return record.Value.(int), nil
}

// Double returns the double value as a float64, or an error if the type is not a double
func (record Record) Double() (float64, error) {
	if record.Type != TypeDouble {
		return 0, fmt.Errorf("type error: expected type double (%d), got %d", TypeDouble, record.Type)
	}

	return record.Value.(float64), nil
}

// StructItems returns items for a struct value, or an error if not a struct.
func (record *Record) StructItems() ([]StructItem, error) {
	if record.Type != TypeStruct {
		return nil, fmt.Errorf("type error: expected type struct (%d), got %d", TypeStruct, record.Type)
	}

	return record.Value.([]StructItem), nil
}

// Scan copies the value in the Record into the values pointed at by dest. Valid dest type are *int, *string, and *[]StructItem
func (record *Record) Scan(dest any) error {
	switch dest.(type) {
	case *string:
		s := dest.(*string)

		switch record.Type {
		case TypeString:
			*s = record.Value.(string)
		case TypeInt:
			*s = strconv.Itoa(record.Value.(int))
		case TypeDouble:
			*s = fmt.Sprintf("%.3f", record.Value.(float64))
		default:
			return fmt.Errorf("type error: cannot convert type %d to string", record.Type)
		}
	case *int:
		i := dest.(*int)

		switch record.Type {
		case TypeString:
			if n, err := strconv.Atoi(record.Value.(string)); err == nil {
				*i = n
			} else {
				return err
			}
		case TypeInt:
			*i = record.Value.(int)
		default:
			return fmt.Errorf("type error: cannot convert type %d to int", record.Type)
		}
	case *float64:
		f := dest.(*float64)

		switch record.Type {
		case TypeString:
			if value, err := strconv.ParseFloat(record.Value.(string), 64); err == nil {
				*f = value
			} else {
				return err
			}
		case TypeInt:
			*f = float64(record.Value.(int))
		case TypeDouble:
			*f = record.Value.(float64)
		default:
			return fmt.Errorf("type error: cannot convert type %d to double", record.Type)
		}
	case *[]StructItem:
		if record.Type != TypeStruct {
			return fmt.Errorf("type error: cannot convert type %d to []StructItem", record.Type)
		}

		items := dest.(*[]StructItem)
		*items = record.Value.([]StructItem)
	default:
		return errors.New("invalid dest type")
	}

	return nil
}

// Encode is a low level function that encodes a record and writes it to w.
func (record *Record) Encode(w io.Writer) error {
	var value bytes.Buffer

	switch record.Type {
	case TypeInt:
		var v int
		var ok bool

		if v, ok = record.Value.(int); !ok {
			return errors.New("type error: expected type int")
		}

		// shortcut!
		if v == 0 {
			_, err := w.Write([]byte{0x00})
			return err
		}

		value.Write(intToBytesBE(v))
	case TypeString:
		if s, ok := record.Value.(string); !ok {
			return errors.New("type error: expected type string")
		} else {
			value.WriteString(s)
		}

		value.WriteByte(0x00)
	case TypeDouble:
		var v float64
		var ok bool

		if v, ok = record.Value.(float64); !ok {
			return errors.New("type error: expected type float64")
		}

		value.Write(intToBytesBE(int(v * 1000)))
	default:
		return fmt.Errorf("type error: type %d not implemented", record.Type)
	}

	sizeOfValue := value.Len()

	var buffer bytes.Buffer

	if sizeOfValue < 8 {
		// this can fit in 3 bits
		header := byte(sizeOfValue<<4) | record.Type
		buffer.WriteByte(header)
		buffer.Write(value.Bytes())
	} else {
		sizeBytes := intToBytesBE(sizeOfValue)

		header := 1<<7 | uint8(len(sizeBytes)<<4) | record.Type

		buffer.WriteByte(header)
		buffer.Write(sizeBytes)
		buffer.Write(value.Bytes())
	}

	if _, err := buffer.WriteTo(w); err != nil {
		return err
	}

	return nil
}

// CreateRecord is a low level function that creates a Record from value v and fills the Type property automatically.
func CreateRecord[T ValidTypes](v T) (*Record, error) {
	record := Record{
		Value: v,
	}

	switch any(v).(type) {
	case string:
		record.Type = TypeString
	case int:
		record.Type = TypeInt
	case float64:
		record.Type = TypeDouble
	default:
		return nil, errors.New("type not implemented")
	}

	return &record, nil
}

// ReadHeader is a low level function that reads from r and returns a Header.
func ReadHeader(r io.Reader) (*Header, error) {
	buf := make([]byte, 2)

	if len, err := r.Read(buf); err != nil {
		return nil, fmt.Errorf("cannot read header: %w", err)
	} else if len != 2 {
		return nil, fmt.Errorf("cannot read header: read=%d/%d", len, 2)
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

	if len, err := r.Read(buf); err != nil {
		return nil, fmt.Errorf("cannot read total length: %w", err)
	} else if len != int(sizeOfLength) {
		return nil, fmt.Errorf("cannot read total length, read=%d/%d", len, sizeOfLength)
	}

	header := Header{}

	for _, b := range buf {
		header.PayloadLength = header.PayloadLength<<8 + int(b)
	}

	cookieBytes := make([]byte, sizeOfCookie)

	if len, err := r.Read(cookieBytes); err != nil {
		return nil, fmt.Errorf("cannot read cookie: %w", err)
	} else if len != int(sizeOfCookie) {
		return nil, fmt.Errorf("cannot read cookie, read=%d/%d", len, sizeOfCookie)
	}

	for _, b := range cookieBytes {
		header.Cookie = header.Cookie<<8 | uint32(b)
	}

	return &header, nil
}

// ReadRecord is a low level function that reads from r and returns a Record or an error if one occurred.
func ReadRecord(r io.Reader) (*Record, error) {
	record := Record{}

	buf := make([]byte, 1)

	if len, err := r.Read(buf); err != nil {
		return nil, fmt.Errorf("cannot read record header: %w", err)
	} else if len != 1 {
		return nil, fmt.Errorf("cannot read record header: read=%d/1", len)
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

		if len, err := r.Read(buf); err != nil {
			return nil, fmt.Errorf("cannot read record size: %w", err)
		} else if len != size {
			return nil, fmt.Errorf("cannot read record size: read=%d/%d", len, size)
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

		if len, err := r.Read(buf); err != nil {
			return nil, fmt.Errorf("cannot read record value: %w", err)
		} else if len != size {
			return nil, fmt.Errorf("cannot read record value: read=%d/%d", len, size)
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
	case TypeDouble:
		record.Value = int(0)

		for _, b := range buf {
			record.Value = record.Value.(int)<<8 + int(b)
		}

		// double are implemented as int*1000
		record.Value = float64(record.Value.(int)) / 1000.0
	case TypeStruct:
		var items []StructItem

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

			items = append(items, StructItem{
				Key:   avpName.Value.(string),
				Value: *avpValue,
			})

			record.size += avpValue.size
		}

		record.Value = items
	default:
		return nil, fmt.Errorf("type error: type %d not implemented", record.Type)
	}

	return &record, nil
}

// ReadPacket reads from r and returns records, or an error if one occurred.
// If expectedCookie is not zero, it verifies the cookie.
func ReadPacket(r io.Reader, expectedCookie uint32) ([]Record, error) {
	bufreader := bufio.NewReader(r)
	header, err := ReadHeader(bufreader)

	if err != nil {
		return nil, err
	}

	if expectedCookie != 0 && expectedCookie != header.Cookie {
		return nil, errors.New("expected cookie did not match")
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
func WritePacket[T ValidTypes](w io.Writer, values ...T) (uint32, error) {
	if len(values) == 0 {
		return 0, errors.New("missing values")
	}

	var header bytes.Buffer
	var payload bytes.Buffer

	for _, v := range values {
		record, err := CreateRecord(v)

		if err != nil {
			return 0, err
		}

		if err = record.Encode(&payload); err != nil {
			return 0, err
		}
	}

	cookie := rand.Uint32()
	cookieBytes := intToBytesBE(int(cookie))
	lengthBE := intToBytesBE(payload.Len())

	if len(lengthBE) > MaxSizeOfLength {
		return 0, fmt.Errorf("packet length too big: %d/%d bytes", len(lengthBE), MaxSizeOfLength)
	}

	header.WriteByte(BinRPCMagic<<4 | BinRPCVersion)
	header.WriteByte(byte((len(lengthBE)-1)<<2 | len(cookieBytes) - 1))
	header.Write(lengthBE)
	header.Write(cookieBytes)

	writer := bufio.NewWriter(w)

	if _, err := writer.Write(header.Bytes()); err != nil {
		return 0, fmt.Errorf("cannot write header: err=%v", err)
	}
	if _, err := writer.Write(payload.Bytes()); err != nil {
		return 0, fmt.Errorf("cannot write payload: err=%v", err)
	}
	if err := writer.Flush(); err != nil {
		return 0, err
	}

	return cookie, nil
}

// getMinBinarySizeOfInt returns the minimum size in bytes required to store an integer.
func getMinBinarySizeOfInt(value int) uint8 {
	n := uint32(value)
	size := uint8(0)

	for size = 4; size > 0 && ((n & (0xff << 24)) == 0); size-- {
		n <<= 8
	}

	return size
}

func intToBytesBE(n int) []byte {
	size := getMinBinarySizeOfInt(n)
	bytes := make([]byte, size)

	for ; size > 0; size-- {
		// big endian
		bytes[size-1] = byte(n)
		n >>= 8
	}

	return bytes
}
