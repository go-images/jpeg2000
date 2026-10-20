package jpeg2000

import (
	"encoding/binary"
	"fmt"
)

// checkSegment validates a marker segment's length field BEFORE any parser
// reads the segment. pos is just past the marker. It returns the segment
// length (Lxxx, which counts its own two bytes).
//
// Each parser used to read Lxxx itself and derive counts from it -- such as
// numBands := segLen - 3 -- without a minimum, so a QCD of length 2 asked
// make() for -1 elements (found by FuzzDecode). Checking once, here, means a
// parser may assume its segment is whole and at least as long as the
// standard allows.
//
// The minimums are OpenJPEG's (opj_j2k_read_* in j2k.c, whose p_header_size
// excludes the two length bytes, so each is that figure plus 2). T.800 itself
// is not freely available to cite. compRoom is the width of a component index:
// one byte for up to 256 components, two above.
func checkSegment(data []byte, pos int, marker uint16, numComps int) (int, error) {
	if pos+2 > len(data) {
		return 0, ErrTruncatedData
	}
	segLen := int(binary.BigEndian.Uint16(data[pos : pos+2]))
	if pos+segLen > len(data) {
		return 0, ErrTruncatedData
	}
	compRoom := 1
	if numComps > 256 {
		compRoom = 2
	}
	min, exact := 2, 0
	switch marker {
	case markerSIZ:
		min = 41 // 38 + 3 for one component; parseSIZ checks the exact length
	case markerCOD:
		min = 12 // Scod and SGcod (5, opj_j2k_read_cod), then SPcod (5, opj_j2k_read_SPCod_SPCoc)
	case markerCOC:
		min = 8 + compRoom
	case markerQCD:
		min = 3
	case markerQCC:
		min = 3 + compRoom
	case markerRGN:
		exact = 4 + compRoom
	case markerPOC:
		chunk := 5 + 2*compRoom
		if segLen < 2+chunk || (segLen-2)%chunk != 0 {
			return 0, fmt.Errorf("%w: POC length %d is not a whole number of %d-byte entries", ErrInvalidHeader, segLen, chunk)
		}
	case markerTLM, markerPPM, markerPPT:
		min = 4
	case markerPLM, markerPLT:
		min = 3
	case markerSOT:
		exact = 10
	}
	if exact != 0 && segLen != exact {
		return 0, fmt.Errorf("%w: marker %#04x has length %d, want %d", ErrInvalidHeader, marker, segLen, exact)
	}
	if segLen < min {
		return 0, fmt.Errorf("%w: marker %#04x has length %d, at least %d is needed", ErrInvalidHeader, marker, segLen, min)
	}
	return segLen, nil
}

// checkSPcod validates the coding parameters a COD or COC segment carries.
// sp starts at the decomposition-level byte and runs to the segment's end;
// checkSegment has already made sure it holds at least the five fixed fields.
// customPrecincts is bit 0 of Scod/Scoc.
//
// The rules are OpenJPEG's opj_j2k_read_SPCod_SPCoc. Without them a COD could
// ask for a code block of 2^50 by 2^50 samples, and the tile decoder sized a
// slice from it (found by FuzzDecode).
func checkSPcod(sp []byte, customPrecincts bool) error {
	levels := int(sp[0])
	if levels+1 > 33 { // OPJ_J2K_MAXRLVLS
		return fmt.Errorf("%w: %d decomposition levels", ErrInvalidHeader, levels)
	}
	xcb, ycb := int(sp[1])+2, int(sp[2])+2
	if xcb > 10 || ycb > 10 || xcb+ycb > 12 {
		return fmt.Errorf("%w: code-block exponents %d x %d (at most 10 each, 12 together)", ErrInvalidHeader, xcb, ycb)
	}
	if sp[4] > 1 {
		return fmt.Errorf("%w: wavelet type %d", ErrUnsupportedWavelet, sp[4])
	}
	if customPrecincts {
		if len(sp) < 5+levels+1 {
			return fmt.Errorf("%w: segment too short for %d precinct sizes", ErrTruncatedData, levels+1)
		}
		// T.800 Table A.21, as OpenJPEG reads it: an exponent of zero is
		// allowed only at the lowest resolution.
		for r := 1; r <= levels; r++ {
			if b := sp[5+r]; b&0x0F == 0 || b>>4 == 0 {
				return fmt.Errorf("%w: zero precinct exponent at resolution %d", ErrInvalidHeader, r)
			}
		}
	}
	return nil
}

// hasSegment reports whether a marker is followed by a length field.
// SOC, SOD, EOC, EPH and the reserved 0xFF30-0xFF3F carry none.
func hasSegment(marker uint16) bool {
	switch marker {
	case markerSOC, markerSOD, markerEOC, markerEPH:
		return false
	}
	return marker < 0xFF30 || marker > 0xFF3F
}
