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

import (
	"errors"
)

// GRRDProc 通用细化区域解码过程
type GRRDProc struct {
	GRTEMPLATE    bool
	TPGRON        bool
	GRW           uint32
	GRH           uint32
	GRREFERENCEDX int32
	GRREFERENCEDY int32
	GRREFERENCE   *Image
	GRAT          [4]int8
}

// NewGRRDProc 创建通用细化区域解码过程对象
// 返回: *GRRDProc 对象
func NewGRRDProc() *GRRDProc {
	return &GRRDProc{}
}

// Decode 解码
// 入参: arithDecoder 算术解码器, grContexts 上下文
// 返回: *Image 图像, error 错误信息
func (g *GRRDProc) Decode(arithDecoder *ArithDecoder, grContexts []ArithCtx) (*Image, error) {
	if g.GRW > JBig2MaxImageSize || g.GRH > JBig2MaxImageSize {
		return nil, errors.New("image size too large")
	}
	if g.GRREFERENCE == nil {
		return nil, errors.New("reference image is nil")
	}
	if !g.GRTEMPLATE {
		if g.GRAT[0] == -1 && g.GRAT[1] == -1 && g.GRAT[2] == -1 && g.GRAT[3] == -1 {
			return g.decodeTemplate0Opt(arithDecoder, grContexts)
		}
		return g.decodeTemplate0Unopt(arithDecoder, grContexts)
	}
	return g.decodeTemplate1Opt(arithDecoder, grContexts)
}

// decodeTemplate0Opt 模板0优化解码
// 入参: decoder 算术解码器, contexts 上下文
// 返回: *Image 图像, error 错误信息
func (g *GRRDProc) decodeTemplate0Opt(decoder *ArithDecoder, contexts []ArithCtx) (*Image, error) {
	grReg := NewImage(int32(g.GRW), int32(g.GRH))
	if grReg == nil {
		return nil, errors.New("failed to create image")
	}
	ltp := 0
	var lines [5]uint32
	width := int32(g.GRW)
	referenceWidth := g.GRREFERENCE.width
	referenceX := -g.GRREFERENCEDX
	for h := int32(0); h < int32(g.GRH); h++ {
		if g.TPGRON {
			if decoder.IsComplete() {
				return nil, errors.New("decoder complete prematurely")
			}
			if decoder.Decode(&contexts[0x0010]) != 0 {
				ltp ^= 1
			}
		}
		row := grReg.row(h)
		previousRow := grReg.row(h - 1)
		refY := h - g.GRREFERENCEDY
		refPreviousRow := g.GRREFERENCE.row(refY - 1)
		refRow := g.GRREFERENCE.row(refY)
		refNextRow := g.GRREFERENCE.row(refY + 1)
		lines[0] = getPixelFromRow(previousRow, 1, width)
		lines[0] |= getPixelFromRow(previousRow, 0, width) << 1
		lines[1] = 0
		lines[2] = getPixelFromRow(refPreviousRow, referenceX+1, referenceWidth)
		lines[2] |= getPixelFromRow(refPreviousRow, referenceX, referenceWidth) << 1
		lines[3] = getPixelFromRow(refRow, referenceX+1, referenceWidth)
		lines[3] |= getPixelFromRow(refRow, referenceX, referenceWidth) << 1
		lines[3] |= getPixelFromRow(refRow, referenceX-1, referenceWidth) << 2
		lines[4] = getPixelFromRow(refNextRow, referenceX+1, referenceWidth)
		lines[4] |= getPixelFromRow(refNextRow, referenceX, referenceWidth) << 1
		lines[4] |= getPixelFromRow(refNextRow, referenceX-1, referenceWidth) << 2
		for w := int32(0); w < width; w++ {
			bVal := 0
			needDecode := ltp == 0
			if ltp != 0 {
				value, predictable := typicalPixelFromRows(refPreviousRow, refRow, refNextRow, referenceX+w, referenceWidth)
				bVal = int(value)
				needDecode = !predictable
			}
			if needDecode {
				context := lines[4]
				context |= lines[3] << 3
				context |= lines[2] << 6
				context |= getPixelFromRow(refPreviousRow, referenceX+w-1, referenceWidth) << 8
				context |= lines[1] << 9
				context |= lines[0] << 10
				context |= getPixelFromRow(previousRow, w-1, width) << 12
				if decoder.IsComplete() {
					return nil, errors.New("decoder complete prematurely")
				}
				bVal = decoder.Decode(&contexts[context])
			}
			if bVal != 0 {
				setPixelInRow(row, w)
			}
			lines[0] = ((lines[0] << 1) | getPixelFromRow(previousRow, w+2, width)) & 0x03
			lines[1] = ((lines[1] << 1) | uint32(bVal)) & 0x01
			lines[2] = ((lines[2] << 1) | getPixelFromRow(refPreviousRow, referenceX+w+2, referenceWidth)) & 0x03
			lines[3] = ((lines[3] << 1) | getPixelFromRow(refRow, referenceX+w+2, referenceWidth)) & 0x07
			lines[4] = ((lines[4] << 1) | getPixelFromRow(refNextRow, referenceX+w+2, referenceWidth)) & 0x07
		}
	}
	return grReg, nil
}

