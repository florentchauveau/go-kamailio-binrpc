package binrpc

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"math"
	"net"
	"testing"
)

func TestReadHeader(t *testing.T) {
	data := []byte{0xa1, 0x03, 0x0b, 0x6f, 0x8d, 0xa2, 0x97}
	reader := bytes.NewReader(data)

	header, err := ReadHeader(reader)

	if err != nil {
		t.Error(err)
	}

	if header.Cookie != 0x6f8da297 {
		t.Error("cookie mismatch")
	}

	if header.PayloadLength != 0x0b {
		t.Errorf("wrong payload length, expected %d, got %d", 0x0b, header.PayloadLength)
	}
}

func TestReadHeaderInvalid(t *testing.T) {
	data := []byte{0xa1}
	reader := bytes.NewReader(data)

	header, err := ReadHeader(reader)

	if err == nil {
		t.Error("error must be returned")
	}

	if header != nil {
		t.Error("header must be nil")
	}
}

func TestReadRecordString(t *testing.T) {
	data := []byte{0x91, 0x09, 0x74, 0x6d, 0x2e, 0x73, 0x74, 0x61, 0x74, 0x73, 0x00}
	reader := bytes.NewReader(data)

	record, err := ReadRecord(reader)

	if err != nil {
		t.Error(err)
	}

	if record.Type != TypeString {
		t.Errorf("type mismatch, expected %d, got %d", TypeString, record.Type)
	}
	if record.Value != "tm.stats" {
		t.Errorf(`value mismatch, expected "tm.stats", got "%s"`, record.Value)
	}
}

func TestReadRecordInt(t *testing.T) {
	data := []byte{0x10, 0x2A}
	reader := bytes.NewReader(data)

	record, err := ReadRecord(reader)

	if err != nil {
		t.Error(err)
	}

	if record.Type != TypeInt {
		t.Errorf("type mismatch, expected %d, got %d", TypeInt, record.Type)
	}
	if record.Value != 42 {
		t.Errorf("value mismatch, expected %d, got %d", 42, record.Value)
	}
}

func TestReadRecordStruct(t *testing.T) {
	raw := "03950863757272656e74001001950877616974696e6700100165746f74616c00308929f5950c746f74616c5f6c6f63616c00302396c7950d72706c5f7265636569766564004001276f74950e72706c5f67656e65726174656400304b8e01950972706c5f73656e74004001277f7e4536787800201cea45357878003022e3d24534787800300e98fa45337878000045327878003057b03895086372656174656400308929f565667265656400308929f4950d64656c617965645f66726565000083"
	data, _ := hex.DecodeString(raw)
	reader := bytes.NewReader(data)

	record, err := ReadRecord(reader)

	if err != nil {
		t.Error(err)
	}
	if _, ok := record.Value.([]StructItem); !ok {
		t.Error("type mismatch, expected struct")
	}

	items := record.Value.([]StructItem)

	found := false
	for _, item := range items {
		if item.Key == "total" {
			found = true
			if item.Value.Value.(int) != 8989173 {
				t.Error(`value of "total" != 8989173`)
			}
			break
		}
	}

	if !found {
		t.Error(`expected key "total" not found`)
	}
}

func TestWritePacket(t *testing.T) {
	expectedHeader, _ := hex.DecodeString("a1030b")
	expectedRecord, _ := hex.DecodeString("9109746d2e737461747300")

	var buffer bytes.Buffer

	cookie, err := WritePacket(&buffer, "tm.stats")

	if err != nil {
		t.Error(err)
	}

	cookieLength := int(getMinBinarySizeOfInt(int(cookie)))
	expectedLength := len(expectedHeader) + len(expectedRecord) + cookieLength
	if buffer.Len() != expectedLength {
		t.Errorf("output length mismatch, expected %d, got %d", expectedLength, buffer.Len())
	}

	var expected bytes.Buffer

	// updated the cookie length in the expected header
	expectedHeader[1] = byte(cookieLength - 1)

	expected.Write(expectedHeader)
	for i := 0; i < cookieLength; i++ {
		// big endian
		expected.WriteByte(byte(cookie >> ((cookieLength - i - 1) * 8)))
	}
	expected.Write(expectedRecord)

	if !bytes.Equal(expected.Bytes(), buffer.Bytes()) {
		t.Errorf("output differ, expected %x, got %x", expected.Bytes(), buffer.Bytes())
	}
}

