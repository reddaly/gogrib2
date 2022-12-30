// Package grib1 contains a parser for GRIB messages that use edition 1.
//
// The specification for GRIB1 is available as PDF from
// https://wmoomm.sharepoint.com/sites/wmocpdb/eve_activityarea/Forms/AllItems.aspx?id=%2Fsites%2Fwmocpdb%2Feve%5Factivityarea%2FWMO%20Codes%2FWMO306%5FvI2%2FPrevEDITIONS%2FGRIB1%2FWMO306%5FvI2%5FGRIB1%5Fen%2Epdf&parent=%2Fsites%2Fwmocpdb%2Feve%5Factivityarea%2FWMO%20Codes%2FWMO306%5FvI2%2FPrevEDITIONS%2FGRIB1&p=true&ga=1
// and in an HTML format https://apps.ecmwf.int/codes/grib/format/grib1/sections/3/.
package grib1

/*

During development of this library, it's useful to grib_dump use grib_dump to
inspect the contents from the C library:
/usr/local/google/home/reddaly/tmp/ERA5_Land_Hourly_20221023_default_00.grib
*/

import (
	"encoding/binary"
	"fmt"
)

// Message is a GRIB1 record.
type Message struct {
	ind     *indicatorSection
	product *productDefSection
	grid    *gridDescriptionSection
	bitmap  *bitmapSection
	binary  *binaryDataSection
}

// String returns a summary description of the message.
func (m *Message) String() string {
	suffix := ""

	if m.grid != nil {
		suffix += fmt.Sprintf(" datarep = %d", m.grid.dataRepresentationType)
	}

	switch m.product.indicatorOfParameter {
	case 169:
		suffix += " (SOLAR DOWNWARD RADIATION)"
	case 165:
		suffix += " (eastward component of the 10m wind)"
	case 166:
		suffix += " (northward component of the 10m wind)"
	}

	return fmt.Sprintf("indicator of parameter = https://apps.ecmwf.int/codes/grib/param-db/?id=%d; table2Version = %d%s", m.product.indicatorOfParameter, m.product.table2Version, suffix)
}

// Value is data item of GRIB2 file
type Value struct {
	Longitude float64
	Latitude  float64
	Value     float32
}

// Read reads data from a raw GRIB file and returns a slice of parsed messages.
//
// GRIB2 is specified here: https://library.wmo.int/doc_num.php?explnum_id=11283
//
// Multiple messages may be present in a single .grib file.
func Read(data []byte) ([]*Message, error) {
	var out []*Message
	unconsumed := data
	offset := 0
	for len(unconsumed) > 0 {
		record, bytesRead, err := read1MaybeZeroPadded(unconsumed)
		if err != nil {
			return nil, fmt.Errorf("error reading GRIB record @ byte offset %d: %w", offset, err)
		}
		out = append(out, record)
		unconsumed = unconsumed[bytesRead:]
		offset += bytesRead
	}
	return out, nil
}

func read1MaybeZeroPadded(data []byte) (*Message, int, error) {
	// It seems some files include zeros at the beginning. Read all the zeros before calling read1.
	zerosConsumed := 0
	for {
		if len(data) == 0 {
			return nil, zerosConsumed, nil
		}
		if data[0] == 0 {
			zerosConsumed++
			data = data[1:]
			continue
		}
		got, recordBytes, err := Read1(data)
		return got, recordBytes + zerosConsumed, err
	}
}

