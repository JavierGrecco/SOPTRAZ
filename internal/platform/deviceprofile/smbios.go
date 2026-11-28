// Package deviceprofile collects platform-specific, read-only device evidence.
package deviceprofile

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
)

const (
	rawSMBIOSHeaderSize = 8
	smbiosStructureHeaderSize = 4
	smbiosSystemEnclosureType = 3
	smbiosEndOfTableType = 127
	maxRawSMBIOSBytes = 16 << 20
)

var (
	ErrInvalidRawSMBIOS = errors.New("tabla SMBIOS sin formato inválido")
)

// ParseSMBIOSChassisTypes extracts only the chassis-type byte from SMBIOS
// Type 3 (System Enclosure) structures. It deliberately ignores all strings,
// serial numbers, UUIDs, asset tags and other identifying data.
//
// The input is the complete RSMB buffer returned by GetSystemFirmwareTable,
// including the eight-byte RawSMBIOSData header.
func ParseSMBIOSChassisTypes(raw []byte) ([]uint16, error) {
	if len(raw) < rawSMBIOSHeaderSize {
		return nil, fmt.Errorf("%w: encabezado RawSMBIOSData truncado", ErrInvalidRawSMBIOS)
	}
	if len(raw) > maxRawSMBIOSBytes {
		return nil, fmt.Errorf("%w: el búfer supera el límite de %d bytes", ErrInvalidRawSMBIOS, maxRawSMBIOSBytes)
	}

	tableLength := uint64(binary.LittleEndian.Uint32(raw[4:8]))
	available := uint64(len(raw) - rawSMBIOSHeaderSize)
	if tableLength == 0 || tableLength > available {
		return nil, fmt.Errorf("%w: longitud declarada %d, bytes disponibles %d", ErrInvalidRawSMBIOS, tableLength, available)
	}

	table := raw[rawSMBIOSHeaderSize : rawSMBIOSHeaderSize+int(tableLength)]
	seen := make(map[uint16]struct{})
	chassisTypes := make([]uint16, 0, 2)

	for offset := 0; offset < len(table); {
		remaining := len(table) - offset
		if remaining < smbiosStructureHeaderSize {
			return nil, fmt.Errorf("%w: estructura truncada en offset %d", ErrInvalidRawSMBIOS, offset)
		}

		structureType := table[offset]
		formattedLength := int(table[offset+1])
		if formattedLength < smbiosStructureHeaderSize || formattedLength > remaining {
			return nil, fmt.Errorf("%w: longitud %d inválida para estructura %d en offset %d", ErrInvalidRawSMBIOS, formattedLength, structureType, offset)
		}

		formattedEnd := offset + formattedLength
		stringTerminator := findSMBIOSStringTerminator(table, formattedEnd)
		if stringTerminator < 0 {
			return nil, fmt.Errorf("%w: terminador de cadenas ausente en estructura %d, offset %d", ErrInvalidRawSMBIOS, structureType, offset)
		}

		if structureType == smbiosSystemEnclosureType {
			// Type 3 offset 5 is the enclosure type. Bit 7 is the lock
			// indicator; only bits 6:0 contain the chassis enumeration.
			if formattedLength < 6 {
				return nil, fmt.Errorf("%w: estructura System Enclosure demasiado corta (%d bytes)", ErrInvalidRawSMBIOS, formattedLength)
			}
			chassisType := uint16(table[offset+5] & 0x7f)
			if _, exists := seen[chassisType]; !exists {
				seen[chassisType] = struct{}{}
				chassisTypes = append(chassisTypes, chassisType)
			}
		}

		offset = stringTerminator + 2
		if structureType == smbiosEndOfTableType {
			for _, trailing := range table[offset:] {
				if trailing != 0 {
					return nil, fmt.Errorf("%w: datos no nulos después de End-of-Table", ErrInvalidRawSMBIOS)
				}
			}
			break
		}
	}

	sort.Slice(chassisTypes, func(i, j int) bool { return chassisTypes[i] < chassisTypes[j] })
	return chassisTypes, nil
}

func findSMBIOSStringTerminator(table []byte, start int) int {
	for index := start; index+1 < len(table); index++ {
		if table[index] == 0 && table[index+1] == 0 {
			return index
		}
	}
	return -1
}
