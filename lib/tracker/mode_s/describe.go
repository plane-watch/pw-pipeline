package mode_s

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

type featureDescriptionType struct {
	field, meaning string
}

type featureBreakdown struct {
	name, longName string
	start, end     int
	subFields      map[string][]featureBreakdown
}

var featureDescription = map[string]featureDescriptionType{
	"AA":   {field: "Address Announced", meaning: "aircraft identification in All-Call reply - ICAO"},
	"AC":   {field: "altitude Code", meaning: "Aircraft altitude code. All bits are Zeros if altitude information is not available."},
	"AP":   {field: "Address/Parity", meaning: "Error detection field. Parity overlaid on the address"},
	"AQ":   {field: "Acquisition", meaning: "part of air-to-air protocol"},
	"AB":   {field: "Air Speed Bit", meaning: "0=indicated air speed, 1=true air speed"},
	"AS":   {field: "True/Indicated Air Speed", meaning: "0=indicated air speed, 1=true air speed"},
	"CA":   {field: "Capability", meaning: "aircraft report of system capability"},
	"CC":   {field: "Crosslink Capability", meaning: "Indicates XPDR has ability to support crosslink capability"},
	"DF":   {field: "Downlink Format", meaning: "downlink descriptor"},
	"DI":   {field: "Designator Identification", meaning: "describes content of SD field"},
	"DR":   {field: "Downlink Request", meaning: "aircraft requests permission to send data"},
	"FS":   {field: "flight status", meaning: "aircraft's situation report"},
	"ID":   {field: "Identification", meaning: "equivalent to ATCRBS identity number (squawk)"},
	"II":   {field: "Interrogator Identification", meaning: "site number for multisite features"},
	"KE":   {field: "Control, ELM", meaning: "part of Extended Length Message protocol"},
	"MA":   {field: "Message, Comm-A", meaning: "message to aircraft"},
	"MB":   {field: "Message, Comm-B", meaning: "message from aircraft"},
	"MC":   {field: "Message, Comm-C", meaning: "long message segment to aircraft"},
	"MD":   {field: "Message, Comm-D", meaning: "long message segment from aircraft"},
	"MU":   {field: "Message, Comm-U", meaning: "air-to-air message to aircraft"},
	"MV":   {field: "Message, Comm-V", meaning: "air-to-air message from aircraft"},
	"NC":   {field: "Number, C-segment", meaning: "part of ELM protocol"},
	"ND":   {field: "Number, D-segment", meaning: "part of ELM protocol"},
	"PC":   {field: "Protocol", meaning: "operating commands for the transponder"},
	"PI":   {field: "Parity/Interr.Identity", meaning: "reports source of interrogation. Contains the parity overlaid on the interrogator identity code"},
	"PR":   {field: "Probability of Reply", meaning: "used in stochastic acquisition mode"},
	"RC":   {field: "Reply Control", meaning: "part of ELM protocol"},
	"RI":   {field: "Reply Information", meaning: "aircraft status information for TCAS"},
	"RL":   {field: "Reply Length", meaning: "commands air-to-air reply length"},
	"RR":   {field: "Reply Request", meaning: "commands details of reply"},
	"SD":   {field: "special Designator", meaning: "control codes to transponder"},
	"SL":   {field: "Sensitivity level, ACAS", meaning: "Reports the current operating sensitivity level of TCAS"},
	"SS":   {field: "Surveillance status", meaning: "  0: No condition,  1: Permanent alert, 2: Temporary alert, 3: SPI condition"},
	"UF":   {field: "Uplink Format", meaning: "format descriptor"},
	"UM":   {field: "Utility Message", meaning: "protocol message"},
	"VS":   {field: "Vertical status", meaning: "aircraft status, airborne (0) or on the ground (1)"},
	"  ":   {field: "Padding", meaning: "Unused"},
	"??":   {field: "???", meaning: "Unknown"},
	"CCC":  {field: "Capability Class Code", meaning: "Capability Class Code"},
	"OMC":  {field: "Operational Mode Code", meaning: "Operational Mode Code"},
	"CRC":  {field: "CRC", meaning: "CRC Checksum"},
	"TC":   {field: "DF 17 Message Type", meaning: "Message Type Code"},
	"SUB":  {field: "DF 17 Message Sub Type", meaning: "Message Sub Type"},
	"DATA": {field: "ADS-B Data", meaning: "ADS-B Data"},
	"CHAR": {field: "flight Number", meaning: "1 character of the AIS charset"},
	"TI":   {field: "UTC Sync Time Bit", meaning: "Indicates if the Time of Applicability of the message is UTC Sync'd. 0=no"},
	"CPR":  {field: "CPR Odd/Even", meaning: "CPR Odd/Even Frame Type"},
	"LAT":  {field: "CPR latitude", meaning: "1 of 4 sets of data required to decode planes lat/lon"},
	"LON":  {field: "CPR longitude", meaning: "1 of 4 sets of data required to decode planes lat/lon"},
	"CAT":  {field: "Aircraft Category", meaning: "Category field includes DF field"},
	"MOV":  {field: "Movement Field", meaning: "Ground Speed"},
	"HB":   {field: "heading Bit", meaning: "There is a heading available"},
	"HD":   {field: "heading Field", meaning: "The direction the plane is facing"},
	"VR":   {field: "Vertical Rate", meaning: "How fast the plane is going up or down"},
	"VRS":  {field: "Vertical Rate Sign", meaning: "0=up 1=down"},
	"EWD":  {field: "East/West Direction", meaning: "Non-zero == negative velocity. 0=east, 1=west"},
	"EWV":  {field: "East/West velocity", meaning: "How fast the plane is going in the indicated direction"},
	"NSD":  {field: "North/South Direction", meaning: "Non-zero == negative velocity. 0=north,1=south"},
	"NSV":  {field: "North/South velocity", meaning: "How fast the plane is going in the indicated direction"},
	"VSRC": {field: "Source Antenna", meaning: "Which antenna this signal was transitted from"},
	"HAED": {field: "Height Above Ellipsoid (HAE) Direction", meaning: "Direction indicator: 1=down, 0=up"},
	"HAEV": {field: "Height Above Ellipsoid (HAE) Delta", meaning: "Barometer offset"},
	"EID":  {field: "Emergency ID", meaning: "Emergency Table Lookup ID"},

	"IC":  {field: "Intent Change", meaning: "If aircraft wants to change altitude etc"},
	"IFR": {field: "Instrument flight Rules Capability", meaning: "ADSB v1 Only"},

	"NICp": {field: "Navigation Integrity Category", meaning: ""},
	"NICb": {field: "Navigation Integrity Category Supplement B", meaning: ""},
	"NACv": {field: "Navigation Accuracy Category", meaning: ""},
	"NUC":  {field: "Navigation Uncertainty Category", meaning: ""},
	"SIL":  {field: "Surveillance/Source Integrity Level", meaning: "indicates the probability of exceeding the NIC containment radius"},
	"APLW": {field: "Airplane Width and Length", meaning: ""},
	"VER":  {field: "ADSB Version", meaning: "This airframes ADSB Compatability"},
	"GVA":  {field: "Geometric Vertical Accuracy", meaning: ""},

	"NTH": {field: "NIC Altiude|Track/heading", meaning: "Altudude (sub type 0) or track/heading (sub type 1) have been cross checked with other sources"},
	"HRD": {field: "heading North Info", meaning: "heading based on 0=true north, 1=magnetic north"},
}