// Read1 reads a single GRIB1 message from a byte array.
func Read1(data []byte) (*Message, int, error) {
	offset := 0
	sec0 := &indicatorSection{}
	bytesRead, err := sec0.parseBytes(data)
	if err != nil {
		return nil, 0, fmt.Errorf("error parsing indicator section: %w", err)
	}
	unconsumed := data[bytesRead:]
	offset += bytesRead

	sec1 := &productDefSection{}
	bytesRead, err = sec1.parseBytes(unconsumed)
	if err != nil {
		return nil, 0, fmt.Errorf("error parsing indicator section: %w", err)
	}
	unconsumed = unconsumed[bytesRead:]
	offset += bytesRead

	var sec2 *gridDescriptionSection
	var sec3 *bitmapSection

	if sec1.gridDescriptionSectionIncluded() {
		sec2 = &gridDescriptionSection{}
		bytesRead, err = sec2.parseBytes(unconsumed)
		if err != nil {
			return nil, 0, fmt.Errorf("error parsing indicator section: %w", err)
		}
		unconsumed = unconsumed[bytesRead:]
		offset += bytesRead
	}

	if sec1.bitmapSectionIncluded() {
		sec3 = &bitmapSection{}
		bytesRead, err = sec3.parseBytes(unconsumed)
		if err != nil {
			return nil, 0, fmt.Errorf("error parsing indicator section: %w", err)
		}
		unconsumed = unconsumed[bytesRead:]
		offset += bytesRead
	}

	sec4 := &binaryDataSection{}
	bytesRead, err = sec4.parseBytes(unconsumed)
	if err != nil {
		return nil, 0, fmt.Errorf("error parsing binary data section: %w", err)
	}
	unconsumed = unconsumed[bytesRead:]
	offset += bytesRead

	sec5 := &endSection{}
	bytesRead, err = sec5.parseBytes(unconsumed)
	if err != nil {
		return nil, 0, fmt.Errorf("error parsing binary data section: %w", err)
	}
	unconsumed = unconsumed[bytesRead:]
	offset += bytesRead

	consumedCount := len(data) - len(unconsumed)
	if consumedCount != int(sec0.messageLength) {
		extraInfo := ""
		if int(sec0.messageLength) > consumedCount {
			unconsumedBytes := data[consumedCount:sec0.messageLength]
			if len(unconsumedBytes) < 100 {
				extraInfo = fmt.Sprintf("; unconsumed bytes = %+v (%q)", unconsumedBytes, string(unconsumedBytes))
			}

		}
		return nil, 0, fmt.Errorf("consumed %d bytes, expected to consume %d based on message length in header%s", consumedCount, sec0.messageLength, extraInfo)
	}

	return &Message{
		sec0, sec1, sec2, sec3, sec4,
	}, consumedCount, nil
}

type indicatorSection struct {
	messageLength uint64
}

func (is *indicatorSection) parseBytes(data []byte) (int, error) {
	/* https://apps.ecmwf.int/codes/grib/format/grib1/overview

	Octets	Key	Type	Content
	1-4	identifier	ascii	GRIB (coded according to the CCITT International Alphabet No. 5)
	5-7	totalLength	unsigned	Total length of GRIB message (including Section 0)
	8	editionNumber	unsigned	GRIB edition number (currently 1)
	*/

	if len(data) < 8 {
		return 0, fmt.Errorf("invalid GRIB file < 8 bytes long")
	}
	messageData := data
	data = data[0:8]
	if got, want := string(data[0:4]), "GRIB"; got != want {
		return 0, fmt.Errorf("first four bytes = %q, want %q", got, want)
	}

	if got, want := data[7], byte(1); got != want {
		return 0, fmt.Errorf("got GRIB edition %d, expected edition %d", got, want)
	}

	is.messageLength = uint64(parse3ByteUint(data[4], data[5], data[6]))

	if int(is.messageLength) > len(messageData) {
		return 0, fmt.Errorf("message length is %d, but only %d bytes supplied", is.messageLength, len(messageData))
	}

	return 8, nil
}

