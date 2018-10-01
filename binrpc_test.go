package binrpc

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"net"
	"testing"
)

func TestHeaderParse(t *testing.T) {
	data := []byte{0xa1, 0x03, 0x0b, 0x6f, 0x8d, 0xa2, 0x97}
	reader := bytes.NewReader(data)

	header, err := ReadHeader(reader)

	if err != nil {
		t.Error(err)
	}

	if bytes.Compare(header.Cookie, data[3:]) != 0 {
		t.Error("cookie mismatch")
	}
	if header.PayloadLength != 0x0b {
		t.Errorf("wrong payload length, expected %d, got %d", 0x0b, header.PayloadLength)
	}
}

func TestRecordParseString(t *testing.T) {
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
		t.Errorf("value mismatch, expected \"tm.stats\", got \"%s\"", record.Value)
	}
}

func TestRecordParseInt(t *testing.T) {
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

func TestRecordParseStruct(t *testing.T) {
	raw := "03950863757272656e74001001950877616974696e6700100165746f74616c00308929f5950c746f74616c5f6c6f63616c00302396c7950d72706c5f7265636569766564004001276f74950e72706c5f67656e65726174656400304b8e01950972706c5f73656e74004001277f7e4536787800201cea45357878003022e3d24534787800300e98fa45337878000045327878003057b03895086372656174656400308929f565667265656400308929f4950d64656c617965645f66726565000083"
	data, _ := hex.DecodeString(raw)
	reader := bytes.NewReader(data)

	record, err := ReadRecord(reader)

	if err != nil {
		t.Error(err)
	}
	if _, ok := record.Value.(map[string]Record); !ok {
		t.Error("type mismatch, expected struct")
	}

	v := record.Value.(map[string]Record)

	if _, ok := v["total"]; !ok {
		t.Error("expected key \"total\" not found")
	}

	if record, ok := v["total"]; !ok || record.Value.(int) != 8989173 {
		t.Error("value of \"total\" != 8989173")
	}
}

func TestWritePacketString(t *testing.T) {
	expectedHeader, _ := hex.DecodeString("a1030b")
	expectedRecord, _ := hex.DecodeString("9109746d2e737461747300")

	var buffer bytes.Buffer

	cookie, err := WritePacketValue(&buffer, "tm.stats")

	if err != nil {
		t.Error(err)
	}

	if len(cookie) != 4 {
		t.Errorf("expected cookie len of 4, got %d", len(cookie))
	}
	if buffer.Len() != 18 {
		t.Errorf("output length mismatch, expected 18, got %d", buffer.Len())
	}

	var expected bytes.Buffer

	expected.Write(expectedHeader)
	expected.Write(cookie)
	expected.Write(expectedRecord)

	if bytes.Compare(expected.Bytes(), buffer.Bytes()) != 0 {
		t.Errorf("output differ, expected %x, got %x", expected.Bytes(), buffer.Bytes())
	}
}

func TestWritePacketInt(t *testing.T) {
	expectedHeader, _ := hex.DecodeString("a10303")
	expectedRecord, _ := hex.DecodeString("20008e")

	var buffer bytes.Buffer

	cookie, err := WritePacketValue(&buffer, 142)

	if err != nil {
		t.Error(err)
	}

	if len(cookie) != 4 {
		t.Errorf("expected cookie len of 4, got %d", len(cookie))
	}
	if buffer.Len() != 10 {
		t.Errorf("output length mismatch, expected 10, got %d", buffer.Len())
	}

	var expected bytes.Buffer

	expected.Write(expectedHeader)
	expected.Write(cookie)
	expected.Write(expectedRecord)

	if bytes.Compare(expected.Bytes(), buffer.Bytes()) != 0 {
		t.Errorf("output differ, expected %x, got %x", expected.Bytes(), buffer.Bytes())
	}
}

func TestMinBinarySizeOfInt(t *testing.T) {
	size, value := getMinBinarySizeOfInt(42)

	if size != 1 {
		t.Errorf("expected size of 1, got %d", size)
	}
	if _, ok := value.(int8); !ok {
		t.Error("expected type int8")
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
	if i, _ := record.GetInt(); i != 42 {
		t.Errorf("expected value %d, got %d", 42, i)
	}
}

func TestReadPacketError(t *testing.T) {
	raw := "a1332a512aaee42001f49125636f6d6d616e6420636f72652e6563686f20626f6e6a6f757273206e6f7420666f756e6400"
	data, _ := hex.DecodeString(raw)
	cookie := []byte{0x51, 0x2a, 0xae, 0xe4}

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

func ExampleWritePacketStrings() {
	// establish connection to Kamailio server
	conn, err := net.Dial("tcp", "localhost:2049")

	if err != nil {
		panic(err)
	}

	cookie, err := WritePacketStrings(conn, []string{"core.echo", "bonjours"})

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
	// a map[string]Record (TypeStruct)

	response, _ := records[0].GetString()

	fmt.Printf("response = %s", response)
}

func ExampleWritePacketString() {
	// establish connection to Kamailio server
	conn, err := net.Dial("tcp", "localhost:2049")

	if err != nil {
		panic(err)
	}

	// WritePacketString returns the cookie generated
	cookie, err := WritePacketString(conn, "tm.stats")

	if err != nil {
		panic(err)
	}

	// the cookie is passed again for verification
	// we receive records in response
	records, err := ReadPacket(conn, cookie)

	if err != nil {
		panic(err)
	}

	// "tm.stats" returns one record that is a map
	// with at least "total" and "current" keys
	avpMap, _ := records[0].GetMap()

	total, _ := avpMap["total"].GetInt()
	current, _ := avpMap["current"].GetInt()

	fmt.Printf("total = %d\ncurrent = %d\n",
		total,
		current,
	)
}