var featureDF17FlightName = []featureBreakdown{
	{name: "TC", start: 32, end: 37},
	{name: "CAT", start: 37, end: 40},
	{name: "CHAR", start: 40, end: 46},
	{name: "CHAR", start: 46, end: 52},
	{name: "CHAR", start: 52, end: 58},
	{name: "CHAR", start: 58, end: 64},
	{name: "CHAR", start: 64, end: 70},
	{name: "CHAR", start: 70, end: 76},
	{name: "CHAR", start: 76, end: 82},
	{name: "CHAR", start: 82, end: 88},
}
var featureDF17SurfacePosition = []featureBreakdown{
	{name: "TC", start: 32, end: 37},
	{name: "MOV", start: 37, end: 44},
	{name: "HB", start: 44, end: 45},
	{name: "HD", start: 45, end: 52},
	{name: "??", start: 52, end: 53},
	{name: "CPR", start: 53, end: 54},
	{name: "LAT", start: 54, end: 71},
	{name: "LON", start: 71, end: 88},
}
var featureDF17AirPosition = []featureBreakdown{
	{name: "TC", start: 32, end: 37},
	{name: "SS", start: 37, end: 39},
	{name: "NICb", start: 39, end: 40},
	{name: "AC", start: 40, end: 52},
	{name: "TI", start: 52, end: 53},
	{name: "CPR", start: 53, end: 54},
	{name: "LAT", start: 54, end: 71},
	{name: "LON", start: 71, end: 88},
}

var featureDF17AirVelocityUnknown = []featureBreakdown{
	{name: "TC", start: 32, end: 37},
	{name: "SUB", start: 37, end: 40},
	{name: "??", start: 40, end: 88},
}
var featureDF17AirVelocityGroundSpeed = []featureBreakdown{
	{name: "TC", start: 32, end: 37},
	{name: "SUB", start: 37, end: 40},
	{name: "IC", start: 40, end: 41},
	{name: "IFR", start: 41, end: 42},
	{name: "NACv", start: 42, end: 45},
	{name: "EWD", start: 45, end: 46},
	{name: "EWV", start: 46, end: 56},
	{name: "NSD", start: 56, end: 57},
	{name: "NSV", start: 57, end: 67},
	{name: "VSRC", start: 67, end: 68},
	{name: "VRS", start: 68, end: 69},
	{name: "VR", start: 69, end: 78},
	{name: "??", start: 78, end: 80},
	{name: "HAED", start: 80, end: 81},
	{name: "HAEV", start: 81, end: 88},
}
var featureDF17AirVelocityAirSpeed = []featureBreakdown{
	{name: "TC", start: 32, end: 37},
	{name: "SUB", start: 37, end: 40},
	{name: "IC", start: 40, end: 41},
	{name: "IFR", start: 41, end: 42},
	{name: "NACv", start: 42, end: 45},
	{name: "HB", start: 45, end: 46},
	{name: "HD", start: 46, end: 56},
	{name: "AB", start: 56, end: 57},
	{name: "AS", start: 57, end: 67},
	{name: "VSRC", start: 67, end: 68},
	{name: "VRS", start: 68, end: 69},
	{name: "VR", start: 69, end: 78},
	{name: "??", start: 78, end: 80},
	{name: "HAED", start: 80, end: 81},
	{name: "HAEV", start: 81, end: 88},
}

var featureDF17AirVelocity = []featureBreakdown{
	{name: "??", start: 37, end: 88, subFields: map[string][]featureBreakdown{
		"0": featureDF17AirVelocityUnknown,
		"1": featureDF17AirVelocityGroundSpeed,
		"2": featureDF17AirVelocityGroundSpeed,
		"3": featureDF17AirVelocityAirSpeed,
		"4": featureDF17AirVelocityAirSpeed,
		"5": featureDF17AirVelocityUnknown,
		"6": featureDF17AirVelocityUnknown,
		"7": featureDF17AirVelocityUnknown,
	},
	},
}