// decodeTemplate1Opt 模板1优化解码
// 入参: decoder 算术解码器, contexts 上下文
// 返回: *Image 图像, error 错误信息
func (g *GRRDProc) decodeTemplate1Opt(decoder *ArithDecoder, contexts []ArithCtx) (*Image, error) {
	grReg := NewImage(int32(g.GRW), int32(g.GRH))
	if grReg == nil {
		return nil, errors.New("failed to create image")
	}
	ltp := 0
	width := int32(g.GRW)
	referenceWidth := g.GRREFERENCE.width
	referenceX := -g.GRREFERENCEDX
	for h := int32(0); h < int32(g.GRH); h++ {
		if g.TPGRON {
			if decoder.IsComplete() {
				return nil, errors.New("decoder complete prematurely")
			}
			if decoder.Decode(&contexts[0x0008]) != 0 {
				ltp ^= 1
			}
		}
		row := grReg.row(h)
		previousRow := grReg.row(h - 1)
		refY := h - g.GRREFERENCEDY
		refPreviousRow := g.GRREFERENCE.row(refY - 1)
		refRow := g.GRREFERENCE.row(refY)
		refNextRow := g.GRREFERENCE.row(refY + 1)
		line1 := getPixelFromRow(previousRow, 1, width)
		line1 |= getPixelFromRow(previousRow, 0, width) << 1
		line1 |= getPixelFromRow(previousRow, -1, width) << 2
		var line2 uint32
		line3 := getPixelFromRow(refPreviousRow, referenceX, referenceWidth)
		line4 := getPixelFromRow(refRow, referenceX+1, referenceWidth)
		line4 |= getPixelFromRow(refRow, referenceX, referenceWidth) << 1
		line4 |= getPixelFromRow(refRow, referenceX-1, referenceWidth) << 2
		line5 := getPixelFromRow(refNextRow, referenceX+1, referenceWidth)
		line5 |= getPixelFromRow(refNextRow, referenceX, referenceWidth) << 1
		for w := int32(0); w < width; w++ {
			bVal := 0
			needDecode := ltp == 0
			if ltp != 0 {
				value, predictable := typicalPixelFromRows(refPreviousRow, refRow, refNextRow, referenceX+w, referenceWidth)
				bVal = int(value)
				needDecode = !predictable
			}
			if needDecode {
				context := line5
				context |= line4 << 2
				context |= line3 << 5
				context |= line2 << 6
				context |= line1 << 7
				if decoder.IsComplete() {
					return nil, errors.New("decoder complete prematurely")
				}
				bVal = decoder.Decode(&contexts[context])
			}
			if bVal != 0 {
				setPixelInRow(row, w)
			}
			line1 = ((line1 << 1) | getPixelFromRow(previousRow, w+2, width)) & 0x07
			line2 = ((line2 << 1) | uint32(bVal)) & 0x01
			line3 = ((line3 << 1) | getPixelFromRow(refPreviousRow, referenceX+w+1, referenceWidth)) & 0x01
			line4 = ((line4 << 1) | getPixelFromRow(refRow, referenceX+w+2, referenceWidth)) & 0x07
			line5 = ((line5 << 1) | getPixelFromRow(refNextRow, referenceX+w+2, referenceWidth)) & 0x03
		}
	}
	return grReg, nil
}

