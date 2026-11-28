package deviceprofile

import (
	"encoding/binary"
	"errors"
	"reflect"
	"testing"
)

func TestParseSMBIOSChassisTypesExtractsAndSortsUniqueValues(t *testing.T) {
	table := appendSMBIOSStructure(nil, 3, []byte{1, 0x80 | 31})
	table = appendSMBIOSStructure(table, 1, []byte{1, 2})
	table = appendSMBIOSStructure(table, 3, []byte{2, 30})
	table = appendSMBIOSStructure(table, 3, []byte{3, 31})
	table = appendSMBIOSStructure(table, smbiosEndOfTableType, nil)

	got, err := ParseSMBIOSChassisTypes(wrapRawSMBIOS(table))
	if err != nil {
		t.Fatalf("ParseSMBIOSChassisTypes() error = %v", err)
	}
	want := []uint16{30, 31}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("chassis types = %v, want %v", got, want)
	}
}

func TestParseSMBIOSChassisTypesPreservesUnknownChassisCodes(t *testing.T) {
	table := appendSMBIOSStructure(nil, 3, []byte{1, 0})
	table = appendSMBIOSStructure(table, smbiosEndOfTableType, nil)

	got, err := ParseSMBIOSChassisTypes(wrapRawSMBIOS(table))
	if err != nil {
		t.Fatalf("ParseSMBIOSChassisTypes() error = %v", err)
	}
	if !reflect.DeepEqual(got, []uint16{0}) {
		t.Fatalf("chassis types = %v, want [0]", got)
	}
}

func TestParseSMBIOSChassisTypesReturnsEmptyWhenNoEnclosureIsPresent(t *testing.T) {
	table := appendSMBIOSStructure(nil, 1, []byte{1, 2, 3})
	table = appendSMBIOSStructure(table, smbiosEndOfTableType, nil)

	got, err := ParseSMBIOSChassisTypes(wrapRawSMBIOS(table))
	if err != nil {
		t.Fatalf("ParseSMBIOSChassisTypes() error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("chassis types = %v, want empty", got)
	}
}

func TestParseSMBIOSChassisTypesIgnoresBytesOutsideDeclaredTable(t *testing.T) {
	table := appendSMBIOSStructure(nil, 3, []byte{1, 9})
	table = appendSMBIOSStructure(table, smbiosEndOfTableType, nil)
	raw := wrapRawSMBIOS(table)
	raw = append(raw, 0xde, 0xad, 0xbe, 0xef)

	got, err := ParseSMBIOSChassisTypes(raw)
	if err != nil {
		t.Fatalf("ParseSMBIOSChassisTypes() error = %v", err)
	}
	if !reflect.DeepEqual(got, []uint16{9}) {
		t.Fatalf("chassis types = %v, want [9]", got)
	}
}

func TestParseSMBIOSChassisTypesRejectsTruncatedRawHeader(t *testing.T) {
	_, err := ParseSMBIOSChassisTypes([]byte{1, 2, 3})
	if !errors.Is(err, ErrInvalidRawSMBIOS) {
		t.Fatalf("error = %v, want ErrInvalidRawSMBIOS", err)
	}
}

func TestParseSMBIOSChassisTypesRejectsInvalidTableLengths(t *testing.T) {
	tests := []struct {
		name   string
		length uint32
	}{
		{name: "zero", length: 0},
		{name: "larger than available", length: 100},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw := make([]byte, rawSMBIOSHeaderSize)
			binary.LittleEndian.PutUint32(raw[4:8], test.length)
			_, err := ParseSMBIOSChassisTypes(raw)
			if !errors.Is(err, ErrInvalidRawSMBIOS) {
				t.Fatalf("error = %v, want ErrInvalidRawSMBIOS", err)
			}
		})
	}
}

func TestParseSMBIOSChassisTypesRejectsOversizedRawBuffer(t *testing.T) {
	raw := make([]byte, maxRawSMBIOSBytes+1)
	_, err := ParseSMBIOSChassisTypes(raw)
	if !errors.Is(err, ErrInvalidRawSMBIOS) {
		t.Fatalf("error = %v, want ErrInvalidRawSMBIOS", err)
	}
}

func TestParseSMBIOSChassisTypesRejectsTruncatedStructureHeader(t *testing.T) {
	raw := wrapRawSMBIOS([]byte{3, 4, 0})
	_, err := ParseSMBIOSChassisTypes(raw)
	if !errors.Is(err, ErrInvalidRawSMBIOS) {
		t.Fatalf("error = %v, want ErrInvalidRawSMBIOS", err)
	}
}

func TestParseSMBIOSChassisTypesRejectsInvalidFormattedLengths(t *testing.T) {
	tests := []struct {
		name  string
		table []byte
	}{
		{name: "short header length", table: []byte{1, 3, 0, 0, 0, 0}},
		{name: "structure extends past table", table: []byte{1, 8, 0, 0, 0, 0}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseSMBIOSChassisTypes(wrapRawSMBIOS(test.table))
			if !errors.Is(err, ErrInvalidRawSMBIOS) {
				t.Fatalf("error = %v, want ErrInvalidRawSMBIOS", err)
			}
		})
	}
}

func TestParseSMBIOSChassisTypesRejectsShortSystemEnclosure(t *testing.T) {
	table := appendSMBIOSStructure(nil, 3, []byte{1})
	_, err := ParseSMBIOSChassisTypes(wrapRawSMBIOS(table))
	if !errors.Is(err, ErrInvalidRawSMBIOS) {
		t.Fatalf("error = %v, want ErrInvalidRawSMBIOS", err)
	}
}

func TestParseSMBIOSChassisTypesRejectsMissingStringTerminator(t *testing.T) {
	table := []byte{3, 6, 0, 0, 1, 9}
	_, err := ParseSMBIOSChassisTypes(wrapRawSMBIOS(table))
	if !errors.Is(err, ErrInvalidRawSMBIOS) {
		t.Fatalf("error = %v, want ErrInvalidRawSMBIOS", err)
	}
}

func TestParseSMBIOSChassisTypesRejectsNonzeroDataAfterEndMarker(t *testing.T) {
	table := appendSMBIOSStructure(nil, smbiosEndOfTableType, nil)
	table = append(table, 1)
	_, err := ParseSMBIOSChassisTypes(wrapRawSMBIOS(table))
	if !errors.Is(err, ErrInvalidRawSMBIOS) {
		t.Fatalf("error = %v, want ErrInvalidRawSMBIOS", err)
	}
}

func wrapRawSMBIOS(table []byte) []byte {
	raw := make([]byte, rawSMBIOSHeaderSize, rawSMBIOSHeaderSize+len(table))
	raw[1] = 3
	raw[2] = 7
	binary.LittleEndian.PutUint32(raw[4:8], uint32(len(table)))
	return append(raw, table...)
}

func appendSMBIOSStructure(table []byte, structureType byte, formatted []byte) []byte {
	structureLength := smbiosStructureHeaderSize + len(formatted)
	table = append(table, structureType, byte(structureLength), 0, 0)
	table = append(table, formatted...)
	return append(table, 0, 0)
}


func TestParseSMBIOSChassisTypesRejectsMissingEndMarker(t *testing.T) {
	table := appendSMBIOSStructure(nil, 3, []byte{1, 9})
	_, err := ParseSMBIOSChassisTypes(wrapRawSMBIOS(table))
	if !errors.Is(err, ErrInvalidRawSMBIOS) {
		t.Fatalf("error = %v, want ErrInvalidRawSMBIOS", err)
	}
}
