package gogrib2

import (
	"encoding/binary"
	"fmt"
	"time"

	"github.com/golang/glog"
	"github.com/pkg/errors"
	"github.com/sdifrance/gogrib2/internal"
)

// GRIB2 is simplified GRIB2 file structure
type GRIB2 struct {
	RefTime     time.Time
	VerfTime    time.Time
	Name        string
	Description string
	Unit        string
	Level       string
	Values      []Value
}

// Value is data item of GRIB2 file
type Value struct {
	Longitude float64
	Latitude  float64
	Value     float32
}

// Read reads raw GRIB2 files and return slice of structured GRIB2 data
//
// GRIB2 is specified here: https://library.wmo.int/doc_num.php?explnum_id=11283
func Read(data []byte) ([]GRIB2, error) {

	ind := &indicatorSection{}
	if err := ind.parseBytes(data); err != nil {
		return nil, fmt.Errorf("error parsing indicator section: %w", err)
	}

	dlen := len(data)

	if dlen < 4 {
		return nil, errors.New("raw data should be 4 bytes at least")
	}

	gribs := []GRIB2{}

	start := 0
	eod := false
	for !eod {
		if string(data[0:4]) != "GRIB" {
			return nil, errors.New("First 4 bytes of raw data must be 'GRIB'")
		}

		grib := GRIB2{
			Values: []Value{},
		}

		sections := [][]byte{
			nil, // Indicator section: “GRIB”, discipline, GRIB edition number, length of message
			nil, // Identification section
			nil, // Local use section (repeated)
			nil,
			nil,
			nil,
			nil,
			nil, // End section
		}

		size := 16
		sections[0] = data[start : start+size]
		start += size

		prv := -1
		cur := 0
		eof := false
		for !eof {
			fmt.Println(sections)
			prv = cur
			if prv == 7 {
				// block is read -> export data to values

				grib.RefTime = internal.RefTime(sections)

				var err error
				grib.VerfTime, err = internal.VerfTime(sections)
				if err != nil {
					return nil, errors.Wrapf(err, "Failed to get VerfTime")
				}

				grib.Name, grib.Description, grib.Unit, err = internal.GetInfo(sections)
				if err != nil {
					return nil, errors.Wrapf(err, "Failed to GetInfo")
				}

				grib.Level, err = internal.GetLevel(sections)
				if err != nil {
					return nil, errors.Wrapf(err, "Failed to GetLevel")
				}

				var lon, lat []float64
				err = internal.LatLon(sections, &lon, &lat)
				if err != nil {
					return nil, errors.Wrapf(err, "Failed to get longitude and latitude")
				}
				raw, err := internal.UnpackData(sections)
				if err != nil {
					return nil, errors.Wrapf(err, "Failed to unpack data")
				}
				c := len(lon)
				v := make([]Value, c, c)
				for i := 0; i < c; i++ {
					v[i].Longitude = lon[i]
					v[i].Latitude = lat[i]
					v[i].Value = raw[i]
				}

				grib.Values = append(grib.Values, v...)

				sections[2] = nil
				sections[3] = nil
				sections[4] = nil
				sections[5] = nil
				sections[6] = nil
				sections[7] = nil

				if string(data[start:start+4]) == "7777" {
					eof = true
					size = 4
				}
			} else {
				size = int(binary.BigEndian.Uint32(data[start:]))
				cur = int(data[start+4])
				if start+size > len(data) {
					return nil, fmt.Errorf("internal error: tried to read [%d:%d] from data array of length %d", start, start+size, len(data))
				}
				sections[cur] = data[start : start+size]
			}
			start += size
		}

		gribs = append(gribs, grib)

		if start == dlen {
			eod = true
		}
	}

	return gribs, nil
}

type indicatorSection struct {
	discipline    byte
	edition       byte
	messageLength uint64
}

func (is *indicatorSection) parseBytes(data []byte) error {
	/* https://library.wmo.int/doc_num.php?explnum_id=11283

	92.2 Section 0 – Indicator section

	Section 0 – Indicator section
	Octet No. Contents
	1–4 GRIB (coded according to the International Alphabet No. 5)
	5–6 Reserved
	7 Discipline – GRIB Master table number (see Code table 0.0)
	8 GRIB edition number (currently 2)
	9–16 Total length of GRIB message in octets (including Section 0)
	*/

	if len(data) < 16 {
		return fmt.Errorf("invalid GRIB file < 16 bytes long")
	}
	data = data[0:16]
	if got, want := string(data[0:4]), "GRIB"; got != want {
		return fmt.Errorf("first four bytes = %q, want %q", got, want)
	}
	is.discipline = data[6]
	is.edition = data[7]

	is.messageLength = binary.BigEndian.Uint64(data[8 : 8+8])

	glog.Infof("read indicator section %+v", is)

	return nil
}
