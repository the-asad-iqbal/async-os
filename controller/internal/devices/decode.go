package devices

import (
	"fmt"
	"math"
)

// decode turns raw registers into an engineering value. sf is the SunSpec
// style power-of-ten exponent (0 when unused).
func decode(r Register, raw []uint16, sf int16) (value float64, ok bool, err error) {
	n := r.Type.Registers()
	if len(raw) != n {
		return 0, false, fmt.Errorf("decode %s: got %d registers, want %d", r.Type, len(raw), n)
	}
	words := raw
	if r.WordOrder == "low_first" && n > 1 {
		words = make([]uint16, n)
		for i := range raw {
			words[i] = raw[n-1-i]
		}
	}
	var bits uint64
	for _, w := range words {
		bits = bits<<16 | uint64(w)
	}

	var v float64
	switch r.Type {
	case TypeUint16, TypeUint32, TypeUint64:
		v = float64(bits)
	case TypeInt16:
		v = float64(int16(bits))
	case TypeInt32:
		v = float64(int32(bits))
	case TypeInt64:
		v = float64(int64(bits))
	case TypeFloat32:
		f := math.Float32frombits(uint32(bits))
		if math.IsNaN(float64(f)) || math.IsInf(float64(f), 0) {
			return 0, false, nil
		}
		v = float64(f)
	}
	for _, u := range r.Unavailable {
		if v == u {
			return 0, false, nil
		}
	}

	scale := r.Scale
	if scale == 0 {
		scale = 1
	}
	return v * scale * math.Pow10(int(sf)), true, nil
}

// encode turns an engineering value into raw registers for a write.
func encode(r Register, value float64, sf int16) ([]uint16, error) {
	scale := r.Scale
	if scale == 0 {
		scale = 1
	}
	raw := math.Round(value / scale / math.Pow10(int(sf)))

	var bits uint64
	switch r.Type {
	case TypeUint16:
		if raw < 0 || raw > math.MaxUint16 {
			return nil, fmt.Errorf("value %g out of range for uint16", raw)
		}
		bits = uint64(raw)
	case TypeInt16:
		if raw < math.MinInt16 || raw > math.MaxInt16 {
			return nil, fmt.Errorf("value %g out of range for int16", raw)
		}
		bits = uint64(uint16(int16(raw)))
	case TypeUint32:
		if raw < 0 || raw > math.MaxUint32 {
			return nil, fmt.Errorf("value %g out of range for uint32", raw)
		}
		bits = uint64(raw)
	case TypeInt32:
		if raw < math.MinInt32 || raw > math.MaxInt32 {
			return nil, fmt.Errorf("value %g out of range for int32", raw)
		}
		bits = uint64(uint32(int32(raw)))
	case TypeFloat32:
		bits = uint64(math.Float32bits(float32(value / scale / math.Pow10(int(sf)))))
	default:
		return nil, fmt.Errorf("type %s is not writable", r.Type)
	}

	n := r.Type.Registers()
	words := make([]uint16, n)
	for i := n - 1; i >= 0; i-- {
		words[i] = uint16(bits)
		bits >>= 16
	}
	if r.WordOrder == "low_first" {
		for i, j := 0, n-1; i < j; i, j = i+1, j-1 {
			words[i], words[j] = words[j], words[i]
		}
	}
	return words, nil
}
