package mode_s

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/rs/zerolog/log"
)

const (
	BdsESAirbornePosition  = "0.5"
	BdsESSurfacePosition   = "0.6"
	BdsESStatus            = "0.7"
	BdsESIdCat             = "0.8"
	BdsESAirVelocity       = "0.9"
	BdsElsDataLinkCap      = "1.0"
	BdsElsGicbCap          = "1.7"
	BdsElsAircraftIdent    = "2.0"
	BdsElsAcasRA           = "3.0"
	BdsEhsSelVertIntent    = "4.0"
	BdsEhsAircraftIntent   = "4.3"
	BdsMetRoutineAirReport = "4.4"
	BdsMetHazartReport     = "4.5"
	BdsEhsTrackTurnReport  = "5.0"
	BdsEhsPosRepCourse     = "5.1"
	BdsEhsPosRepFine       = "5.2"
	BdsEhsAirRefStateVec   = "5.3"
	BdsEhsHeadingSpeed     = "6.0"
)

type bds struct {
}

var (
	ErrUnknownCommBMessage  = errors.New("unable to infer Comm-B message type")
	ErrCommBIncorrectLength = errors.New("Comm-B must be exactly 7 bytes")
	bdsFields               = map[string]string{
		// ADSB Service
		"0.5": "Extended Squitter Airborne Position",
		"0.6": "Extended Squitter Surface Position",
		"0.7": "Extended Squitter status",
		"0.8": "Extended Squitter Identification and Category",
		"0.9": "Extended Squitter Airborne velocity Information",
		"1.0": "Data Link Capability Report",         // ELS Service
		"1.7": "Common usage GICB capability report", // ELS Service
		"2.0": "Aircraft Identity",                   // ELS Service
		"3.0": "ACAS active resolution advisory",     // ELS Service
		"4.0": "Selected vertical intention",         // EHS Service
		"4.3": "Aircraft Intention",
		"4.4": "Meteorological Routine Air Report", // Meteorological Service
		"4.5": "Meteorological Hazard Report",      // Meteorological Service
		"5.0": "Track and Turn Report",             // EHS Service
		"5.1": "Position Report Coarse",
		"5.2": "Position Report Fine",
		"5.3": "Air-referenced State Vector",
		"6.0": "heading and Speed Report", // EHS Service
	}
)

func (f *Frame) DescribeBds() string {
	key := f.BdsMessageType()
	s, ok := bdsFields[key]
	if !ok {
		return key + ": Unknown"
	}
	return key + ": " + s
}

func (f *Frame) BdsMessageType() string {
	return fmt.Sprintf("%d.%d", f.bdsMajor, f.bdsMinor)
}

// Decodes an MB Field
func (f *Frame) decodeCommB() error {
	var err error
	f.bdsMajor, f.bdsMinor, err = inferCommBMessageType(f.message[4:11])
	if nil != err {
		// log the error?
		if !errors.Is(err, ErrUnknownCommBMessage) {
			log.Error().Err(err).Send()
		}
	}

	// Not unknown, just not self-identifying: ErrUnknownCommBMessage means
	// the reply is not one of the ELS registers (1,0, 1,7, 2,0, 3,0), the only
	// ones that carry their own BDS code. The EHS registers do not, so they
	// can only be recognised by content: status bits, reserved bits and value
	// ranges. More than one may fit; the tracker settles that with
	// ResolveCommB.
	if errors.Is(err, ErrUnknownCommBMessage) {
		var alt *int32
		if f.downLinkFormat == 20 && f.validAltitude && f.unit == modesUnitFeet {
			a := f.altitude
			alt = &a
		}
		f.commB = decodeEhs(f.message[4:11], alt)
		if f.commB != nil && len(f.commB.Candidates) == 1 {
			f.setBds(f.commB.Candidates[0])
		}
	}

	switch f.BdsMessageType() {
	case BdsEhsSelVertIntent: // 4.0, sole candidate
		f.targetState = f.commB.B40.targetState()
	case BdsElsDataLinkCap: // 1.0
		f.dataLink = decodeBds10(f.message[4:11])
	case BdsElsGicbCap: // 1.7
		f.gicb = decodeBds17(f.message[4:11])
	case BdsElsAircraftIdent: // 2.0
		f.decodeFlightNumber()
	case BdsElsAcasRA: // 3.0
		f.acasRA = decodeBds30(f.message[4:11])
	}

	// things get a lot murkier from here on in!
	// we should attempt to decode each BDS frame, in turn.
	// if we cannot decode it as a given frame (lots of error checking) fall through to the next type

	// BDS 4,0 - BDS status bits = 1, 14, 26, 37, 48
	// BDS 4,3 - BDS status bits = 1, 13, 26. bits 43-56 are 0's
	// BDS 4,4 - BDS status bits = 5, 24, 35, 47, 50
	// BDS 4,5 - BDS status bits = 1, 4, 7, 10, 13, 16, 27, 39. 52-56 are 0's
	// BDS 5,0 - BDS status bits = 1, 12, 24, 35, 46
	// BDS 5,1 - BDS status bits = 1
	// BDS 5,2 - BDS status bits = 1
	// BDS 5,3 - BDS status bits = 1, 13, 24, 34, 47
	// BDS 6,0 - BDS status bits = 1, 13, 24, 35, 46

	return nil
}

