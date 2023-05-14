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
	"math"
)

// Message is a GRIB1 record.
type Message struct {
	ind     *indicatorSection
	product *ProductDefinition
	grid    *GridDescription
	bitmap  *Bitmap
	binary  *binaryDataSection
}

// ProductDefinition returns an object that describes the data contained in the record.
//
// See https://apps.ecmwf.int/codes/grib/format/grib1/sections/1/.
func (m *Message) ProductDefinition() *ProductDefinition {
	return m.product
}

// Bitmap returns the bitmap infromation stored in the message.
func (m *Message) Bitmap() *Bitmap {
	return m.bitmap
}

// GridDescription returns the GridDescription stored in the message.
func (m *Message) GridDescription() *GridDescription {
	return m.grid
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

	sec1 := &ProductDefinition{}
	bytesRead, err = sec1.parseBytes(unconsumed)
	if err != nil {
		return nil, 0, fmt.Errorf("error parsing indicator section: %w", err)
	}
	unconsumed = unconsumed[bytesRead:]
	offset += bytesRead

	var sec2 *GridDescription
	var sec3 *Bitmap

	if sec1.gridDescriptionSectionIncluded() {
		sec2 = &GridDescription{}
		bytesRead, err = sec2.parseBytes(unconsumed)
		if err != nil {
			return nil, 0, fmt.Errorf("error parsing indicator section: %w", err)
		}
		unconsumed = unconsumed[bytesRead:]
		offset += bytesRead
	}

	if sec1.BitmapIncluded() {
		sec3 = &Bitmap{}
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

// ProductDefinition has information about the contents of a Message.
type ProductDefinition struct {
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
	indicatorOfParameter                     IndicatorOfParameter // data[8]
	indicatorOfTypeOfLevel                   uint8                // data[9]
	heightPressureEtcOfLevels                uint32               // parse2ByteUint(data[10], data[11])
	yearOfCentury                            uint8                // data[12]
	month                                    uint8                // data[13]
	day                                      uint8                // data[14]
	hour                                     uint8                // data[15]
	minute                                   uint8                // data[16]
	unitOfTimeRange                          UnitOfTime           // data[17]
	p1                                       uint8                // data[18]
	p2                                       uint8                // data[19]
	timeRangeIndicator                       uint8                // data[20]
	numberIncludedInAverage                  uint32               // parse2ByteUint(data[21], data[22])
	numberMissingFromAveragesOrAccumulations uint8                // data[23]
	centuryOfReferenceTimeOfData             uint8                // data[24]
	subCentre                                uint8                // data[25]
	decimalScaleFactor                       int32                // parse2ByteUint(data[21], data[22])
}

func (p *ProductDefinition) IndicatorOfParameter() IndicatorOfParameter {
	return p.indicatorOfParameter
}

// IndicatorOfParameter is one of the values from the table defined here: https://codes.ecmwf.int/grib/format/grib1/parameter/2/.
//
// A machine readable list of parameters can be obtianed from https://codes.ecmwf.int/grib/json/.
type IndicatorOfParameter uint8

const (
	ParameterID10MeterUWindComponent          = 165
	ParameterID10MeterVWindComponent          = 166
	ParameterIDSurfaceSolarRadiationDownwards = 169
)

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

func (s *ProductDefinition) gridDescriptionSectionIncluded() bool {
	return (s.section1Flags & section2Included) != 0
}

func (s *ProductDefinition) BitmapIncluded() bool {
	return (s.section1Flags & section3Included) != 0
}

func (s *ProductDefinition) parseBytes(data []byte) (int, error) {
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
	s.indicatorOfParameter = IndicatorOfParameter(data[8])
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

// GridDescription contains information about the coordinate system and bitmap entries.
//
// Based on
type GridDescription struct {
	// 	Length of section (octets)
	section2Length uint32
	// 	NV number of vertical coordinate parameters
	numberOfVerticalCoordinateValues uint8
	// PV location (octet number) of the list of vertical coordinate parameters,
	// if present; or PL location (octet number) of the list of numbers of points
	// in each row (if no vertical coordinate parameters are present), if present;
	// or 255 (all bits set to 1) if neither are present
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

	// parsedValue is the parsed value of the grid description based on dataRepresentationType.
	parsedValue interface{} // *LatLongGrid, for example.
}

func (s *GridDescription) parseBytes(data []byte) (int, error) {
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

	representationBytes := data[6:s.section2Length]
	switch s.dataRepresentationType {
	case DataRepresentationTypeLL:
		grid := &LatLongGrid{}
		if err := grid.parseBytes(representationBytes); err != nil {
			return 0, fmt.Errorf("section 2 failed to parse DataRepresentationTypeLL: %w", err)
		}
		s.parsedValue = grid
	default:
		s.parsedValue = unparsedGridDescription(representationBytes)
		// Don't attempt to parse the remaining bytes.
	}

	return int(s.section2Length), nil
}

// LatLongGrid returns the LatLongGrid parsed from the GridDescription iff
// the DataRepresentationType is DataRepresentationTypeLL. Otherwise, returns
// nil.
func (s *GridDescription) LatLongGrid() *LatLongGrid {
	if x, ok := s.parsedValue.(*LatLongGrid); ok {
		return x
	}
	return nil
}

// unparsedGridDescription stores the part of GridDescription that wasn't parsed.
type unparsedGridDescription []byte

// LatLongGrid specifies a latitude/longitude grid or equidistant cylindrical points.
type LatLongGrid struct {
	numPointsAlongParallel, numPointsAlongMeridian uint16
	firstGridPoint, lastGridPoint                  LatLng
	parallelIncrement, meridianIncrement           QuantizedAngle
	resolutionAndComponentFlags                    resolutionAndComponentFlags
	scanningMode                                   scanningMode
}

func (s *LatLongGrid) parseBytes(data []byte) error {
	/* https://codes.ecmwf.int/grib/format/grib1/grids/0/


	Octets	Key	Type	Content
	7-8	Ni	unsigned	Ni number of points along a parallel
	9-10	Nj	unsigned	Nj number of points along a meridian
	11-13	latitudeOfFirstGridPoint	signed	La1 latitude of first grid point
	14-16	longitudeOfFirstGridPoint	signed	Lo1 longitude of first grid point
	17	resolutionAndComponentFlags	codeflag	Resolution and component flags (see Code table 7)
	18-20	latitudeOfLastGridPoint	signed	La2 latitude of last grid point
	21-23	longitudeOfLastGridPoint	signed	Lo2 longitude of last grid point
	24-25	iDirectionIncrement	unsigned	Di i direction increment
	26-27	jDirectionIncrement	unsigned	Dj j direction increment
	28	scanningMode	codeflag	Scanning mode (flags see Flag/Code table 8)
	29-32			Set to zero (reserved)
	*/
	s.numPointsAlongParallel = uint16(parse2ByteUint(data[0], data[1]))
	s.numPointsAlongMeridian = uint16(parse2ByteUint(data[2], data[3]))

	s.firstGridPoint.lat.milliDegrees = parse3ByteInt(data[4], data[5], data[6])
	s.firstGridPoint.lng.milliDegrees = parse3ByteInt(data[7], data[8], data[9])
	s.resolutionAndComponentFlags = resolutionAndComponentFlags(data[10])
	s.lastGridPoint.lat.milliDegrees = parse3ByteInt(data[11], data[12], data[13])
	s.lastGridPoint.lng.milliDegrees = parse3ByteInt(data[14], data[15], data[16])
	s.parallelIncrement.milliDegrees = int32(parse2ByteUint(data[17], data[18]))
	s.meridianIncrement.milliDegrees = int32(parse2ByteUint(data[19], data[20]))
	s.scanningMode = scanningMode(data[21])

	if !s.scanningMode.pointsScanInPlusIDirection() {
		s.parallelIncrement.milliDegrees *= -1
	}
	if !s.scanningMode.pointsScanInPlusJDirection() {
		s.meridianIncrement.milliDegrees *= -1
	}

	return nil
}

func (s *LatLongGrid) Points() []LatLng {
	var out []LatLng

	if s.scanningMode.adjacentPointsInIDirectionAreConsecutive() {
		for j := 0; j < int(s.numPointsAlongMeridian); j++ {
			lat := s.firstGridPoint.lat
			lat.milliDegrees += int32(j) * s.meridianIncrement.milliDegrees
			for i := 0; i < int(s.numPointsAlongParallel); i++ {
				lng := s.firstGridPoint.lng
				lng.milliDegrees += int32(i) * s.parallelIncrement.milliDegrees
				out = append(out, LatLng{lat, lng})
			}
		}
	} else {
		for i := 0; i < int(s.numPointsAlongParallel); i++ {
			lng := s.firstGridPoint.lng
			lng.milliDegrees += int32(i) * s.parallelIncrement.milliDegrees
			for j := 0; j < int(s.numPointsAlongMeridian); j++ {
				lat := s.firstGridPoint.lat
				lat.milliDegrees += int32(j) * s.meridianIncrement.milliDegrees
				out = append(out, LatLng{lat, lng})
			}
		}
	}

	return out
}

// QuantizedAngle is used for a lat/lng point.
type QuantizedAngle struct {
	milliDegrees int32
}

// Degrees returns the angle in degrees.
func (a QuantizedAngle) Degrees() float32 {
	return float32(a.milliDegrees) / 1000
}

// LatLng represents a latitude/longitude point.
type LatLng struct {
	lat, lng QuantizedAngle
}

// String returns a human-readable representation of the lat/lng.
func (ll LatLng) String() string {
	return fmt.Sprintf("%f, %f", ll.lat.Degrees(), ll.lng.Degrees())
}

// Plus adds one Lat/Lng to another.
func (ll LatLng) Plus(other LatLng) LatLng {
	ll.lat.milliDegrees += other.lat.milliDegrees
	ll.lng.milliDegrees += other.lng.milliDegrees
	return ll
}

// Lat returns the latitude.
func (ll LatLng) Lat() QuantizedAngle { return ll.lat }

// Lng returns the longitue.
func (ll LatLng) Lng() QuantizedAngle { return ll.lng }

// resolutionAndComponentFlags describes a value from table 7 https://codes.ecmwf.int/grib/format/grib1/flag/7/.
type resolutionAndComponentFlags uint8

const (
	directionIncrementsGiven     = 1 << 7
	earthAssumedOblateSpheroidal = 1 << 6
)

func (f resolutionAndComponentFlags) DirectionIncrementsGiven() bool {
	return (f & directionIncrementsGiven) != 0
}

// scanningMode is a value for the codepoint flag described here:
// https://codes.ecmwf.int/grib/format/grib1/flag/8/. It affects
// how grid representation incrementing works.
type scanningMode uint8

func (m scanningMode) String() string {
	iDir := "-i"
	if m.pointsScanInPlusIDirection() {
		iDir = "+i"
	}
	jDir := "-j"
	if m.pointsScanInPlusJDirection() {
		jDir = "+j"
	}
	adj := "jDirAdj"
	if m.adjacentPointsInIDirectionAreConsecutive() {
		adj = "iDirAdj"
	}

	return fmt.Sprintf("(%s, %s, %s)", iDir, jDir, adj)
}

const (
	pointsScanInMinusIDirection    = 1 << 7
	pointsScanInPlusJDirection     = 1 << 6
	adjPointsJDirectionConsecutive = 1 << 5
)

func (m scanningMode) pointsScanInPlusIDirection() bool {
	return (m & pointsScanInMinusIDirection) == 0
}

func (m scanningMode) pointsScanInPlusJDirection() bool {
	return (m & pointsScanInPlusJDirection) != 0
}

func (m scanningMode) adjacentPointsInIDirectionAreConsecutive() bool {
	return (m & adjPointsJDirectionConsecutive) == 0
}

type Bitmap struct {
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

func (s *Bitmap) parseBytes(data []byte) (int, error) {
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
	dataFlag binaryDataFlag
	// Table reference: If the octets contain zero, a bit-map follows If the
	// octets contain a number, it refers to a predetermined bit-map provided by
	// the centre.
	binaryScaleFactor int32

	// Reference value (minimum of packed values)
	referenceValue real
	// Number of bits containing each packed value
	bitsPerValue uint8

	// Variable, depending on the flag value in octet 4.
	variables         []float32
	unparsedVariables []byte
}

func (s *binaryDataSection) parseBytes(data []byte) (int, error) {
	/* https://codes.ecmwf.int/grib/format/grib1/sections/4/

	1-3	section4Length	unsigned	Length of section
	4	dataFlag	codeflag	Flag (see Code table 11) (first 4 bits). Number of unused bits at end of Section 4 (last 4 bits)
	5-6	binaryScaleFactor	signed	Scale factor (E)
	7-10	referenceValue	real	Reference value (minimum of packed values)
	11	bitsPerValue	unsigned	Number of bits containing each packed value
	12-nn			Variable, depending on the flag value in octet 4
	*/

	if len(data) < 11 { // data[10] should be valid
		return 0, fmt.Errorf("GRIB file section must be at least 11 bytes long, got %d", len(data))
	}
	s.section4Length = parse3ByteUint(data[0], data[1], data[2])
	s.dataFlag = binaryDataFlag(data[3])
	s.binaryScaleFactor = parse2ByteInt(data[4], data[5])
	s.referenceValue = parse4ByteReal(data[6], data[7], data[8], data[9])
	s.bitsPerValue = data[10]

	if int(s.section4Length) > len(data) {
		return 0, fmt.Errorf("section 3 claims its length %d is greater than data size %d", s.section4Length, len(data))
	}

	// 	Data shall be coded in the form of non-negative scaled differences from a reference value.
	// Notes:
	// (1) The reference value is normally the minimum value of the data set which is represented.
	// (2) The actual value Y (in the units of Code table 2) is linked to the coded value X, the reference
	// value R, the binary scale factor E and the decimal scale factor D by means of the following
	// formula:
	// Y × 10^D = R + (X1 + X2) × 2^E
	if s.dataFlag.floatingPointValuesRepresented() {
		if s.bitsPerValue != 32 {
			return 0, fmt.Errorf("bitsPerValue = %d, wanted 32 for floating point values", s.bitsPerValue)
		}
		unparsedVariables := data[11:]
		if len(unparsedVariables)%4 != 0 {
			return 0, fmt.Errorf("len(data) = %d isn't divisible by 4", len(unparsedVariables))
		}
		for i := 0; i < len(unparsedVariables); i += 4 {
			s.variables = append(s.variables, math.Float32frombits(binary.LittleEndian.Uint32(unparsedVariables[0:4])))
		}
	} else {
		s.unparsedVariables = data[11:]
	}

	return int(s.section4Length), nil
}

// https://codes.ecmwf.int/grib/format/grib1/flag/11/
type binaryDataFlag uint8

const (
	binaryDataFlagSphericalHarmonicCoefficients = 1 << (8 - 1)
	binaryDataFlagComplexOrSecondOrderPacking   = 1 << (8 - 2)
	binaryDataFlagIntegerValues                 = 1 << (8 - 3)
	binaryDataFlagOctet14ContainsMoreFlagValues = 1 << (8 - 4)
)

func (f binaryDataFlag) floatingPointValuesRepresented() bool {
	return f&binaryDataFlagIntegerValues == 0
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
	absValue := (unsigned & 0b0111111111111111)
	negative := unsigned&(1<<15) != 0
	if negative {
		return -1 * int32(absValue)
	}
	return int32(absValue)
}

func parse3ByteInt(byte0, byte1, byte2 byte) int32 {
	unsigned := parse3ByteUint(byte0, byte1, byte2)
	absValue := (unsigned & 0b011111111111111111111111)
	negative := unsigned&(1<<23) != 0
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