var asdbFeatures = map[string][]featureBreakdown{
	"1":  featureDF17FlightName,
	"2":  featureDF17FlightName,
	"3":  featureDF17FlightName,
	"4":  featureDF17FlightName,
	"5":  featureDF17SurfacePosition,
	"6":  featureDF17SurfacePosition,
	"7":  featureDF17SurfacePosition,
	"8":  featureDF17SurfacePosition,
	"9":  featureDF17AirPosition,
	"10": featureDF17AirPosition,
	"11": featureDF17AirPosition,
	"12": featureDF17AirPosition,
	"13": featureDF17AirPosition,
	"14": featureDF17AirPosition,
	"15": featureDF17AirPosition,
	"16": featureDF17AirPosition,
	"17": featureDF17AirPosition,
	"18": featureDF17AirPosition,
	"19": featureDF17AirVelocity,
	"20": featureDF17AirPosition,
	"21": featureDF17AirPosition,
	"22": featureDF17AirPosition,
	"23": {
		{name: "TC", start: 32, end: 37},
		{name: "SUB", start: 37, end: 40},
		{name: "ID", start: 40, end: 53},
		{name: "  ", start: 53, end: 88},
	},
	"28": {
		{name: "TC", start: 32, end: 37},
		{name: "SUB", start: 37, end: 40},
		{name: "??", start: 40, end: 88, subFields: map[string][]featureBreakdown{
			"0": {
				{name: "??", start: 40, end: 88, longName: "RESERVED"},
			},
			"1": { // EMERGENCY (or priority), status
				{name: "EID", start: 40, end: 43}, // 3
				{name: "ID", start: 43, end: 56},  // 5 + 8
				{name: "  ", start: 56, end: 88},
			},
			"2": {
				{name: "??", start: 40, end: 88, longName: "ACAS RA Broadcast"},
			},
			"3": {
				{name: "??", start: 40, end: 88, longName: "RESERVED"},
			},
			"4": {
				{name: "??", start: 40, end: 88, longName: "RESERVED"},
			},
			"5": {
				{name: "??", start: 40, end: 88, longName: "RESERVED"},
			},
			"6": {
				{name: "??", start: 40, end: 88, longName: "RESERVED"},
			},
			"7": {
				{name: "??", start: 40, end: 88, longName: "RESERVED"},
			},
		}},
	},
	"29": {
		{name: "TC", start: 32, end: 37},
		{name: "SUB", start: 37, end: 39},
		{name: "??", start: 39, end: 88, subFields: map[string][]featureBreakdown{
			"0": {
				// 39 == MB Field Bit 8 in doco, 7 in 0 based
				{name: "  ", start: 39, end: 41, longName: "Vertical Data Available / Source Indicator"},
				{name: "  ", start: 41, end: 42, longName: "Target Altitude Type"},
				{name: "  ", start: 42, end: 43, longName: "Backward Compatibility Flag"},
				{name: "  ", start: 43, end: 45, longName: "Target Altitude Capability"},
				{name: "  ", start: 45, end: 47, longName: "Vertical Mode Indicator"},
				{name: "  ", start: 47, end: 57, longName: "Target Altitude"},
				{name: "  ", start: 57, end: 59, longName: "Horizontal Data Available"},
				{name: "  ", start: 59, end: 68, longName: "Target Heading / Track Angle"},
				{name: "  ", start: 68, end: 69, longName: "Target Heading / Track Indicator"},
				{name: "  ", start: 69, end: 71, longName: "Horizontal Mode Indicator"},
				{name: "  ", start: 71, end: 75, longName: "Navigation Accuracy Category — Position (NACP)"},
				{name: "  ", start: 75, end: 76, longName: "Navigation Integrity Category — Baro (NICBARO)"},
				{name: "  ", start: 76, end: 78, longName: "Surveillance Integrity Level (SIL)"},
				{name: "  ", start: 78, end: 83, longName: "Reserved"},
				{name: "  ", start: 83, end: 85, longName: "Capability / Mode Codes"},
				{name: "  ", start: 85, end: 88, longName: "Emergency / Priority Status"},
			},
			"1": {
				{name: "  ", start: 39, end: 40, longName: "SIL Supplement"},
				{name: "  ", start: 40, end: 41, longName: "Selected Altitude Type"},
				{name: "alt", start: 41, end: 52, longName: "MCP / FCU | FMS Selected Altitude"},
				{name: "baro", start: 52, end: 61, longName: "Barometric Pressure Setting"},
				{name: "  ", start: 61, end: 62, longName: "Status"},
				{name: "  ", start: 62, end: 63, longName: "Sign"},
				{name: "hdg", start: 63, end: 71, longName: "Selected Heading"},
				{name: "NACp", start: 71, end: 75, longName: "Navigation Accuracy Category for Position (NACp)"},
				{name: "NICb", start: 75, end: 76, longName: "Navigation Integrity Category for Baro (NICbaro)"},
				{name: "SIL", start: 76, end: 78, longName: "Source Integrity Level (SIL)"},
				{name: "  ", start: 78, end: 79, longName: "Status of MCP / FCU Mode Bits"},
				{name: "ap", start: 79, end: 80, longName: "Autopilot Engaged"},
				{name: "vnav", start: 80, end: 81, longName: "VNAV Mode Engaged"},
				{name: "ah", start: 81, end: 82, longName: "Altitude Hold Mode"},
				{name: "  ", start: 82, end: 83, longName: "Reserved for ADS-R Flag"},
				{name: "  ", start: 83, end: 84, longName: "Approach Mode"},
				{name: "tcas", start: 84, end: 85, longName: "TCAS/ACAS Operational"},
				{name: "lnav", start: 85, end: 86, longName: "LNAV Mode"},
				{name: "  ", start: 86, end: 88, longName: "Reserved"},
			},
			"2": {
				{name: "??", start: 39, end: 88, longName: "RESERVED"},
			},
			"3": {
				{name: "??", start: 39, end: 88, longName: "RESERVED"},
			},
		}},
	},
	"31": {
		{name: "TC", start: 32, end: 37},
		{name: "SUB", start: 37, end: 40},
		{name: "CCC", start: 40, end: 88, subFields: map[string][]featureBreakdown{
			// ADSB Version 0
			"0": { // airborne
				{name: "CC ", start: 40, end: 56, longName: "Airborne Capability Class Codes"},
				{name: "OM ", start: 56, end: 72, longName: "Airborne Operational Mode Codes"},
				{name: "Ver", start: 72, end: 75, longName: "ADSB Version Number"},
				{name: "   ", start: 75, end: 76, longName: "NIC Supplement-A"},
				{name: "   ", start: 76, end: 80, longName: "Navigational Accuracy Category – Position NACp"},
				{name: "   ", start: 80, end: 82, longName: "GVA"},
				{name: "   ", start: 82, end: 84, longName: "Source Integrity Level (SIL)"},
				{name: "   ", start: 84, end: 85, longName: "NIC baro"},
				{name: "   ", start: 85, end: 86, longName: "HRD"},
				{name: "   ", start: 86, end: 87, longName: "SIL Supplement"},
				{name: "   ", start: 87, end: 88, longName: "Reserved"},
			},
			"1": { // surface
				{name: "CC", start: 40, end: 52, longName: "Surface Capability Class Codes"},
				{name: "APLW", start: 52, end: 56, longName: "Length/Width Codes"},
				{name: "OM  ", start: 56, end: 72, longName: "Surface Operational Mode Codes"},
				{name: "Ver ", start: 72, end: 75, longName: "ADSB Version Number"},
				{name: "    ", start: 75, end: 76, longName: "NIC Supplement-A"},
				{name: "    ", start: 76, end: 80, longName: "Navigational Accuracy Category – Position NACp"},
				{name: "    ", start: 80, end: 82, longName: "RESERVED"},
				{name: "    ", start: 82, end: 84, longName: "Source Integrity Level (SIL)"},
				{name: "    ", start: 84, end: 85, longName: "TRK/HDG"},
				{name: "    ", start: 85, end: 86, longName: "HRD"},
				{name: "    ", start: 86, end: 87, longName: "SIL Supplement"},
				{name: "    ", start: 87, end: 88, longName: "Reserved"},
			},
		}},
	},
}