type productDefSection struct {
	section1Length              uint32 // parse3ByteUint(data[0], data[1], data[2])
	table2Version               uint8  // data[3]
	center                      uint8  // data[4]
	generatingProcessIdentifier uint8  // data[5]
	gridDefinition              uint8  // data[6]
	section1Flags               uint8  // data[7]
	// Indicator of parameter (see Code table 2).
	//
	// This might indicate the type of data represented? e.g., 169 corresponds to
	// downward solar radiation. https://apps.ecmwf.int/codes/grib/param-db/?id=169
	indicatorOfParameter                     uint8      // data[8]
	indicatorOfTypeOfLevel                   uint8      // data[9]
	heightPressureEtcOfLevels                uint32     // parse2ByteUint(data[10], data[11])
	yearOfCentury                            uint8      // data[12]
	month                                    uint8      // data[13]
	day                                      uint8      // data[14]
	hour                                     uint8      // data[15]
	minute                                   uint8      // data[16]
	unitOfTimeRange                          UnitOfTime // data[17]
	p1                                       uint8      // data[18]
	p2                                       uint8      // data[19]
	timeRangeIndicator                       uint8      // data[20]
	numberIncludedInAverage                  uint32     // parse2ByteUint(data[21], data[22])
	numberMissingFromAveragesOrAccumulations uint8      // data[23]
	centuryOfReferenceTimeOfData             uint8      // data[24]
	subCentre                                uint8      // data[25]
	decimalScaleFactor                       int32      // parse2ByteUint(data[21], data[22])
}

/*
	Code table 1 – Flag indication relative to Sections 2 and 3

Bit No. Value Meaning
1       0     Section 2 omitted
1       1     Section 2 included
2       0     Section 3 omitted
2       1     Section 3 included
Note: Bits enumerated from left to right.
*/
const (
	section2Included = 1 << 7
	section3Included = 1 << 6
)

func (s *productDefSection) gridDescriptionSectionIncluded() bool {
	return (s.section1Flags & section2Included) != 0
}

func (s *productDefSection) bitmapSectionIncluded() bool {
	return (s.section1Flags & section3Included) != 0
}

func (s *productDefSection) parseBytes(data []byte) (int, error) {
	/* https://apps.ecmwf.int/codes/grib/format/grib1/sections/1/

		Octets	Key	Type	Content
	1-3	section1Length	unsigned	Length of section
	4	table2Version	unsigned	GRIB tables Version No. (currently 3 for international exchange) Version numbers 128-254 are reserved for local use
	5	centre	codetable	Identification of originating/generating centre (see Code table 0 = Common Code table C1 in Part C/c.)
	6	generatingProcessIdentifier	unsigned	Generating process identification number (allocated by originating centre)
	7	gridDefinition	unsigned	Grid definition (Number of grid used from catalogue defined by originating centre)
	8	section1Flags	codeflag	Flag (see Regulation 92.3.2 and Code table 1)
	9	indicatorOfParameter	codetable	Indicator of parameter (see Code table 2)
	10	indicatorOfTypeOfLevel	codetable	Indicator of type of level (see Code table 3)
	11-12			Height, pressure, etc. of levels (see Code table 3)
	13	yearOfCentury	unsigned	Year of century
	14	month	unsigned	Month      Reference time of data date and time of
	15	day	unsigned	Day          start of averaging or accumulation period
	16	hour	unsigned	Hour
	17	minute	unsigned	Minute
	18	unitOfTimeRange	codetable	Indicator of unit of time range (see Code table 4)
	19	P1	unsigned	P1 Period of time (number of time units) (0 for analyses or initialized analyses). Units of time given by octet 18
	20	P2	unsigned	P2 Period of time (number of time units); or Time interval between successive analyses, initialized analyses or forecasts, undergoing averaging or accumulation. Units of time given by octet 18
	21	timeRangeIndicator	codetable	Time range indicator (see Code table 5)
	22-23	numberIncludedInAverage	unsigned	Number included in average, when octet 21 (Code table 5) indicates an average or accumulation; otherwise set to zero
	24	numberMissingFromAveragesOrAccumulations	unsigned	Number missing from averages or accumulations
	25	centuryOfReferenceTimeOfData	unsigned	Century of reference time of data
	26	subCentre	codetable	Sub-centre identification (see common Code table C1 in Part C/c., Note (3))
	27-28	decimalScaleFactor	signed	Units decimal scale factor (D)
	29-40			Reserved: need not be present
	41-nn			Reserved for originating centre use
	*/

	if len(data) < 28 { // data[27] should be decimalScaleFactor
		return 0, fmt.Errorf("GRIB file section must be at least 28 bytes long")
	}
	s.section1Length = parse3ByteUint(data[0], data[1], data[2])
	s.table2Version = data[3]
	s.center = data[4]
	s.generatingProcessIdentifier = data[5]
	s.gridDefinition = data[6]
	s.section1Flags = data[7]
	s.indicatorOfParameter = data[8]
	s.indicatorOfTypeOfLevel = data[9]
	s.heightPressureEtcOfLevels = parse2ByteUint(data[10], data[11])
	s.yearOfCentury = data[12]
	s.month = data[13]
	s.day = data[14]
	s.hour = data[15]
	s.minute = data[16]
	s.unitOfTimeRange = UnitOfTime(data[17])
	s.p1 = data[18]
	s.p2 = data[19]
	s.timeRangeIndicator = data[20]
	s.numberIncludedInAverage = parse2ByteUint(data[21], data[22])
	s.numberMissingFromAveragesOrAccumulations = data[23]
	s.centuryOfReferenceTimeOfData = data[24]
	s.subCentre = data[25]
	s.decimalScaleFactor = parse2ByteInt(data[21], data[22])

	if int(s.section1Length) > len(data) {
		return 0, fmt.Errorf("section 1 claims its length %d is greater than data size %d", s.section1Length, len(data))
	}

	return int(s.section1Length), nil
}