// setBds sets the register from a code in "4.0" form.
func (f *Frame) setBds(code string) {
	f.bdsMajor, f.bdsMinor = code[0]-'0', code[2]-'0'
}

func (b *Bds40) targetState() *TargetState {
	return &TargetState{
		Source:              TargetStateFromCommB,
		SelectedAltitudeMcp: b.SelectedAltitudeMcp,
		SelectedAltitudeFms: b.SelectedAltitudeFms,
		BaroSetting:         b.BaroSetting,
		NavModes:            b.NavModes,
	}
}

// inferCommBMessageType uses some fancy guesswork to determine the type of response we have.
// pass in the Comm-B message bytes (MB Field)
// decoding based on this https://mode-s.org/decode/content/mode-s/9-inference.html
func inferCommBMessageType(mb []byte) (byte, byte, error) {
	if len(mb) != 7 {
		return 0, 0, ErrCommBIncorrectLength
	}

	// BDS == Comm-B Data Selector

	// Starting with ELS Detection

	// BDS 1,0 - Data Link Capability Report
	// 00 01 00 00 == 0x10  && X0 00 00 XX
	if mb[0] == 0b0001_0000 && mb[1]&0b0111_1100 == 0 {
		return 1, 0, nil
	}

	// BDS 1,7 - Common usage GICB capability report
	// bit 7 == 1, bits 29-56 zeros
	// Detection: BDS Code && Reserved Bits
	if mb[0]&0x2 == 0x2 && ((0 == mb[3]&0xF) && (0 == mb[4]|mb[5]|mb[6])) {
		return 1, 7, nil
	}

	// BDS 2, 0
	// bits 1-8 ==
	// Detection: BDS Code && Callsign
	if mb[0] == 0b0010_0000 {
		// bits 9-56 are call sign, should not contain any ? chars from aisCharset
		callsign := string(decodeFlightNumber(mb[1:7]))
		if callsign != "" && !strings.Contains(callsign, "?") {
			return 2, 0, nil
		}
	}

	// BDS 3, 0
	// Detection: BDS Code && Threat Type && ACAS
	var bits uint64
	// get bits 16-22 as the LSB
	bits = ((uint64(mb[1]) << 8) | uint64(mb[2])) >> 2
	if mb[0] == 0b0011_0000 && mb[3]&0b0000_1100 != 0b0000_1100 && bits&0b0111_1111 < 48 {
		return 3, 0, nil
	}

	// EHS registers are tested separately (decodeEhs): several can match.

	// and lastly onto Meteorological Detection
	// TODO: Implement MRAR and MHR

	return 0, 0, ErrUnknownCommBMessage
}

// Comm-B Elementary Surveillance (ELS) registers: BDS 1,0, 1,7 and 3,0.
// 2,0 is the callsign, read by decodeFlightNumber. Bit numbers are within
// the 56-bit MB field; layouts from ICAO Doc 9871 tables A-2-16, A-2-23
// and A-2-48, checked against pyModeS.