var bdsFeatures = map[string][]featureBreakdown{
	"1.0": {
		{name: "BDS#", start: 32, end: 40, longName: "BDS Code"},
		{name: "Conf", start: 40, end: 41, longName: "Configuration flag"},
		{name: "??", start: 41, end: 46, longName: "Reserved"},
		{name: "OCC", start: 46, end: 47, longName: "Overlay Command Capability"},
		{name: "??ACAS", start: 47, end: 48, longName: "Reserved for ACAS"},
		{name: "ModeSver", start: 48, end: 55, longName: "Mode S Subnetwork Version Number"},
		{name: "Enhanced", start: 55, end: 56, longName: "Transponder Enhanced Protocol Indicator"},
		{name: "CAP", start: 56, end: 57, longName: "Mode S Specific Services Capability"},
		{name: "ELM Up", start: 57, end: 60, longName: "Uplink ELM average throughput capacity"},
		{name: "ELM Dn", start: 60, end: 64, longName: "Downlink ELM throughput"},
		{name: "ID Cap", start: 64, end: 65, longName: "Aircraft Identification Capability"},
		{name: "SCS", start: 65, end: 66, longName: "Squitter Capability Subfield"},
		{name: "SIC", start: 66, end: 67, longName: "Surveillance identifier Code"},
		{name: "GICB", start: 67, end: 68, longName: "Common usage GICB capability report"},
		{name: "??ACAS", start: 68, end: 72, longName: "Reserved for ACAS"},
		{name: "DTE", start: 72, end: 88, longName: "Data terminal equipment (DTE) status"},
	},
	"1.7": {
		{name: "F01", start: 32, end: 33, longName: "0,5 Extended squitter airborne position"},
		{name: "F02", start: 33, end: 34, longName: "0,6 Extended squitter surface position"},
		{name: "F03", start: 34, end: 35, longName: "0,7 Extended squitter status"},
		{name: "F04", start: 35, end: 36, longName: "0,8 Extended squitter identification and category"},
		{name: "F05", start: 36, end: 37, longName: "0,9 Extended squitter airborne velocity information"},
		{name: "F06", start: 37, end: 38, longName: "0,A Extended squitter event-driven information"},
		{name: "F07", start: 38, end: 39, longName: "2,0 Aircraft identification"},
		{name: "F08", start: 39, end: 40, longName: "2,1 Aircraft registration number"},
		{name: "F09", start: 40, end: 41, longName: "4,0 Selected vertical intention"},
		{name: "F11", start: 41, end: 42, longName: "4,1 Next waypoint identifier"},
		{name: "F11", start: 42, end: 43, longName: "4,2 Next waypoint position"},
		{name: "F12", start: 43, end: 44, longName: "4,3 Next waypoint information"},
		{name: "F13", start: 44, end: 45, longName: "4,4 Meteorological routine report"},
		{name: "F14", start: 45, end: 46, longName: "4,5 Meteorological hazard report"},
		{name: "F15", start: 46, end: 47, longName: "4.8 VHF channel report"},
		{name: "F16", start: 47, end: 48, longName: "5,0 Track and turn report"},
		{name: "F17", start: 48, end: 49, longName: "5,1 Position coarse"},
		{name: "F18", start: 49, end: 50, longName: "5,2 Position fine"},
		{name: "F19", start: 50, end: 51, longName: "5,3 Air-referenced state vector"},
		{name: "F20", start: 51, end: 52, longName: "5,4 Waypoint 1"},
		{name: "F21", start: 52, end: 53, longName: "5,5 Waypoint 2"},
		{name: "F22", start: 53, end: 54, longName: "5,6 Waypoint 3"},
		{name: "F23", start: 54, end: 55, longName: "5,F Quasi-static parameter monitoring"},
		{name: "F24", start: 55, end: 56, longName: "6,0 heading and speed report"},
		{name: "F25", start: 56, end: 57, longName: "Reserved for aircraft capability"},
		{name: "F26", start: 57, end: 58, longName: "Reserved for aircraft capability"},
		{name: "F27", start: 58, end: 59, longName: "E,1 Reserved for Mode S BITE (Built In Test Equipment)"},
		{name: "F28", start: 59, end: 60, longName: "E,2 Reserved for Mode S BITE (Built In Test Equipment)"},
		{name: "F29", start: 60, end: 61, longName: "F,1 Military applications"},
		{name: "??", start: 61, end: 88, longName: "Reserved"},
	},
	"2.0": {
		{name: "BDS#", start: 32, end: 40, longName: "BDS Code"},
		{name: "CHAR", start: 40, end: 46},
		{name: "CHAR", start: 46, end: 52},
		{name: "CHAR", start: 52, end: 58},
		{name: "CHAR", start: 58, end: 64},
		{name: "CHAR", start: 64, end: 70},
		{name: "CHAR", start: 70, end: 76},
		{name: "CHAR", start: 76, end: 82},
		{name: "CHAR", start: 82, end: 88},
	},
	"3.0": {
		{name: "BDS#", start: 32, end: 40, longName: "BDS Code"},
		{name: "A RA", start: 40, end: 54, longName: "Active resolution advisories"},
		{name: "RA C", start: 54, end: 58, longName: "Resolution advisory complements record"},
		{name: "RA T", start: 58, end: 59, longName: "RA terminated"},
		{name: "MT", start: 59, end: 60, longName: "Multiple threat encounter"},
		{name: "TTI", start: 60, end: 62, longName: "Threat type indicator"},
		{name: "TID", start: 62, end: 86, longName: "Threat identity data"},
		{name: "??", start: 86, end: 88, longName: "Reserved"},
	},
}

