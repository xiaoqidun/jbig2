// Copyright 2026 肖其顿 (XIAO QI DUN)
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package jbig2

// arithEncoder 算术编码寄存器与输出缓冲
type arithEncoder struct {
	a       uint32
	c       uint32
	ct      uint
	b       byte
	started bool
	data    []byte
	limit   int
	err     error
}

// newArithEncoder 创建算术编码器
// 入参: limit 最大输出字节数
// 返回: *arithEncoder 算术编码器
func newArithEncoder(limit int) *arithEncoder {
	return &arithEncoder{a: defaultAValue, ct: 12, limit: limit}
}

// tryEncodeFast 尝试无需重归一化的MPS编码
// 入参: cx 上下文, bit 像素位
// 返回: bool 是否完成编码
func (e *arithEncoder) tryEncodeFast(cx *ArithCtx, bit uint8) bool {
	if bit != cx.state&1 {
		return false
	}
	qe := arithDecodeStates[cx.state].qe
	a := e.a - qe
	if a&defaultAValue == 0 {
		return false
	}
	e.a = a
	e.c += qe
	return true
}

// encode 编码一个二进制符号并更新上下文
// 入参: cx 上下文, bit 像素位
func (e *arithEncoder) encode(cx *ArithCtx, bit uint8) {
	state := arithDecodeStates[cx.state]
	e.a -= state.qe
	if bit == cx.state&1 {
		if e.a&defaultAValue != 0 {
			e.c += state.qe
			return
		}
		if e.a < state.qe {
			e.a = state.qe
		} else {
			e.c += state.qe
		}
		cx.state = state.nmps
	} else {
		if e.a < state.qe {
			e.c += state.qe
		} else {
			e.a = state.qe
		}
		cx.state = state.nlps
	}
	for e.a&defaultAValue == 0 {
		e.a <<= 1
		e.c <<= 1
		e.ct--
		if e.ct == 0 {
			e.byteOut()
		}
	}
}

// byteOut 写出码字节并处理进位与位填充
func (e *arithEncoder) byteOut() {
	if e.b != 0xff && e.c&0x08000000 != 0 {
		e.b++
		e.c &= 0x07ffffff
	}
	if e.started {
		e.appendByte(e.b)
	}
	e.started = true
	if e.b == 0xff {
		e.b = byte(e.c >> 20)
		e.c &= 0x000fffff
		e.ct = 7
	} else {
		e.b = byte(e.c >> 19)
		e.c &= 0x0007ffff
		e.ct = 8
	}
}

// appendByte 在输出长度限制内追加一个字节
// 入参: value 字节值
func (e *arithEncoder) appendByte(value byte) {
	if e.err != nil {
		return
	}
	if len(e.data) >= e.limit {
		e.err = ErrLimitExceeded
		return
	}
	if len(e.data) == cap(e.data) {
		size := cap(e.data) + min(max(256, cap(e.data)), e.limit-cap(e.data))
		data := make([]byte, len(e.data), size)
		copy(data, e.data)
		e.data = data
	}
	e.data = append(e.data, value)
}

// finish 完成算术编码并写入终止标记
// 返回: []byte 编码数据, error 错误信息
func (e *arithEncoder) finish() ([]byte, error) {
	end := e.c + e.a
	e.c |= 0xffff
	if e.c >= end {
		e.c -= 0x8000
	}
	e.c <<= e.ct
	e.byteOut()
	e.c <<= e.ct
	e.byteOut()
	e.appendByte(e.b)
	if e.b != 0xff {
		e.appendByte(0xff)
	}
	e.appendByte(0xac)
	if e.err != nil {
		return nil, e.err
	}
	return e.data, nil
}