// DataLinkCapability is the BDS 1,0 report: what the transponder can do.
type DataLinkCapability struct {
	Continuation          bool // another capability register follows
	OverlayCommand        bool // overlay command capability
	AcasOperational       bool
	SubnetworkVersion     uint8 // Mode S subnetwork version number
	EnhancedProtocol      bool  // level 5 transponder
	SpecificServices      bool  // Mode S specific services
	UplinkElmThroughput   uint8
	DownlinkElmThroughput uint8
	AircraftIdent         bool // can report BDS 2,0
	Squitter              bool // squitter capability subfield
	SurveillanceIdent     bool // surveillance identifier code
	CommonUsageGicb       bool // can report BDS 1,7
	AcasHybridSurv        bool
	AcasGeneratesRA       bool // ACAS generating TAs and RAs
	AcasVersion           uint8
	DteStatus             uint16
}

// GicbCapability is the BDS 1,7 report: which common-usage registers the
// transponder will answer a ground interrogation with. Codes are in "4.0"
// form, the same as BdsMessageType.
type GicbCapability struct {
	Supported []string
}

// Has reports whether the aircraft says it supports a register.
func (g *GicbCapability) Has(code string) bool {
	if g == nil {
		return false
	}
	for _, c := range g.Supported {
		if c == code {
			return true
		}
	}
	return false
}

// AcasRA is the BDS 3,0 report: the resolution advisory ACAS is issuing.
type AcasRA struct {
	Active           bool // an RA is active (ARA bit 1)
	Corrective       bool
	DownwardSense    bool
	IncreasedRate    bool
	SenseReversal    bool
	AltitudeCrossing bool
	Positive         bool
	NoBelow          bool // RA complement: do not pass below
	NoAbove          bool
	NoLeft           bool
	NoRight          bool
	Terminated       bool
	MultipleThreats  bool
	ThreatType       uint8 // 0 none, 1 identified by ICAO, 2 by altitude, range and bearing

	ThreatIcao    uint32   // type 1
	ThreatAltFt   *int32   // type 2, Mode C altitude
	ThreatRangeNm *float64 // type 2
	ThreatBearing *float64 // type 2, degrees relative to own heading
}

// gicbRegisters is the BDS 1,7 bit map, bit 1 first (Doc 9871 A-2-23).
var gicbRegisters = []string{
	"0.5", "0.6", "0.7", "0.8", "0.9", "0.A", "2.0", "2.1",
	"4.0", "4.1", "4.2", "4.3", "4.4", "4.5", "4.8",
	"5.0", "5.1", "5.2", "5.3", "5.4", "5.5", "5.6", "5.F", "6.0",
}

func bit(mb []byte, n int) bool { return bitsAt(mb, n, n) == 1 }

func decodeBds10(mb []byte) *DataLinkCapability {
	return &DataLinkCapability{
		Continuation:          bit(mb, 9),
		OverlayCommand:        bit(mb, 15),
		AcasOperational:       bit(mb, 16),
		SubnetworkVersion:     uint8(bitsAt(mb, 17, 23)),
		EnhancedProtocol:      bit(mb, 24),
		SpecificServices:      bit(mb, 25),
		UplinkElmThroughput:   uint8(bitsAt(mb, 26, 28)),
		DownlinkElmThroughput: uint8(bitsAt(mb, 29, 32)),
		AircraftIdent:         bit(mb, 33),
		Squitter:              bit(mb, 34),
		SurveillanceIdent:     bit(mb, 35),
		CommonUsageGicb:       bit(mb, 36),
		AcasHybridSurv:        bit(mb, 37),
		AcasGeneratesRA:       bit(mb, 38),
		AcasVersion:           uint8(bitsAt(mb, 39, 40)),
		DteStatus:             uint16(bitsAt(mb, 41, 56)),
	}
}

func decodeBds17(mb []byte) *GicbCapability {
	g := &GicbCapability{}
	for i, code := range gicbRegisters {
		if bit(mb, i+1) {
			g.Supported = append(g.Supported, code)
		}
	}
	return g
}