var frameFeatures = map[byte][]featureBreakdown{
	0: {
		{name: "DF", start: 0, end: 5},
		{name: "VS", start: 5, end: 6},
		{name: "CC", start: 6, end: 7},
		{name: "  ", start: 7, end: 8},
		{name: "SL", start: 8, end: 11},
		{name: "  ", start: 11, end: 13},
		{name: "RI", start: 13, end: 17},
		{name: "  ", start: 17, end: 20},
		{name: "AC", start: 20, end: 32},
		{name: "AP", start: 32, end: 56},
	},
	4: {
		{name: "DF", start: 0, end: 5},
		{name: "FS", start: 5, end: 8},
		{name: "DR", start: 8, end: 13},
		{name: "UM", start: 13, end: 19},
		{name: "AC", start: 19, end: 32},
		{name: "AP", start: 32, end: 56},
	},
	5: {
		{name: "DF", start: 0, end: 5},
		{name: "FS", start: 5, end: 8},
		{name: "DR", start: 8, end: 13},
		{name: "UM", start: 13, end: 19},
		{name: "ID", start: 19, end: 32},
		{name: "AP", start: 32, end: 56},
	},

	11: {
		{name: "DF", start: 0, end: 5},
		{name: "CA", start: 5, end: 8},
		{name: "AA", start: 8, end: 32},
		{name: "PI", start: 32, end: 56},
	},

	16: {
		{name: "DF", start: 0, end: 5},
		{name: "VS", start: 5, end: 6},
		{name: "  ", start: 6, end: 8},
		{name: "SL", start: 8, end: 11},
		{name: "  ", start: 11, end: 13},
		{name: "RI", start: 13, end: 17},
		{name: "  ", start: 17, end: 19},
		{name: "AC", start: 19, end: 32},
		{name: "MV", start: 32, end: 88},
		{name: "AP", start: 88, end: 112},
	},
	17: {
		{name: "DF", start: 0, end: 5},
		{name: "CA", start: 5, end: 8},
		{name: "AA", start: 8, end: 32},
		{name: "ME", start: 32, end: 88, subFields: asdbFeatures},
		{name: "PI", start: 88, end: 112},
	},
	18: {
		{name: "DF", start: 0, end: 5},
		{name: "CA", start: 5, end: 8},
		{name: "AA", start: 8, end: 32},
		{name: "ME", start: 32, end: 88, subFields: asdbFeatures},
		{name: "CRC", start: 88, end: 112},
	},
	19: {
		{name: "DF", start: 0, end: 5},
	},
	20: {
		{name: "DF", start: 0, end: 5},
		{name: "FS", start: 5, end: 8},
		{name: "DR", start: 8, end: 13},
		{name: "UM", start: 13, end: 19},
		{name: "AC", start: 19, end: 32},
		{name: "MB", start: 32, end: 88, subFields: bdsFeatures},
		{name: "AP", start: 88, end: 112},
	},
	21: {
		{name: "DF", start: 0, end: 5},
		{name: "FS", start: 5, end: 8},
		{name: "DR", start: 8, end: 13}, //
		{name: "UM", start: 13, end: 19},
		{name: "ID", start: 19, end: 32},
		{name: "MB", start: 32, end: 88, subFields: bdsFeatures},
		{name: "AP", start: 88, end: 112},
	},
	24: {
		{name: "DF", start: 0, end: 2},
		{name: "  ", start: 2, end: 3},
		{name: "KE", start: 3, end: 4},
		{name: "ND", start: 4, end: 8},
		{name: "MD", start: 8, end: 88},
		{name: "AP", start: 88, end: 112},
	},
}

func (f *Frame) String() string {
	buf := bytes.NewBufferString("")

	return buf.String()
}
func (f *Frame) Describe(output io.Writer) {
	fprintf(output, "MODE S Packet:\n")
	fprintf(output, "Length              : %d bits\n", f.getMessageLengthBits())
	fprintf(output, "Frame               : %s\n", f.RawString())
	fprintf(output, "DF: Downlink Format : (%d) %s\n", f.downLinkFormat, f.DownLinkFormat())
	if f.mode == "MLAT" {
		fprintf(output, "MLAT: Beast Ticks  : %d (@12mhz clock)\n", f.beastTicks)
		fprintf(output, "MLAT: Beast Uptime  : %s\n", time.Duration(f.beastTicksNs).String())
	}
	// decode the specific DF type
	switch f.downLinkFormat {
	case 0:
		f.showVerticalStatus(output)
		f.showCrossLinkCapability(output)
		f.showSensitivityLevel(output)
		f.showReplyInformation(output)
		f.showAltitude(output)
		f.showICAO(output)
	case 4:
		f.showFlightStatus(output)
		f.showDownLinkRequest(output)
		f.showUtilityMessage(output)
		f.showAltitude(output)
		f.showICAO(output)
	case 5:
		f.showFlightStatus(output)
		f.showDownLinkRequest(output)
		f.showUtilityMessage(output)
		f.showIdentity(output)
		f.showICAO(output)
	case 11:
		f.showCapability(output)
		f.showICAO(output)
	case 16:
		f.showVerticalStatus(output)
		f.showSensitivityLevel(output)
		f.showReplyInformation(output)
		f.showAltitude(output)
		f.showICAO(output)
	case 17:
		f.showCapability(output)
		f.showICAO(output)
		f.showAdsb(output)
	case 18: // DF_18
		// f.showCapability() // control field
		if f.ca == 0 {
			f.showCapability(output)
			f.showICAO(output)
			f.showAdsb(output)
		} else {
			fprintln(output, "Unable to decode DF18 Capability:", f.ca)
		}
	case 20: // DF_20
		f.showFlightStatus(output)
		f.showAltitude(output)
		f.showFlightNumber(output)
		f.showBdsData(output)
		f.showICAO(output)
	case 21: // DF_21
		f.showFlightStatus(output)
		f.showIdentity(output) // gillham encoded squawk
		f.showFlightNumber(output)
		f.showBdsData(output)
		f.showICAO(output)
	}

	f.showBitString(output)

}