// typicalPixelFromRows 获取典型预测像素
// 入参: previousRow 上一行, row 当前行, nextRow 下一行, x 横坐标, width 行宽度
// 返回: uint32 像素值, bool 是否可预测
func typicalPixelFromRows(previousRow, row, nextRow []byte, x, width int32) (uint32, bool) {
	value := getPixelFromRow(row, x, width)
	predictable := value == getPixelFromRow(previousRow, x-1, width) &&
		value == getPixelFromRow(previousRow, x, width) &&
		value == getPixelFromRow(previousRow, x+1, width) &&
		value == getPixelFromRow(row, x-1, width) &&
		value == getPixelFromRow(row, x+1, width) &&
		value == getPixelFromRow(nextRow, x-1, width) &&
		value == getPixelFromRow(nextRow, x, width) &&
		value == getPixelFromRow(nextRow, x+1, width)
	return value, predictable
}

// decodeTemplate0Unopt 模板0非优化解码
// 入参: decoder 算术解码器, contexts 上下文
// 返回: *Image 图像, error 错误信息
func (g *GRRDProc) decodeTemplate0Unopt(decoder *ArithDecoder, contexts []ArithCtx) (*Image, error) {
	grReg := NewImage(int32(g.GRW), int32(g.GRH))
	if grReg == nil {
		return nil, errors.New("failed to create image")
	}
	ltp := 0
	lines := make([]uint32, 5)
	for h := int32(0); h < int32(g.GRH); h++ {
		if g.TPGRON {
			if decoder.IsComplete() {
				return nil, errors.New("decoder complete prematurely")
			}
			bit := decoder.Decode(&contexts[0x0010])
			if bit != 0 {
				ltp ^= 1
			}
		}
		lines[0] = uint32(grReg.GetPixel(1, h-1))
		lines[0] |= uint32(grReg.GetPixel(0, h-1)) << 1
		lines[1] = 0
		lines[2] = uint32(g.GRREFERENCE.GetPixel(-g.GRREFERENCEDX+1, h-g.GRREFERENCEDY-1))
		lines[2] |= uint32(g.GRREFERENCE.GetPixel(-g.GRREFERENCEDX, h-g.GRREFERENCEDY-1)) << 1
		lines[3] = uint32(g.GRREFERENCE.GetPixel(-g.GRREFERENCEDX+1, h-g.GRREFERENCEDY))
		lines[3] |= uint32(g.GRREFERENCE.GetPixel(-g.GRREFERENCEDX, h-g.GRREFERENCEDY)) << 1
		lines[3] |= uint32(g.GRREFERENCE.GetPixel(-g.GRREFERENCEDX-1, h-g.GRREFERENCEDY)) << 2
		lines[4] = uint32(g.GRREFERENCE.GetPixel(-g.GRREFERENCEDX+1, h-g.GRREFERENCEDY+1))
		lines[4] |= uint32(g.GRREFERENCE.GetPixel(-g.GRREFERENCEDX, h-g.GRREFERENCEDY+1)) << 1
		lines[4] |= uint32(g.GRREFERENCE.GetPixel(-g.GRREFERENCEDX-1, h-g.GRREFERENCEDY+1)) << 2
		if ltp == 0 {
			for w := int32(0); w < int32(g.GRW); w++ {
				CONTEXT := g.calculateContext0(grReg, lines, w, h)
				if decoder.IsComplete() {
					return nil, errors.New("decoder complete prematurely")
				}
				bVal := decoder.Decode(&contexts[CONTEXT])
				g.setPixel0(grReg, lines, w, h, bVal)
			}
		} else {
			for w := int32(0); w < int32(g.GRW); w++ {
				bVal, predictable := g.typicalReferencePixel(w, h)
				needDecode := true
				if g.TPGRON && predictable {
					needDecode = false
				}
				if needDecode {
					CONTEXT := g.calculateContext0(grReg, lines, w, h)
					if decoder.IsComplete() {
						return nil, errors.New("decoder complete prematurely")
					}
					bVal = int(decoder.Decode(&contexts[CONTEXT]))
				}
				g.setPixel0(grReg, lines, w, h, int(bVal))
			}
		}
	}
	return grReg, nil
}