func decodeBds30(mb []byte) *AcasRA {
	ra := &AcasRA{
		Active:           bit(mb, 9),
		Corrective:       bit(mb, 10),
		DownwardSense:    bit(mb, 11),
		IncreasedRate:    bit(mb, 12),
		SenseReversal:    bit(mb, 13),
		AltitudeCrossing: bit(mb, 14),
		Positive:         bit(mb, 15),
		NoBelow:          bit(mb, 23),
		NoAbove:          bit(mb, 24),
		NoLeft:           bit(mb, 25),
		NoRight:          bit(mb, 26),
		Terminated:       bit(mb, 27),
		MultipleThreats:  bit(mb, 28),
		ThreatType:       uint8(bitsAt(mb, 29, 30)),
	}
	switch ra.ThreatType {
	case 1:
		ra.ThreatIcao = uint32(bitsAt(mb, 31, 54))
	case 2:
		// 31-43: Mode C altitude code; 44-50: range, (n-1)/10 NM, 0 = no
		// data, 127 = beyond 12.55 NM; 51-56: bearing, 6(n-1)+3 degrees,
		// 0 = no data, 61-63 invalid.
		if alt, ok := decodeAC13(uint32(bitsAt(mb, 31, 43))); ok {
			ra.ThreatAltFt = &alt
		}
		if n := bitsAt(mb, 44, 50); n != 0 {
			r := float64(n-1) / 10
			ra.ThreatRangeNm = &r
		}
		if n := bitsAt(mb, 51, 56); n != 0 && n <= 60 {
			b := float64(6*(n-1) + 3)
			ra.ThreatBearing = &b
		}
	}
	return ra
}

// DataLinkCapability returns the BDS 1,0 report carried by this frame, or nil.
func (f *Frame) DataLinkCapability() *DataLinkCapability {
	if f == nil {
		return nil
	}
	return f.dataLink
}

// GicbCapability returns the BDS 1,7 report carried by this frame, or nil.
func (f *Frame) GicbCapability() *GicbCapability {
	if f == nil {
		return nil
	}
	return f.gicb
}

// AcasRA returns the BDS 3,0 report carried by this frame, or nil.
func (f *Frame) AcasRA() *AcasRA {
	if f == nil {
		return nil
	}
	return f.acasRA
}

// Comm-B Enhanced Surveillance (EHS) registers: BDS 4,0, 5,0 and 6,0.
//
// A Comm-B reply does not say which register it carries: the register number
// is in the interrogation, which receivers do not hear. Each register is
// tested in turn (status bits consistent with their values, reserved bits
// zero, values in range), following https://mode-s.org/decode/content/mode-s/9-inference.html.
// More than one register can pass. 5,0 and 6,0 are then told apart against
// the aircraft's known speed and track (ResolveCommB); other ambiguous
// replies are left undecided.
//
// Bit numbers below are within the 56-bit MB field (message bits 33-88).

// Bds40 is the Selected Vertical Intention register.
type Bds40 struct {
	SelectedAltitudeMcp *int32   // ft
	SelectedAltitudeFms *int32   // ft
	BaroSetting         *float64 // hPa
	NavModes            *NavModes
	TargetAltSource     *byte // 0 unknown, 1 aircraft altitude, 2 MCP/FCU, 3 FMS
}

// Bds50 is the Track and Turn Report.
type Bds50 struct {
	Roll         *float64 // degrees, right wing down positive
	TrueTrack    *float64 // degrees
	GroundSpeed  *float64 // kt
	TrackRate    *float64 // degrees per second
	TrueAirspeed *int     // kt
}

// Bds60 is the Heading and Speed Report.
type Bds60 struct {
	MagneticHeading      *float64 // degrees
	IndicatedAirspeed    *int     // kt
	Mach                 *float64
	BaroVerticalRate     *int // ft/min
	InertialVerticalRate *int // ft/min
}

// CommB holds every EHS register a reply could be.
type CommB struct {
	Candidates []string // BDS codes in "4.0" form, in test order
	B40        *Bds40
	B50        *Bds50
	B60        *Bds60
}

// CommB returns the EHS candidates for a DF20/21 reply, or nil when the reply
// matched an ELS register (1,0, 1,7, 2,0, 3,0) or no register at all.
func (f *Frame) CommB() *CommB {
	if f == nil {
		return nil
	}
	return f.commB
}