type codetable uint8

type gridDescriptionSection struct {
	// 	Length of section (octets)
	section2Length uint32
	// 	NV number of vertical coordinate parameters
	numberOfVerticalCoordinateValues uint8
	// PV location (octet number) of the list of vertical coordinate parameters, if present; or PL location (octet number) of the list of numbers of points in each row (if no vertical coordinate parameters are present), if present; or 255 (all bits set to 1) if neither are present
	pvlLocation uint8

	// Data representation type (see Code table 6)
	dataRepresentationType DataRepresentationType
	/*Grid definition (according to data representation type octet 6 above)
	33-42			Extensions of grid definition for rotation or stretching of the coordinate system or Lambert conformal projection or Mercator projection
	33-44			Extensions of grid definition for space view perspective projection
	33-52			Extensions of grid definition for stretched and rotated coordinate system
	PV			List of vertical coordinate parameters (length = NV × 4 octets); if present, then PL = 4NV + PV
	PL			List of numbers of points in each row (length = NROWS x 2 octets, where NROWS is the total number of rows defined within the grid description)
	*/
}

func (s *gridDescriptionSection) parseBytes(data []byte) (int, error) {
	/* https://apps.ecmwf.int/codes/grib/format/grib1/sections/1/

			Octets	Key	Type	Content
		1-3	section2Length	unsigned	Length of section (octets)
	4	numberOfVerticalCoordinateValues	unsigned	NV number of vertical coordinate parameters
	5	pvlLocation	unsigned	PV location (octet number) of the list of vertical coordinate parameters, if present; or PL location (octet number) of the list of numbers of points in each row (if no vertical coordinate parameters are present), if present; or 255 (all bits set to 1) if neither are present
	6	dataRepresentationType	codetable	Data representation type (see Code table 6)
	7-32			Grid definition (according to data representation type octet 6 above)
	33-42			Extensions of grid definition for rotation or stretching of the coordinate system or Lambert conformal projection or Mercator projection
	33-44			Extensions of grid definition for space view perspective projection
	33-52			Extensions of grid definition for stretched and rotated coordinate system
	PV			List of vertical coordinate parameters (length = NV × 4 octets); if present, then PL = 4NV + PV
	PL			List of numbers of points in each row (length = NROWS x 2 octets, where NROWS is the total number of rows defined within the grid description)
	*/

	if len(data) < 6 { // data[6] should be valid
		return 0, fmt.Errorf("GRIB file section must be at least 6 bytes long, got %d", len(data))
	}
	s.section2Length = parse3ByteUint(data[0], data[1], data[2])
	s.numberOfVerticalCoordinateValues = data[3]
	// PV location (octet number) of the list of vertical coordinate parameters,
	// if present; or PL location (octet number) of the list of numbers of points
	// in each row (if no vertical coordinate parameters are present), if present;
	// or 255 (all bits set to 1) if neither are present
	s.pvlLocation = data[4]

	s.dataRepresentationType = DataRepresentationType(data[5])

	if int(s.section2Length) > len(data) {
		return 0, fmt.Errorf("section 2 claims its length %d is greater than data size %d", s.section2Length, len(data))
	}

	return int(s.section2Length), nil
}