// calculateContext0 计算上下文0
// 入参: grReg 图像, lines 扫描线, w 宽度, h 高度
// 返回: uint32 上下文
func (g *GRRDProc) calculateContext0(grReg *Image, lines []uint32, w, h int32) uint32 {
	CONTEXT := lines[4]
	CONTEXT |= lines[3] << 3
	CONTEXT |= lines[2] << 6
	CONTEXT |= uint32(g.GRREFERENCE.GetPixel(w-g.GRREFERENCEDX+int32(g.GRAT[2]), h-g.GRREFERENCEDY+int32(g.GRAT[3]))) << 8
	CONTEXT |= lines[1] << 9
	CONTEXT |= lines[0] << 10
	CONTEXT |= uint32(grReg.GetPixel(w+int32(g.GRAT[0]), h+int32(g.GRAT[1]))) << 12
	return CONTEXT
}

// setPixel0 设置像素0
// 入参: grReg 图像, lines 扫描线, w 宽度, h 高度, bVal 像素值
func (g *GRRDProc) setPixel0(grReg *Image, lines []uint32, w, h int32, bVal int) {
	grReg.SetPixel(w, h, bVal)
	lines[0] = ((lines[0] << 1) | uint32(grReg.GetPixel(w+2, h-1))) & 0x03
	lines[1] = ((lines[1] << 1) | uint32(bVal)) & 0x01
	lines[2] = ((lines[2] << 1) | uint32(g.GRREFERENCE.GetPixel(w-g.GRREFERENCEDX+2, h-g.GRREFERENCEDY-1))) & 0x03
	lines[3] = ((lines[3] << 1) | uint32(g.GRREFERENCE.GetPixel(w-g.GRREFERENCEDX+2, h-g.GRREFERENCEDY))) & 0x07
	lines[4] = ((lines[4] << 1) | uint32(g.GRREFERENCE.GetPixel(w-g.GRREFERENCEDX+2, h-g.GRREFERENCEDY+1))) & 0x07
}