// decodeEhs tests the MB field against the EHS registers. altFt is the
// altitude from a DF20 header, or nil, for the Mach/airspeed check.
func decodeEhs(mb []byte, altFt *int32) *CommB {
	if [7]byte(mb) == [7]byte{} {
		return nil
	}
	c := &CommB{}
	if b := decodeBds40(mb); b != nil {
		c.B40 = b
		c.Candidates = append(c.Candidates, BdsEhsSelVertIntent)
	}
	if b := decodeBds50(mb); b != nil {
		c.B50 = b
		c.Candidates = append(c.Candidates, BdsEhsTrackTurnReport)
	}
	if b := decodeBds60(mb, altFt); b != nil {
		c.B60 = b
		c.Candidates = append(c.Candidates, BdsEhsHeadingSpeed)
	}
	if len(c.Candidates) == 0 {
		return nil
	}
	return c
}

func decodeBds40(mb []byte) *Bds40 {
	// Reserved bits 40-47 and 52-53 must be zero.
	if bitsAt(mb, 40, 47) != 0 || bitsAt(mb, 52, 53) != 0 {
		return nil
	}
	b := &Bds40{}
	mcp, hasMcp, ok1 := field(mb, 1, 2, 13)
	fms, hasFms, ok2 := field(mb, 14, 15, 26)
	baro, hasBaro, ok3 := field(mb, 27, 28, 39)
	modes, hasModes, ok4 := field(mb, 48, 49, 51)
	src, hasSrc, ok5 := field(mb, 54, 55, 56)
	if !(ok1 && ok2 && ok3 && ok4 && ok5) {
		return nil
	}
	if !(hasMcp || hasFms || hasBaro || hasModes || hasSrc) {
		return nil
	}
	if hasMcp {
		a := int32(mcp) * 16
		if a > 50000 {
			return nil
		}
		b.SelectedAltitudeMcp = &a
	}
	if hasFms {
		a := int32(fms) * 16
		if a > 50000 {
			return nil
		}
		b.SelectedAltitudeFms = &a
	}
	if hasBaro {
		b.BaroSetting = new(800 + float64(baro)*0.1)
	}
	if hasModes {
		var m NavModes
		if modes&0b100 != 0 {
			m |= NavModeVNAV
		}
		if modes&0b010 != 0 {
			m |= NavModeAltHold
		}
		if modes&0b001 != 0 {
			m |= NavModeApproach
		}
		b.NavModes = &m
	}
	if hasSrc {
		s := byte(src)
		b.TargetAltSource = &s
	}
	return b
}

func decodeBds50(mb []byte) *Bds50 {
	b := &Bds50{}
	roll, hasRoll, ok1 := signedField(mb, 1, 2, 11)
	trk, hasTrk, ok2 := signedField(mb, 12, 13, 23)
	gs, hasGs, ok3 := field(mb, 24, 25, 34)
	rate, hasRate, ok4 := signedField(mb, 35, 36, 45)
	tas, hasTas, ok5 := field(mb, 46, 47, 56)
	if !(ok1 && ok2 && ok3 && ok4 && ok5) {
		return nil
	}
	if hasRoll {
		r := float64(roll) * 45 / 256
		// Beyond 35 degrees is almost always another register read as 5,0.
		if math.Abs(r) > 35 {
			return nil
		}
		b.Roll = &r
	}
	if hasTrk {
		t := float64(trk) * 90 / 512
		if t < 0 {
			t += 360
		}
		b.TrueTrack = &t
	}
	if hasGs {
		g := float64(gs) * 2
		if g > 600 {
			return nil
		}
		b.GroundSpeed = &g
	}
	if hasRate {
		b.TrackRate = new(float64(rate) * 8 / 256)
	}
	if hasTas {
		t := int(tas) * 2
		if t > 600 {
			return nil
		}
		b.TrueAirspeed = &t
	}
	if b.GroundSpeed != nil && b.TrueAirspeed != nil && math.Abs(*b.GroundSpeed-float64(*b.TrueAirspeed)) > 200 {
		return nil
	}
	if b.Roll == nil && b.TrueTrack == nil && b.GroundSpeed == nil && b.TrackRate == nil && b.TrueAirspeed == nil {
		return nil
	}
	return b
}