func TestWritePacketInt(t *testing.T) {
	expectedHeader, _ := hex.DecodeString("a10302")
	expectedRecord, _ := hex.DecodeString("108e")

	var buffer bytes.Buffer

	cookie, err := WritePacket(&buffer, 142)

	if err != nil {
		t.Error(err)
	}

	cookieLength := int(getMinBinarySizeOfInt(int(cookie)))
	expectedLength := len(expectedHeader) + len(expectedRecord) + cookieLength
	if buffer.Len() != expectedLength {
		t.Errorf("output length mismatch, expected %d, got %d", expectedLength, buffer.Len())
	}

	var expected bytes.Buffer

	// update the cookie length in the expected header
	expectedHeader[1] = byte(cookieLength - 1)

	expected.Write(expectedHeader)
	for i := 0; i < cookieLength; i++ {
		// big endian
		expected.WriteByte(byte(cookie >> ((cookieLength - i - 1) * 8)))
	}
	expected.Write(expectedRecord)

	if !bytes.Equal(expected.Bytes(), buffer.Bytes()) {
		t.Errorf("output differ, expected %x, got %x", expected.Bytes(), buffer.Bytes())
	}
}

func TestMinBinarySizeOfInt(t *testing.T) {
	size := getMinBinarySizeOfInt(8388605)

	if size != 3 {
		t.Errorf("expected size of 3, got %d", size)
	}
}

func TestCreateRecord(t *testing.T) {
	record, err := CreateRecord(42)

	if err != nil {
		t.Error(err)
	}
	if record.Type != TypeInt {
		t.Errorf("expected type %d, got %d", TypeInt, record.Type)
	}
	if i, _ := record.Int(); i != 42 {
		t.Errorf("expected value %d, got %d", 42, i)
	}
}

func TestReadPacket(t *testing.T) {
	raw := "a1322a9883af2001f49125636f6d6d616e6420636f72652e6563686f20626f6e6a6f757273206e6f7420666f756e6400"
	data, _ := hex.DecodeString(raw)
	cookie := uint32(0x9883af)

	response, err := ReadPacket(bytes.NewReader(data), cookie)

	if err != nil {
		t.Error(err)
	}

	if len(response) != 2 {
		t.Errorf("expected 2 records, found %d", len(response))
	}

	if response[0].Type != TypeInt {
		t.Errorf("expected first record to be type int, found %d", response[0].Type)
	}

	if response[0].Value.(int) != 500 {
		t.Errorf("expected response of 500, got %d", response[0].Value.(int))
	}
}

func TestReadRecordDouble(t *testing.T) {
	raw := "a103034d309725220634"
	data, _ := hex.DecodeString(raw)
	cookie := uint32(0x4d309725)
	expectedValue := 1.588

	response, err := ReadPacket(bytes.NewReader(data), cookie)

	if err != nil {
		t.Error(err)
	}

	if response[0].Type != TypeDouble {
		t.Errorf("expected first record to be type double, found %d", response[0].Type)
	}

	if value, err := response[0].Double(); err != nil {
		t.Error(err)
	} else if value != expectedValue {
		t.Errorf("expected response of %v, got %.3f", expectedValue, response[0].Value)
	}
}

func TestTypeDouble(t *testing.T) {
	expectedValue := 1.588
	expectedRecord, _ := hex.DecodeString("220634")
	record, err := CreateRecord(expectedValue)

	if err != nil {
		t.Error(err)
		return
	}

	var buffer bytes.Buffer

	if err = record.Encode(&buffer); err != nil {
		t.Error(err)
	}

	if !bytes.Equal(buffer.Bytes(), expectedRecord) {
		t.Errorf("expected bytes %x, got %x", expectedRecord, buffer.Bytes())
	}

	var value float64

	if err = record.Scan(&value); err != nil {
		t.Error(err)
	}

	if value != expectedValue {
		t.Errorf("expected value of %v, got %v", expectedValue, value)
	}
}

func ExampleWritePacket() {
	// establish connection to Kamailio server
	conn, err := net.Dial("tcp", "localhost:2049")

	if err != nil {
		panic(err)
	}

	cookie, err := WritePacket(conn, "core.echo", "bonjours")

	if err != nil {
		panic(err)
	}

	records, err := ReadPacket(conn, cookie)

	if err != nil {
		panic(err)
	}

	// based on records[0].Type, records[0].Value is either:
	// an int (TypeInt)
	// a string (TypeString)
	// a []StructItem (TypeStruct)

	response, _ := records[0].String()

	fmt.Printf("response = %s", response)
}

func ExampleWritePacket_scan() {
	// establish connection to Kamailio server
	conn, err := net.Dial("tcp", "localhost:2049")

	if err != nil {
		panic(err)
	}

	cookie, err := WritePacket(conn, "core.echo", "bonjours")

	if err != nil {
		panic(err)
	}

	records, err := ReadPacket(conn, cookie)

	if err != nil {
		panic(err)
	}

	// based on records[0].Type, records[0].Value is either:
	// an int (TypeInt)
	// a string (TypeString)
	// a []StructItem (TypeStruct)

	var response string

	if err = records[0].Scan(&response); err != nil {
		panic(err)
	}

	fmt.Printf("response = %s", response)
}