// decodeTemplate1Unopt 模板1非优化解码
// 入参: decoder 算术解码器, contexts 上下文
// 返回: *Image 图像, error 错误信息
func (g *GRRDProc) decodeTemplate1Unopt(decoder *ArithDecoder, contexts []ArithCtx) (*Image, error) {
	grReg := NewImage(int32(g.GRW), int32(g.GRH))
	if grReg == nil {
		return nil, errors.New("failed to create image")
	}
	ltp := 0
	for h := int32(0); h < int32(g.GRH); h++ {
		if g.TPGRON {
			if decoder.IsComplete() {
				return nil, errors.New("decoder complete prematurely")
			}
			bit := decoder.Decode(&contexts[0x0008])
			if bit != 0 {
				ltp ^= 1
			}
		}
		if ltp == 0 {
			line1 := uint32(grReg.GetPixel(1, h-1))
			line1 |= uint32(grReg.GetPixel(0, h-1)) << 1
			line1 |= uint32(grReg.GetPixel(-1, h-1)) << 2
			line2 := uint32(0)
			line3 := uint32(g.GRREFERENCE.GetPixel(-g.GRREFERENCEDX, h-g.GRREFERENCEDY-1))
			line4 := uint32(g.GRREFERENCE.GetPixel(-g.GRREFERENCEDX+1, h-g.GRREFERENCEDY))
			line4 |= uint32(g.GRREFERENCE.GetPixel(-g.GRREFERENCEDX, h-g.GRREFERENCEDY)) << 1
			line4 |= uint32(g.GRREFERENCE.GetPixel(-g.GRREFERENCEDX-1, h-g.GRREFERENCEDY)) << 2
			line5 := uint32(g.GRREFERENCE.GetPixel(-g.GRREFERENCEDX+1, h-g.GRREFERENCEDY+1))
			line5 |= uint32(g.GRREFERENCE.GetPixel(-g.GRREFERENCEDX, h-g.GRREFERENCEDY+1)) << 1
			for w := int32(0); w < int32(g.GRW); w++ {
				CONTEXT := line5
				CONTEXT |= line4 << 2
				CONTEXT |= line3 << 5
				CONTEXT |= line2 << 6
				CONTEXT |= line1 << 7
				if decoder.IsComplete() {
					return nil, errors.New("decoder complete prematurely")
				}
				bVal := decoder.Decode(&contexts[CONTEXT])
				grReg.SetPixel(w, h, bVal)
				line1 = ((line1 << 1) | uint32(grReg.GetPixel(w+2, h-1))) & 0x07
				line2 = ((line2 << 1) | uint32(bVal)) & 0x01
				line3 = ((line3 << 1) | uint32(g.GRREFERENCE.GetPixel(w-g.GRREFERENCEDX+1, h-g.GRREFERENCEDY-1))) & 0x01
				line4 = ((line4 << 1) | uint32(g.GRREFERENCE.GetPixel(w-g.GRREFERENCEDX+2, h-g.GRREFERENCEDY))) & 0x07
				line5 = ((line5 << 1) | uint32(g.GRREFERENCE.GetPixel(w-g.GRREFERENCEDX+2, h-g.GRREFERENCEDY+1))) & 0x03
			}
		} else {
			line1 := uint32(grReg.GetPixel(1, h-1))
			line1 |= uint32(grReg.GetPixel(0, h-1)) << 1
			line1 |= uint32(grReg.GetPixel(-1, h-1)) << 2
			line2 := uint32(0)
			line3 := uint32(g.GRREFERENCE.GetPixel(-g.GRREFERENCEDX, h-g.GRREFERENCEDY-1))
			line4 := uint32(g.GRREFERENCE.GetPixel(-g.GRREFERENCEDX+1, h-g.GRREFERENCEDY))
			line4 |= uint32(g.GRREFERENCE.GetPixel(-g.GRREFERENCEDX, h-g.GRREFERENCEDY)) << 1
			line4 |= uint32(g.GRREFERENCE.GetPixel(-g.GRREFERENCEDX-1, h-g.GRREFERENCEDY)) << 2
			line5 := uint32(g.GRREFERENCE.GetPixel(-g.GRREFERENCEDX+1, h-g.GRREFERENCEDY+1))
			line5 |= uint32(g.GRREFERENCE.GetPixel(-g.GRREFERENCEDX, h-g.GRREFERENCEDY+1)) << 1
			for w := int32(0); w < int32(g.GRW); w++ {
				bVal, predictable := g.typicalReferencePixel(w, h)
				needDecode := true
				if g.TPGRON && predictable {
					needDecode = false
				}
				if needDecode {
					CONTEXT := line5
					CONTEXT |= line4 << 2
					CONTEXT |= line3 << 5
					CONTEXT |= line2 << 6
					CONTEXT |= line1 << 7
					if decoder.IsComplete() {
						return nil, errors.New("decoder complete prematurely")
					}
					bVal = int(decoder.Decode(&contexts[CONTEXT]))
				}
				grReg.SetPixel(w, h, int(bVal))
				line1 = ((line1 << 1) | uint32(grReg.GetPixel(w+2, h-1))) & 0x07
				line2 = ((line2 << 1) | uint32(bVal)) & 0x01
				line3 = ((line3 << 1) | uint32(g.GRREFERENCE.GetPixel(w-g.GRREFERENCEDX+1, h-g.GRREFERENCEDY-1))) & 0x01
				line4 = ((line4 << 1) | uint32(g.GRREFERENCE.GetPixel(w-g.GRREFERENCEDX+2, h-g.GRREFERENCEDY))) & 0x07
				line5 = ((line5 << 1) | uint32(g.GRREFERENCE.GetPixel(w-g.GRREFERENCEDX+2, h-g.GRREFERENCEDY+1))) & 0x03
			}
		}
	}
	return grReg, nil
}

// typicalReferencePixel 获取参考图像典型预测像素
// 入参: w 横坐标, h 纵坐标
// 返回: int 像素值, bool 是否可预测
func (g *GRRDProc) typicalReferencePixel(w, h int32) (int, bool) {
	x := w - g.GRREFERENCEDX
	y := h - g.GRREFERENCEDY
	value := g.GRREFERENCE.GetPixel(x, y)
	predictable := value == g.GRREFERENCE.GetPixel(x-1, y-1) &&
		value == g.GRREFERENCE.GetPixel(x, y-1) &&
		value == g.GRREFERENCE.GetPixel(x+1, y-1) &&
		value == g.GRREFERENCE.GetPixel(x-1, y) &&
		value == g.GRREFERENCE.GetPixel(x+1, y) &&
		value == g.GRREFERENCE.GetPixel(x-1, y+1) &&
		value == g.GRREFERENCE.GetPixel(x, y+1) &&
		value == g.GRREFERENCE.GetPixel(x+1, y+1)
	return value, predictable
}