func decodeBds60(mb []byte, altFt *int32) *Bds60 {
	b := &Bds60{}
	hdg, hasHdg, ok1 := signedField(mb, 1, 2, 12)
	ias, hasIas, ok2 := field(mb, 13, 14, 23)
	mach, hasMach, ok3 := field(mb, 24, 25, 34)
	vrb, hasVrb, ok4 := signedField(mb, 35, 36, 45)
	vri, hasVri, ok5 := signedField(mb, 46, 47, 56)
	if !(ok1 && ok2 && ok3 && ok4 && ok5) {
		return nil
	}
	if hasHdg {
		h := float64(hdg) * 90 / 512
		if h < 0 {
			h += 360
		}
		b.MagneticHeading = &h
	}
	if hasIas {
		v := int(ias)
		if v > 500 {
			return nil
		}
		b.IndicatedAirspeed = &v
	}
	if hasMach {
		m := float64(mach) * 2.048 / 512
		if m > 1 {
			return nil
		}
		b.Mach = &m
	}
	if hasVrb {
		v := int(vrb) * 32
		if v > 6000 || v < -6000 {
			return nil
		}
		b.BaroVerticalRate = &v
	}
	if hasVri {
		v := int(vri) * 32
		if v > 6000 || v < -6000 {
			return nil
		}
		b.InertialVerticalRate = &v
	}
	// IAS and Mach must agree at the reported altitude.
	if b.IndicatedAirspeed != nil && b.Mach != nil && altFt != nil {
		if math.Abs(float64(*b.IndicatedAirspeed)-machToCas(*b.Mach, float64(*altFt))) > 20 {
			return nil
		}
	}
	if b.MagneticHeading == nil && b.IndicatedAirspeed == nil && b.Mach == nil &&
		b.BaroVerticalRate == nil && b.InertialVerticalRate == nil {
		return nil
	}
	return b
}

// KnownState is what the tracker already knows about the aircraft, used to
// choose between ambiguous Comm-B candidates.
type KnownState struct {
	GroundSpeed float64 // kt
	Track       float64 // degrees
	HasVelocity bool
	AltitudeFt  float64
	HasAltitude bool
	Gicb        *GicbCapability // the aircraft's BDS 1,7 report, if heard
}

// ResolveCommB settles a DF20/21 reply that could be either BDS 5,0 or 6,0
// using what the caller knows about the aircraft, and reports whether the
// frame now has a register. The decoder is stateless: it does not know the
// aircraft's ground speed and track, which is the one thing that tells the
// two registers apart. The tracker, which does, calls this before reading
// BdsMessageType. A frame already settled (an ELS register, or a sole EHS
// candidate) is unchanged.
func (f *Frame) ResolveCommB(k KnownState) bool {
	if f == nil || f.commB == nil {
		return false
	}
	if f.bdsMajor != 0 {
		return true
	}
	code := ResolveCommB(f.commB, k)
	if code == "" {
		return false
	}
	f.setBds(code)
	return true
}

// ResolveCommB picks one register for an ambiguous reply, or "" when it
// cannot. A single candidate is returned as is. 5,0 against 6,0 is decided by
// which one's speed and direction is closer to the known ground speed and
// track: 5,0 reports ground speed and track, 6,0 airspeed and heading, which
// differ only by the wind.
func ResolveCommB(c *CommB, k KnownState) string {
	if c == nil || len(c.Candidates) == 0 {
		return ""
	}
	if len(c.Candidates) == 1 {
		return c.Candidates[0]
	}
	// A register the aircraft says it does not support is not a candidate.
	if k.Gicb != nil {
		var kept []string
		for _, code := range c.Candidates {
			if k.Gicb.Has(code) {
				kept = append(kept, code)
			}
		}
		switch len(kept) {
		case 0:
			return ""
		case 1:
			return kept[0]
		}
		c = &CommB{Candidates: kept, B40: c.B40, B50: c.B50, B60: c.B60}
		if !k.Gicb.Has(BdsEhsSelVertIntent) {
			c.B40 = nil
		}
	}
	if c.B50 == nil || c.B60 == nil || c.B40 != nil || !k.HasVelocity {
		return ""
	}
	d50, ok50 := vectorDistance(spd50(c.B50), c.B50.TrueTrack, k)
	d60, ok60 := vectorDistance(spd60(c.B60, k), c.B60.MagneticHeading, k)
	switch {
	case ok50 && ok60:
		if d50 < d60 {
			return BdsEhsTrackTurnReport
		}
		return BdsEhsHeadingSpeed
	case ok50:
		return BdsEhsTrackTurnReport
	case ok60:
		return BdsEhsHeadingSpeed
	}
	return ""
}