func ExampleWritePacket_structResponse() {
	// establish connection to Kamailio server
	conn, err := net.Dial("tcp", "localhost:2049")

	if err != nil {
		panic(err)
	}

	// WritePacket returns the cookie generated
	cookie, err := WritePacket(conn, "tm.stats")

	if err != nil {
		panic(err)
	}

	// the cookie is passed again for verification
	// we receive records in response
	records, err := ReadPacket(conn, cookie)

	if err != nil {
		panic(err)
	}

	// "tm.stats" returns one record that is a struct
	// and all items are int values
	items, _ := records[0].StructItems()

	for _, item := range items {
		value, _ := item.Value.Int()

		fmt.Printf("%s = %d\n",
			item.Key,
			value,
		)
	}
}

func ExampleWritePacket_structResponseScan() {
	// establish connection to Kamailio server
	conn, err := net.Dial("tcp", "localhost:2049")

	if err != nil {
		panic(err)
	}

	// WritePacket returns the cookie generated
	cookie, err := WritePacket(conn, "tm.stats")

	if err != nil {
		panic(err)
	}

	// the cookie is passed again for verification
	// we receive records in response
	records, err := ReadPacket(conn, cookie)

	if err != nil {
		panic(err)
	}

	// "tm.stats" returns one record that is a struct
	// and all items are int values
	var items []StructItem

	if err = records[0].Scan(&items); err != nil {
		panic(err)
	}

	for _, item := range items {
		value, _ := item.Value.Int()

		fmt.Printf("%s = %d\n",
			item.Key,
			value,
		)
	}
}

// TestEncodeIntRoundTrip verifies that ints of every binary size survive
// an encode/decode round trip. Values larger than 32 bits used to be
// silently truncated when encoding.
func TestEncodeIntRoundTrip(t *testing.T) {
	values := []int{
		0,
		1,
		142,
		255,
		256,
		65536,
		8388605,
		0xFFFFFFFF,
		0x100000000,   // 5 bytes, was truncated to 0
		67108864000,   // 64 MiB as a fixed-point double, was truncated
		math.MaxInt64, // 8 bytes, forces the long size form
		-1,
		-1500,
	}

	for _, value := range values {
		record, err := CreateRecord(value)

		if err != nil {
			t.Fatal(err)
		}

		var buf bytes.Buffer

		if err = record.Encode(&buf); err != nil {
			t.Fatalf("value %d: %s", value, err)
		}

		decoded, err := ReadRecord(&buf)

		if err != nil {
			t.Fatalf("value %d: %s", value, err)
		}

		i, err := decoded.Int()

		if err != nil {
			t.Fatalf("value %d: %s", value, err)
		}

		if i != value {
			t.Errorf("expected %d, got %d", value, i)
		}
	}
}

// TestEncodeDoubleRoundTrip verifies that doubles (fixed-point with
// 3 decimals) survive an encode/decode round trip, including values
// whose fixed-point representation exceeds 32 bits.
func TestEncodeDoubleRoundTrip(t *testing.T) {
	values := []float64{
		0,
		1.5,
		2590984.25,
		4294967.295, // largest fixed-point value that fits in 32 bits
		4294967.296, // smallest that does not
		67108864,    // 64 MiB, as returned by core.shmmem
		-1.5,
	}

	for _, value := range values {
		record, err := CreateRecord(value)

		if err != nil {
			t.Fatal(err)
		}

		var buf bytes.Buffer

		if err = record.Encode(&buf); err != nil {
			t.Fatalf("value %f: %s", value, err)
		}

		decoded, err := ReadRecord(&buf)

		if err != nil {
			t.Fatalf("value %f: %s", value, err)
		}

		d, err := decoded.Double()

		if err != nil {
			t.Fatalf("value %f: %s", value, err)
		}

		if d != value {
			t.Errorf("expected %f, got %f", value, d)
		}
	}
}

// TestWritePacketReadPacketRoundTrip verifies a full packet round trip
// with a value larger than 32 bits.
func TestWritePacketReadPacketRoundTrip(t *testing.T) {
	var buf bytes.Buffer

	cookie, err := WritePacket(&buf, 67108864000)

	if err != nil {
		t.Fatal(err)
	}

	records, err := ReadPacket(&buf, cookie)

	if err != nil {
		t.Fatal(err)
	}

	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	i, err := records[0].Int()

	if err != nil {
		t.Fatal(err)
	}

	if i != 67108864000 {
		t.Errorf("expected 67108864000, got %d", i)
	}
}