type bitmapSection struct {
	// 	Length of section (octets)
	section3Length uint32
	// 	Number of unused bits at end of Section 3
	numberOfUnusedBitsAtEndOfSection3 uint8
	// Table reference: If the octets contain zero, a bit-map follows If the
	// octets contain a number, it refers to a predetermined bit-map provided by
	// the centre.
	tableReference uint32

	// The bit-map contiguous bits with a bit to data point correspondence,
	// ordered as defined in the grid definition.
	values []byte
}

func (s *bitmapSection) parseBytes(data []byte) (int, error) {
	/* https://apps.ecmwf.int/codes/grib/format/grib1/sections/1/

			Octets	Key	Type	Content
		1-3	section3Length	unsigned	Length of section (octets)
	4	numberOfVerticalCoordinateValues	unsigned	NV number of vertical coordinate parameters
	5	pvlLocation	unsigned	PV location (octet number) of the list of vertical coordinate parameters, if present; or PL location (octet number) of the list of numbers of points in each row (if no vertical coordinate parameters are present), if present; or 255 (all bits set to 1) if neither are present
	6	dataRepresentationType	codetable	Data representation type (see Code table 6)
	7-32			Grid definition (according to data representation type octet 6 above)
	33-42			Extensions of grid definition for rotation or stretching of the coordinate system or Lambert conformal projection or Mercator projection
	33-44			Extensions of grid definition for space view perspective projection
	33-52			Extensions of grid definition for stretched and rotated coordinate system
	PV			List of vertical coordinate parameters (length = NV × 4 octets); if present, then PL = 4NV + PV
	PL			List of numbers of points in each row (length = NROWS x 2 octets, where NROWS is the total number of rows defined within the grid description)
	*/

	if len(data) < 6 { // data[5] should be valid
		return 0, fmt.Errorf("GRIB file section must be at least 6 bytes long, got %d", len(data))
	}
	s.section3Length = parse3ByteUint(data[0], data[1], data[2])
	s.numberOfUnusedBitsAtEndOfSection3 = data[3]
	s.tableReference = parse2ByteUint(data[4], data[5])

	if int(s.section3Length) > len(data) {
		return 0, fmt.Errorf("section 3 claims its length %d is greater than data size %d", s.section3Length, len(data))
	}

	if s.tableReference == 0 {
		s.values = data[6:s.section3Length]
	}

	return int(s.section3Length), nil
}

type real int64

type binaryDataSection struct {
	// 	Length of section (octets)
	section4Length uint32
	// 	Flag (see Code table 11) (first 4 bits). Number of unused bits at end of Section 4 (last 4 bits)
	dataFlag uint8
	// Table reference: If the octets contain zero, a bit-map follows If the
	// octets contain a number, it refers to a predetermined bit-map provided by
	// the centre.
	binaryScaleFactor int32

	// Reference value (minimum of packed values)
	referenceValue real
	// Number of bits containing each packed value
	bitsPerValue uint8

	// Variable, depending on the flag value in octet 4.
	variables []float64
}