func spd50(b *Bds50) *float64 {
	if b.GroundSpeed != nil {
		return b.GroundSpeed
	}
	if b.TrueAirspeed != nil {
		return new(float64(*b.TrueAirspeed))
	}
	return nil
}

func spd60(b *Bds60, k KnownState) *float64 {
	switch {
	case b.Mach != nil && k.HasAltitude:
		return new(*b.Mach * speedOfSound(k.AltitudeFt))
	case b.IndicatedAirspeed != nil && k.HasAltitude:
		return new(casToTas(float64(*b.IndicatedAirspeed), k.AltitudeFt))
	case b.IndicatedAirspeed != nil:
		return new(float64(*b.IndicatedAirspeed))
	}
	return nil
}

func vectorDistance(spd, dir *float64, k KnownState) (float64, bool) {
	if spd == nil || dir == nil {
		return 0, false
	}
	r := func(d float64) float64 { return d * math.Pi / 180 }
	dx := *spd*math.Sin(r(*dir)) - k.GroundSpeed*math.Sin(r(k.Track))
	dy := *spd*math.Cos(r(*dir)) - k.GroundSpeed*math.Cos(r(k.Track))
	return math.Hypot(dx, dy), true
}

// --- ISA atmosphere, enough for airspeed conversions ---

const (
	isaP0     = 101325.0  // Pa
	isaRho0   = 1.225     // kg/m3
	isaT0     = 288.15    // K
	isaR      = 287.05287 // J/(kg K)
	isaGamma  = 1.4
	isaLapse  = 0.0065 // K/m
	isaTropo  = 11000.0
	isaG0     = 9.80665
	ftToM     = 0.3048
	msToKt    = 1 / 0.514444
	isaTTropo = 216.65
)

func isaTempPress(altFt float64) (t, p float64) {
	h := altFt * ftToM
	if h < isaTropo {
		t = isaT0 - isaLapse*h
		p = isaP0 * math.Pow(t/isaT0, isaG0/(isaLapse*isaR))
		return t, p
	}
	pTropo := isaP0 * math.Pow(isaTTropo/isaT0, isaG0/(isaLapse*isaR))
	return isaTTropo, pTropo * math.Exp(-isaG0/(isaR*isaTTropo)*(h-isaTropo))
}

// speedOfSound returns the ISA speed of sound in knots.
func speedOfSound(altFt float64) float64 {
	t, _ := isaTempPress(altFt)
	return math.Sqrt(isaGamma*isaR*t) * msToKt
}

// machToCas converts Mach at a pressure altitude to calibrated airspeed (kt).
func machToCas(mach, altFt float64) float64 {
	_, p := isaTempPress(altFt)
	k := isaGamma / (isaGamma - 1)
	qc := p * (math.Pow(1+(isaGamma-1)/2*mach*mach, k) - 1)
	cas := math.Sqrt(2 * k * isaP0 / isaRho0 * (math.Pow(qc/isaP0+1, 1/k) - 1))
	return cas * msToKt
}

// casToTas converts calibrated to true airspeed (kt) at a pressure altitude.
func casToTas(casKt, altFt float64) float64 {
	t, p := isaTempPress(altFt)
	k := isaGamma / (isaGamma - 1)
	cas := casKt / msToKt
	qc := isaP0 * (math.Pow(1+cas*cas/(2*k*isaP0/isaRho0), k) - 1)
	rho := p / (isaR * t)
	tas := math.Sqrt(2 * k * p / rho * (math.Pow(qc/p+1, 1/k) - 1))
	return tas * msToKt
}
