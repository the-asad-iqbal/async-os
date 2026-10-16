package modbus

// crc16 computes the Modbus RTU CRC (polynomial 0xA001, initial 0xFFFF).
func crc16(data []byte) uint16 {
	crc := uint16(0xFFFF)
	for _, b := range data {
		crc ^= uint16(b)
		for range 8 {
			if crc&1 != 0 {
				crc = crc>>1 ^ 0xA001
			} else {
				crc >>= 1
			}
		}
	}
	return crc
}

// appendCRC appends the CRC, low byte first as RTU requires.
func appendCRC(frame []byte) []byte {
	crc := crc16(frame)
	return append(frame, byte(crc), byte(crc>>8))
}

func validCRC(frame []byte) bool {
	if len(frame) < 4 {
		return false
	}
	n := len(frame) - 2
	crc := crc16(frame[:n])
	return frame[n] == byte(crc) && frame[n+1] == byte(crc>>8)
}