func (f *Frame) showVerticalStatus(output io.Writer) {
	if !f.VerticalStatusValid() {
		return
	}
	if f.onGround {
		fprintln(output, "VS: Vertical status : On The Ground")
	} else {
		fprintln(output, "VS: Vertical status : Airborne")
	}
}

func (f *Frame) showVerticalRate(output io.Writer) {
	if f.validVerticalRate {
		fprintf(output, "  Vertical Rate     : %d\n", f.verticalRate)
	} else {
		fprintln(output, "  Vertical Rate     : Invalid")
		fprintln(output, "")
	}
}

func (f *Frame) showCrossLinkCapability(output io.Writer) {
	fprintf(output, "CC: CrossLink Cap   : %d\n", f.cc)
}

func (f *Frame) showAltitude(output io.Writer) {
	if f.validAltitude {
		if f.isGnssAlt {
			fprintf(output, "AC: altitude        : %d %s (GNSS)\n", f.altitude, f.AltitudeUnits())
		} else {
			fprintf(output, "AC: altitude        : %d %s (q bit: %t, m bit: %t)\n", f.altitude, f.AltitudeUnits(), f.acQ, f.acM)
		}
	} else {
		fprintln(output, "AC: altitude        : Invalid")
	}
}

func (f *Frame) showWakeVortex(output io.Writer) {
	var wakeType string
	switch {
	case 1 == f.messageType:
		wakeType = "Reserved!"
	case f.messageType > 4:
		wakeType = "Unknown"
	case f.catSubType == 0:
		wakeType = "No Information Provided"
	default:
		wakeType = wakeVortex[f.messageType][f.catSubType]
	}
	wakeType = fmt.Sprintf("(TC:%d CAT:%d) - %s", f.messageType, f.catSubType, wakeType)

	fprintf(output, "Wake Type           : %s", wakeType)
}

func (f *Frame) showContainmentRadius(output io.Writer) {
	r, err := f.ContainmentRadiusLimit(true)
	if nil != err {
		fprintf(output, "  Containment Radius: %s\n", err)
	} else {
		fprintf(output, "  Containment Radius: %0.2f metres\n", r)
	}
}

func (f *Frame) showSurveilanceStatus(output io.Writer) {
	fprintf(output, "  Surveillance      : (status:%d) %s\n", f.surveillanceStatus, surveillanceStatus[f.surveillanceStatus])
}

func (f *Frame) showNavigationIntegrity(output io.Writer) {
	fprintf(output, "  NIC Supplement B  : %d\n", f.nicSupplementB)
	nic, err := f.NavigationIntegrityCategory(true)
	if nil != err {
		fprintf(output, "  Nav Integrity     : %s\n", err)
	} else {
		fprintf(output, "  Nav Integrity     : %d\n", nic)
	}
}

func (f *Frame) showFlightStatus(output io.Writer) {
	fprintf(output, "FS: flight status   : (%d) %s\n", f.fs, flightStatusTable[f.fs])
	if f.special != "" {
		fprintf(output, "FS: special status  : %s\n", f.special)
	}
	f.showAlert(output)
	f.showVerticalStatus(output)
}

//
// func (f *Frame) showFlightId(output io.Writer) {
//	fprintf(output, "flight          : %s", f.flight())
//	fprintln(output, "")
// }

func (f *Frame) showICAO(output io.Writer) {
	fprintf(output, "AA: ICAO            : %6X", f.icao)
	s, err := f.DecodeAuIcaoRegistration()
	if nil == err {
		fprintf(output, "Registration        : %s", *s)
	}
	fprintln(output, "")
}

func (f *Frame) showCapability(output io.Writer) {
	fprintf(output, "CA: Plane Mode S Cap: (%d) %s\n", f.ca, capabilityTable[f.ca])
	f.showVerticalStatus(output)
}

func (f *Frame) showIdentity(output io.Writer) {
	fprintf(output, "ID: squawk Identity : %04d\n", f.identity)
}

func (f *Frame) showDownLinkRequest(output io.Writer) {
	fprintf(output, "DR: Downlink Request: (%d) %s\n", f.dr, downlinkRequestField[f.dr])
}

// showUtilityMessage displays our utilty message in all of its glory
// Utility message (UM): 6 bits, contains transponder communication status information.
//
//	IIS: The first 4 bits of UM indicate the interrogator identifier code.
//
//	IDS: The last 2 bits indicate the type of reservation made by the interrogator.
//
//	    00: no information
//	    01: IIS contains Comm-B interrogator identifier code
//	    10: IIS contains Comm-C interrogator identifier code
//	    11: IIS contains Comm-D interrogator identifier code
func (f *Frame) showUtilityMessage(output io.Writer) {
	iis := f.um >> 2
	ids := f.um & 0x3

	fprintf(output, "UM: Utility Request : IIS=%04b - IDS=%02b (%s)\n", iis, ids, utilityMessageField[ids])
}

func (f *Frame) showHae(output io.Writer) {
	if f.validHae {
		fprintf(output, "  HAE Delta         : %d (Height Above Ellipsoid)\n", f.haeDelta)
	} else {
		fprintln(output, "  HAE Delta         : Unavailable")

	}
}
func (f *Frame) showVelocity(output io.Writer) {
	if f.superSonic {
		fprintln(output, "  Super Sonic?      : Yes!")
	} else {
		fprintln(output, "  Super Sonic?      : No")
	}
	if f.validVelocity {
		fprintf(output, "  velocity          : %0.2f\n", f.velocity)
		fprintf(output, "  EW/NS VEL         : (East/west: %d) (North/South: %d)\n", f.eastWestVelocity, f.northSouthVelocity)
	} else {
		fprintln(output, "  velocity          : Invalid")
	}
}