func (s *binaryDataSection) parseBytes(data []byte) (int, error) {
	/* https://apps.ecmwf.int/codes/grib/format/grib1/sections/1/

			Octets	Key	Type	Content
		1-3	section4Length	unsigned	Length of section (octets)
	4	numberOfVerticalCoordinateValues	unsigned	NV number of vertical coordinate parameters
	5	pvlLocation	unsigned	PV location (octet number) of the list of vertical coordinate parameters, if present; or PL location (octet number) of the list of numbers of points in each row (if no vertical coordinate parameters are present), if present; or 255 (all bits set to 1) if neither are present
	6	dataRepresentationType	codetable	Data representation type (see Code table 6)
	7-32			Grid definition (according to data representation type octet 6 above)
	33-42			Extensions of grid definition for rotation or stretching of the coordinate system or Lambert conformal projection or Mercator projection
	33-44			Extensions of grid definition for space view perspective projection
	33-52			Extensions of grid definition for stretched and rotated coordinate system
	PV			List of vertical coordinate parameters (length = NV × 4 octets); if present, then PL = 4NV + PV
	PL			List of numbers of points in each row (length = NROWS x 2 octets, where NROWS is the total number of rows defined within the grid description)
	*/

	if len(data) < 11 { // data[10] should be valid
		return 0, fmt.Errorf("GRIB file section must be at least 11 bytes long, got %d", len(data))
	}
	s.section4Length = parse3ByteUint(data[0], data[1], data[2])
	s.dataFlag = data[3]
	s.binaryScaleFactor = parse2ByteInt(data[4], data[5])
	s.referenceValue = parse4ByteReal(data[6], data[7], data[8], data[9])

	if int(s.section4Length) > len(data) {
		return 0, fmt.Errorf("section 3 claims its length %d is greater than data size %d", s.section4Length, len(data))
	}

	// 	Data shall be coded in the form of non-negative scaled differences from a reference value.
	// Notes:
	// (1) The reference value is normally the minimum value of the data set which is represented.
	// (2) The actual value Y (in the units of Code table 2) is linked to the coded value X, the reference
	// value R, the binary scale factor E and the decimal scale factor D by means of the following
	// formula:
	// Y × 10D = R + X × 2E

	return int(s.section4Length), nil
}

type endSection struct{}

func (s *endSection) parseBytes(data []byte) (int, error) {
	if len(data) < 4 {
		return 0, fmt.Errorf("got end section length %d, expected data length of at least 4", len(data))
	}
	if got, want := string(data[0:4]), "7777"; got != want {
		return 0, fmt.Errorf("got end sequence %q, want %q", got, want)
	}
	return 4, nil
}

/*
Note on endinaness:

SPECIFICATIONS OF OCTET CONTENTS
Notes:
(1) Octets are numbered 1, 2, 3, etc., starting at the beginning of each section.
(2) In the following, bit positions within octets are referred to as bit 1 to bit 8, where bit 1 is the most significant and bit
8 is the least significant bit. Thus, an octet with only bit 8 set to 1 would have the integer value 1.

*/

func parse4ByteUint(byte0, byte1, byte2, byte3 byte) uint32 {
	return binary.BigEndian.Uint32([]byte{byte0, byte1, byte2, byte3})
}

func parse3ByteUint(byte0, byte1, byte2 byte) uint32 {
	return parse4ByteUint(0, byte0, byte1, byte2)
}

func parse2ByteUint(byte0, byte1 byte) uint32 {
	return parse3ByteUint(0, byte0, byte1)
}

func parse2ByteInt(byte0, byte1 byte) int32 {
	// A negative value of D shall be indicated by setting the high-order bit (bit 1) in the left-hand octet to 1 (on).
	unsigned := parse2ByteUint(byte0, byte1)
	absValue := unsigned & 0b0111111111111111
	negative := unsigned&(1<<15) != 0
	if negative {
		return -1 * int32(absValue)
	}
	return int32(absValue)
}

func parse4ByteReal(byte0, byte1, byte2, byte3 byte) real {
	// A negative value of D shall be indicated by setting the high-order bit (bit 1) in the left-hand octet to 1 (on).
	return real(parse4ByteUint(byte0, byte1, byte2, byte3))
}

