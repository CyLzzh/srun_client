package encryptlib

func XxteaEncrypt(msg, key string) []byte {
	if len(msg) == 0 {
		return []byte{}
	}

	// 1. 字符串转 uint32 切片 (对应原代码 s(msg, true))
	msgLen := len(msg)
	vLen := (msgLen + 3) / 4
	v := make([]uint32, vLen+1) // +1 用于存长度

	for i := 0; i < msgLen; i++ {
		v[i/4] |= uint32(msg[i]) << ((i % 4) * 8)
	}
	v[vLen] = uint32(msgLen) // 末尾存长度

	// 2. 处理 Key (对应原代码 s(key, false))
	var k [4]uint32
	keyLen := len(key)
	for i := 0; i < keyLen && i < 16; i++ {
		k[i/4] |= uint32(key[i]) << ((i % 4) * 8)
	}

	// 3. 加密循环
	n := len(v) - 1
	z := v[n]
	sum := uint32(0)      // 对应原代码中的 d
	delta := uint32(0x9e3779b9) // 对应原代码中的 c
	
	q := 6 + 52/len(v)

	for q > 0 {
		sum += delta
		e := (sum >> 2) & 3

		// p 从 0 到 n-1
		for p := 0; p < n; p++ {
			y := v[p+1]
			// 【关键修正】这里严格还原了你的非标准公式：
			// 原版: (z>>5 ^ y<<2) + (y>>3 ^ z<<4 ^ (d ^ y)) + (k[p&3^e] ^ z)
			m := (z>>5 ^ y<<2) + (y>>3 ^ z<<4 ^ (sum ^ y)) + (k[(p&3)^int(e)] ^ z)
			v[p] += m
			z = v[p]
		}

		// 边界处理 p == n
		y := v[0]
		// 【关键修正】同上，还原非标准公式
		m := (z>>5 ^ y<<2) + (y>>3 ^ z<<4 ^ (sum ^ y)) + (k[(n&3)^int(e)] ^ z)
		v[n] += m
		z = v[n]

		q--
	}

	// 4. 转换回 []byte
	result := make([]byte, len(v)*4)
	for i, val := range v {
		result[i*4] = byte(val)
		result[i*4+1] = byte(val >> 8)
		result[i*4+2] = byte(val >> 16)
		result[i*4+3] = byte(val >> 24)
	}

	return result
}