func (f *Frame) showHeading(output io.Writer) {
	if f.validHeading {
		fprintf(output, "  heading           : %0.2f\n", f.heading)
	} else {
		fprintln(output, "  heading           : Not Valid")
		fprintln(output, "")

	}
}

func (f *Frame) showIntentChange(output io.Writer) {
	fprintf(output, "  Intent Change     : %t\n", f.intentChange != 0)
}
func (f *Frame) showInstrumentFlightRulesCapability(output io.Writer) {
	fprintf(output, "  IFR Capable       : %t\n", f.ifrCapability != 0)
}

func (f *Frame) showNavAccuracyCat(output io.Writer) {
	if f.validNacV {
		fprintf(output, "  Nav Accuracy Cat  : %d\n", f.nacV)
	}
}

func (f *Frame) showCprLatLon(output io.Writer) {
	fprintln(output, "Before Decoding : Half of vehicle location")
	var oddEven = "Odd"
	if f.IsEven() {
		oddEven = "Even"
	}
	fprintf(output, "  UTC Sync?     : %t\n", f.timeFlag != 0)
	fprintf(output, "  CPR Frame     : %s\n", oddEven)
	fprintf(output, "  CPR latitude  : %d\n", f.rawLatitude)
	fprintf(output, "  CPR longitude : %d\n", f.rawLongitude)
	fprintln(output, "")
}

func (f *Frame) showReplyInformation(output io.Writer) {
	fprintf(output, "RI: TCAS            : (%d) %s\n", f.ri, replyInformationField[f.ri])
}
func (f *Frame) showSensitivityLevel(output io.Writer) {
	fprintf(output, "SL: TCAS            : (%d) %s\n", f.sl, sensitivityLevelInformationField[f.sl])
}

func (f *Frame) showCategory(output io.Writer) {
	if f.ValidCategory() {
		fprintf(output, "CAT: Aircraft Cat   : (%d:%d) %s\n", f.messageType, f.catSubType, f.Category())
	}
}

func (f *Frame) showAdsb(output io.Writer) {
	fprintf(output, "ME : ADSB Msg Type  : (%d) %s\n", f.messageType, f.MessageTypeString())

	switch f.messageType {
	case 1, 2, 3, 4:
		f.showCategory(output)
		f.showFlightNumber(output)
		f.showWakeVortex(output)
	case 5, 6, 7, 8:
		f.showVelocity(output)
		f.showHeading(output)
		f.showCprLatLon(output)
	case 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 20, 21, 22:
		// 20-22 is GNSS altitude, 9-18 is barometric
		f.showContainmentRadius(output)
		f.showSurveilanceStatus(output)
		f.showNavigationIntegrity(output)
		f.showAltitude(output)
		f.showCprLatLon(output)
	case 19:
		f.showAdsbMsgSubType(output)
		switch f.messageSubType {
		case 1, 2, 3, 4:
			f.showIntentChange(output)
			f.showInstrumentFlightRulesCapability(output)
			f.showNavAccuracyCat(output)
			f.showHeading(output)
			f.showVelocity(output)
			f.showVerticalRate(output)
		default:
			// unknown sub type
		}
		f.showHae(output)
	case 23:
		f.showAdsbMsgSubType(output)
		if f.messageSubType == 7 {
			f.showIdentity(output)
		}
	case 28:
		f.showAdsbMsgSubType(output)
		if f.messageSubType == 1 {
			f.showIdentity(output)
			f.showAlert(output)
		} else if 2 == f.messageSubType {
			// TCAS RA
		}
	case 29:
		f.showAdsbMsgSubType(output)
		f.showAlert(output)

	case 31:
		f.showAdsbMsgSubType(output)
		f.showCapabilityClassInfo(output)
		f.showVerticalStatus(output)
		f.showOperationalModeInfo(output)
		f.showAircraftLengthWidth(output)
		f.showAdsbVersion(output)
		f.showNavAccuracyCat(output)
		f.showCrossCheck(output)
		f.showCompassNorth(output)
	default:
		fprintln(output, "Packet Type Not Yet Decoded")
	}

	fprintln(output, "")
}

func (f *Frame) showAdsbMsgSubType(output io.Writer) {
	fprintf(output, "SUB:      Sub Type  : %d \n", f.messageSubType)
}

func (f *Frame) showCapabilityClassInfo(output io.Writer) {
	if f.validCompatibilityClass {
		if nil != f.cccHasLowTxPower {
			fprintf(output, "  Low TX Power      : %t\n", *f.cccHasLowTxPower)
		}
	} else {
		fprintf(output, "Compatibility Class : Unknown\n")
	}
}
func (f *Frame) showOperationalModeInfo(output io.Writer) {

}
func (f *Frame) showAircraftLengthWidth(output io.Writer) {
	length, width, err := f.GetAirplaneLengthWidth()
	if nil == err {
		fprintf(output, "    Airframe Size   : width:%0.1f length:%0.1f metres\n", *width, *length)
	}
}
func (f *Frame) showCrossCheck(output io.Writer) {
	if f.messageSubType == 0 {
		fprintf(output, "NIC Baro CrossCheck : %t\n", f.nicCrossCheck == 1)
	} else if f.messageSubType == 1 {
		fprintf(output, "NIC Track CrossCheck: %t\n", f.nicCrossCheck == 1)
	}
}
func (f *Frame) showCompassNorth(output io.Writer) {
	if f.northReference != 0 {
		fprintf(output, "  Compass heading   : Magnetic North\n")
	} else {
		fprintf(output, "  Compass heading   : True North\n")
	}
}

func (f *Frame) showAlert(output io.Writer) {
	if f.alert {
		fprintf(output, "Plane showing Alert!\n")
	}
	f.showSpecial(output)
}
func (f *Frame) showSpecial(output io.Writer) {
	if f.special != "" {
		fprintf(output, "  special           : %s\n", f.special)
	}
}