// UnitOfTime is based on table 4 from the spec. See
// https://github.com/ecmwf/eccodes/blob/fd549250dc5fe8f7f07dd242b8e781f73982735f/definitions/grib1/4.table
type UnitOfTime uint8

// Units of time from the GRIB1 spec.
//
// See https://apps.ecmwf.int/codes/grib/format/grib1/ctable/4/
const (
	UnitOfTimeMinute    = 0
	UnitOfTimeHour      = 1
	UnitOfTimeDay       = 2
	UnitOfTimeMonth     = 3
	UnitOfTimeYear      = 4
	UnitOfTimeDecade    = 5
	UnitOfTimeNormal    = 6
	UnitOfTimeCentury   = 7
	UnitOfTime3Hours    = 10
	UnitOfTime6Hours    = 11
	UnitOfTime12Hours   = 12
	UnitOfTime15Minutes = 13
	UnitOfTime30Minutes = 14
	UnitOfTimeSecond    = 254
)

// DataRepresentationType indicates the data representation used.
type DataRepresentationType uint8

const (
	// DataRepresentationTypeLL indicates Latitude/Longitude Grid.
	DataRepresentationTypeLL = 0
	// DataRepresentationTypeMM indicates Mercator Projection Grid.
	DataRepresentationTypeMM = 1
	// DataRepresentationTypeGP indicates Gnomonic Projection Grid.
	DataRepresentationTypeGP = 2
	// DataRepresentationTypeLC indicates Lambert Conformal.
	DataRepresentationTypeLC = 3
	// DataRepresentationTypeGG indicates Gaussian Latitude/Longitude Grid.
	DataRepresentationTypeGG = 4
	// DataRepresentationTypePS indicates Polar Stereographic Projection Grid.
	DataRepresentationTypePS = 5
	// DataRepresentationType6 indicates  Universal Transverse Mercator.
	DataRepresentationType6 = 6
	// DataRepresentationType7 indicates  Simple polyconic projection.
	DataRepresentationType7 = 7
	// DataRepresentationType8 indicates Albers equal-area, secant or tangent, conic or bi-polar.
	DataRepresentationType8 = 8
	// DataRepresentationType9 indicates Miller's cylingrical projection.
	DataRepresentationType9 = 9
	// DataRepresentationType10 indicates Rotated Latitude/Longitude grid.
	DataRepresentationType10 = 10
	// DataRepresentationTypeOL indicates Oblique Lambert conformal.
	DataRepresentationTypeOL = 13
	// DataRepresentationType14 indicates Rotated Gaussian latitude/longitude grid.
	DataRepresentationType14 = 14
	// DataRepresentationType20 indicates Stretched latitude/longitude grid.
	DataRepresentationType20 = 20
	// DataRepresentationType24 indicates Stretched Gaussian latitude/longitude.
	DataRepresentationType24 = 24
	// DataRepresentationType30 indicates Stretched and rotated latitude/longitude.
	DataRepresentationType30 = 30
	// DataRepresentationType34 indicates Stretched and rotated Gaussian latitude/longitude.
	DataRepresentationType34 = 34
	// DataRepresentationTypeSH indicates Spherical Harmonic Coefficients.
	DataRepresentationTypeSH = 50
	// DataRepresentationType60 indicates Rotated Spherical Harmonic coefficients.
	DataRepresentationType60 = 60
	// DataRepresentationType70 indicates Stretched Spherical Harmonic coefficients.
	DataRepresentationType70 = 70
	// DataRepresentationType80 indicates Stretched and rotated Spherical Harmonic.
	DataRepresentationType80 = 80
	// DataRepresentationTypeSV indicates Space view perspective or orthographic grid.
	DataRepresentationTypeSV = 90
	// DataRepresentationType193 indicates Quasi-regular latitude/longitude.
	DataRepresentationType193 = 193
)
