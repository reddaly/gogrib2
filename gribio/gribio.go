// Package gribio contains functionality for reading grib files containing both
// GRIB1 and GRIB2 messages.
package gribio

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/golang/glog"
	"github.com/sdifrance/gogrib2/grib1"
)

type File struct {
	grib1Messages []*grib1.Message
}

func (f *File) GRIB1Messages() []*grib1.Message {
	return f.grib1Messages
}

func ReadFile(r io.Reader) (*File, error) {
	var grib1Messages []*grib1.Message

	rr := bufio.NewReader(r)
	offset := 0
	for {
		glog.Infof("reading record starting at byte offset %d", offset)
		skipCount, err := skipZeros(rr)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return &File{grib1Messages}, nil
			}
			return nil, fmt.Errorf("error parsing file: %w", err)
		}
		offset += skipCount

		parseType, messageLen, err := peekParseType(rr)
		if err != nil {
			return nil, fmt.Errorf("error encountered when expecting a GRIB message: %w", err)
		}
		glog.Infof("record @ offset %d is of type %s", offset, parseType)
		recordBytes := make([]byte, int(messageLen))

		if readCount, err := rr.Read(recordBytes); err != nil {
			return nil, fmt.Errorf("error while reading message of expected length %d; only read %d bytes: %w", messageLen, readCount, err)
		}

		switch parseType {
		case parseAsGRIB1:
			msg, _, err := grib1.Read1(recordBytes)
			if err != nil {
				return nil, fmt.Errorf("error reading GRIB1 message: %w", err)
			}
			grib1Messages = append(grib1Messages, msg)
		case parseAsGRIB2:
			glog.Warningf("skipping GRIB edition 2 messaage @ byte offset %d", offset)
		}
		offset += int(messageLen)
		// Peek record header to decide whether to parse as GRIB1 or GRIB2.
	}
}

func skipZeros(rr *bufio.Reader) (int, error) {
	skipCount := 0
	for {
		b, err := rr.ReadByte()
		if err != nil {
			return skipCount, err
		}
		if b == 0 {
			skipCount++
			continue
		}
		if err := rr.UnreadByte(); err != nil {
			return skipCount, err // Maybe annotate this error?
		}
		return skipCount, nil
	}
}

type parseType int

const (
	parseAsInvalidMessage parseType = iota
	parseAsGRIB1
	parseAsGRIB2
)

func peekParseType(rr *bufio.Reader) (parseType, uint64, error) {
	// Peek 16 bytes.
	data, err := rr.Peek(16)
	if err != nil {
		return parseAsInvalidMessage, 0, fmt.Errorf("error while expecting GRIB record: %w", err)
	}

	if got, want := string(data[0:4]), "GRIB"; got != want {
		return parseAsInvalidMessage, 0, fmt.Errorf("first four bytes = %q, want %q", got, want)
	}
	edition := data[7]

	switch edition {
	case 1:
		// https://apps.ecmwf.int/codes/grib/format/grib1/sections/0/
		messageLength := uint64(binary.BigEndian.Uint32([]byte{0, data[4], data[5], data[6]}))
		return parseAsGRIB1, messageLength, nil
	case 2:
		// https://apps.ecmwf.int/codes/grib/format/grib2/sections/0/
		messageLength := binary.BigEndian.Uint64(data[8 : 8+8])
		return parseAsGRIB2, messageLength, nil
	default:
		return parseAsInvalidMessage, 0, fmt.Errorf("invalid edition %d, wanted 1 or 2", edition)
	}
}