func (f *Frame) showFlightNumber(output io.Writer) {
	fprintf(output, "    flight Number   : %s\n", f.FlightNumber())
}

// DownLinkFormat determines what type of mode S Message this frame is
func (f *Frame) DownLinkFormat() string {
	if description, ok := downlinkFormatTable[f.downLinkFormat]; ok {
		return description
	}
	return "Unknown Downlink Format"
}

func (f *Frame) showAdsbVersion(output io.Writer) {
	fprintf(output, "    ADS-B Version   : (%d) %s\n", f.adsbVersion, adsbCompatibilityVersion[f.adsbVersion])
}

func (f *Frame) showBdsData(output io.Writer) {
	fprintln(output, "BDS Info")
	fprintf(output, "  BDS Msg       : %s\n", f.DescribeBds())
}

func (f *Frame) showBitString(output io.Writer) {
	if features, ok := frameFeatures[f.downLinkFormat]; ok {
		fprintln(output, f.formatBitString(features))
	}
}

func (f *Frame) formatBitString(features []featureBreakdown) string {
	var header, separator, bits, rawBits, bitFmt, bitDesc, footer string
	var padLen, realLen, shownBitCount, i int

	for _, i := range f.message {
		rawBits += fmt.Sprintf("%08s", strconv.FormatUint(uint64(i), 2))
	}

	doMakeBitString := func(f featureBreakdown) {
		padLen = len(f.name)
		realLen = f.end - f.start
		if realLen > padLen {
			padLen = realLen
		}
		shownBitCount += f.end - f.start
		bitFmt = fmt.Sprintf(" %%- %ds |", padLen)
		header += fmt.Sprintf(bitFmt, f.name)
		separator += strings.Repeat("-", padLen+2) + "+"
		bits += " "
		for i = f.start; i < f.end; i++ {
			if i%8 == 0 {
				bits += "<span class='byte-start'>" + string(rawBits[i]) + "</span>"
			} else {
				bits += string(rawBits[i])
			}
		}
		bits += strings.Repeat(" ", padLen-(f.end-f.start)+1) + "|"
		bitDesc += fmt.Sprintf(bitFmt, strconv.Itoa(f.start))
	}

	doMakeFooterString := func(f featureBreakdown, indent string) {
		var feature featureDescriptionType
		var fieldBitLength = f.end - f.start
		var suffix string
		if fieldBitLength == 1 {
			suffix = ""
		} else {
			suffix = "s"
		}

		if f.longName != "" {
			feature.field = f.name
			feature.meaning = f.longName
		} else {
			feature = featureDescription[f.name]
		}
		footer += fmt.Sprintf(" %s%-10s (%2d-%2d) %2d bit%s\t %s: %s\n", indent, f.name, f.start, f.end-1, fieldBitLength, suffix, feature.field, feature.meaning)
	}

	var feature featureDescriptionType

	var fieldBitCounter, subFieldBitCounter, subSubFieldBitCounter int
	for _, feat := range features {
		var sk string

		// determine any specified sub feature we need to recurse down into
		switch f.downLinkFormat {
		case 17:
			sk = strconv.Itoa(int(f.messageType))
		case 20, 21:
			sk = f.BdsMessageType()
		}

		if fieldBitCounter != feat.start {
			log.Warn().
				Str("frame", f.RawString()).
				Msgf("Describe: Top Level Fields Not Adding up. (%d %s %d). Expected Start=%d, got=%d", f.downLinkFormat, sk, f.messageSubType, feat.start, fieldBitCounter)
		}
		fieldBitCounter = feat.end

		if len(feat.subFields[sk]) == 0 {
			// this field does not have any sub field features
			doMakeBitString(feat)
			doMakeFooterString(feat, "")
		} else {
			// this field has an array of fields that make up its important properties
			doMakeFooterString(feat, "")

			subFieldBitCounter = feat.start
			if feat.longName != "" {
				feature.field = feat.name
				feature.meaning = feat.longName
			} else {
				feature = featureDescription[feat.name]
			}

			// footer += fmt.Sprintf("-- Field=%s -- SubFields -- %s: %s \n", feat.name, feature.field, feature.meaning)
			ssk := strconv.Itoa(int(f.messageSubType))
			for _, sf := range feat.subFields[sk] {
				if subFieldBitCounter != sf.start {
					log.Warn().
						Str("frame", f.RawString()).
						Msgf("Describe: Second Level Fields Not Adding up. (%d %s %d). Expected Start=%d, got=%d", f.downLinkFormat, sk, f.messageSubType, sf.start, subFieldBitCounter)
				}
				subFieldBitCounter = sf.end
				if len(sf.subFields[ssk]) == 0 {
					doMakeBitString(sf)
					doMakeFooterString(sf, " -> ")

				} else {
					feature = featureDescription[sf.name]
					subSubFieldBitCounter = sf.start
					for _, ssf := range sf.subFields[ssk] {
						if subSubFieldBitCounter != ssf.start {
							log.Warn().
								Str("frame", f.RawString()).
								Msgf("Describe: Third Level Fields Not Adding up. (%d %s %d). Expected Start=%d, got=%d", f.downLinkFormat, sk, f.messageSubType, ssf.start, subSubFieldBitCounter)
						}
						subSubFieldBitCounter = ssf.end
						doMakeBitString(ssf)
						doMakeFooterString(ssf, "   -> ")
					}
				}
			}
			if subFieldBitCounter != feat.end {
				println("The end of the main field did not align with sub field endings", subFieldBitCounter, feat.end)
			}
			footer += "---------------\n"
		}
	}

	return fmt.Sprintf("\n%s\n%s\n%s\n%s\n%s\n\n%s\n%d/%d bits shown\n", header, separator, bits, separator, bitDesc, footer, shownBitCount, f.getMessageLengthBits())
}

func fprintf(output io.Writer, line string, params ...interface{}) {
	line = strings.TrimRight(line, "\n") + "\n"
	// if "\n" != line[len(line)-1:1] {
	//	line += "\n"
	// }
	_, _ = fmt.Fprintf(output, line, params...)
}

func fprintln(output io.Writer, params ...interface{}) {
	_, _ = fmt.Fprintln(output, params...)
}
