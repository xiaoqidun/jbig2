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
	return g.decodeInto(arithDecoder, grContexts, nil)
}

// decodeInto 解码到复用图像
// 入参: arithDecoder 算术解码器, grContexts 上下文, reuse 复用图像
// 返回: *Image 图像, error 错误信息
func (g *GRRDProc) decodeInto(arithDecoder *ArithDecoder, grContexts []ArithCtx, reuse *Image) (*Image, error) {
	if g.GRW > JBig2MaxImageSize || g.GRH > JBig2MaxImageSize {
		return nil, errors.New("image size too large")
	}
	if g.GRREFERENCE == nil {
		return nil, errors.New("reference image is nil")
	}
	if !g.GRTEMPLATE {
		if g.GRAT[0] == -1 && g.GRAT[1] == -1 && g.GRAT[2] == -1 && g.GRAT[3] == -1 {
			return g.decodeTemplate0Opt(arithDecoder, grContexts, reuse)
		}
		return g.decodeTemplate0Custom(arithDecoder, grContexts, reuse)
	}
	return g.decodeTemplate1Opt(arithDecoder, grContexts, reuse)
}

// decodeReferenceTemplate0 解码参考软件使用的重复上行细化模板
// 入参: decoder 算术解码器, contexts 上下文
// 返回: *Image 图像, error 错误信息
func (g *GRRDProc) decodeReferenceTemplate0(decoder *ArithDecoder, contexts []ArithCtx) (*Image, error) {
	img := NewImage(int32(g.GRW), int32(g.GRH))
	if img == nil {
		return nil, errors.New("failed to create image")
	}
	for y := int32(0); y < int32(g.GRH); y++ {
		for x := int32(0); x < int32(g.GRW); x++ {
			rx, ry := x-g.GRREFERENCEDX, y-g.GRREFERENCEDY
			var context uint32
			for i := int32(0); i < 3; i++ {
				context |= uint32(g.GRREFERENCE.GetPixel(rx+1-i, ry-1)) << uint(i)
				context |= uint32(g.GRREFERENCE.GetPixel(rx+1-i, ry)) << uint(i+3)
			}
			context |= uint32(g.GRREFERENCE.GetPixel(rx+1, ry-1)) << 6
			context |= uint32(g.GRREFERENCE.GetPixel(rx, ry-1)) << 7
			context |= uint32(g.GRREFERENCE.GetPixel(rx+int32(g.GRAT[2]), ry+int32(g.GRAT[3]))) << 8
			context |= uint32(img.GetPixel(x-1, y)) << 9
			context |= uint32(img.GetPixel(x+1, y-1)) << 10
			context |= uint32(img.GetPixel(x, y-1)) << 11
			context |= uint32(img.GetPixel(x+int32(g.GRAT[0]), y+int32(g.GRAT[1]))) << 12
			if decoder.IsComplete() {
				return nil, errors.New("decoder complete prematurely")
			}
			img.SetPixel(x, y, decoder.Decode(&contexts[context]))
		}
	}
	return img, nil
}

// decodeTemplate0Opt 模板0优化解码
// 入参: decoder 算术解码器, contexts 上下文, reuse 复用图像
// 返回: *Image 图像, error 错误信息
func (g *GRRDProc) decodeTemplate0Opt(decoder *ArithDecoder, contexts []ArithCtx, reuse *Image) (*Image, error) {
	grReg := reuseImage(reuse, int32(g.GRW), int32(g.GRH))
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
		context := getPixelFromRow(previousRow, 1, width) << 10
		context |= getPixelFromRow(previousRow, 0, width) << 11
		context |= getPixelFromRow(refPreviousRow, referenceX+1, referenceWidth) << 6
		context |= getPixelFromRow(refPreviousRow, referenceX, referenceWidth) << 7
		context |= getPixelFromRow(refPreviousRow, referenceX-1, referenceWidth) << 8
		context |= getPixelFromRow(refRow, referenceX+1, referenceWidth) << 3
		context |= getPixelFromRow(refRow, referenceX, referenceWidth) << 4
		context |= getPixelFromRow(refRow, referenceX-1, referenceWidth) << 5
		context |= getPixelFromRow(refNextRow, referenceX+1, referenceWidth)
		context |= getPixelFromRow(refNextRow, referenceX, referenceWidth) << 1
		context |= getPixelFromRow(refNextRow, referenceX-1, referenceWidth) << 2
		interiorStart := int32(0)
		interiorEnd := width - 2
		if start := -referenceX - 2; start > interiorStart {
			interiorStart = start
		}
		if end := referenceWidth - referenceX - 2; end < interiorEnd {
			interiorEnd = end
		}
		if previousRow == nil || refPreviousRow == nil || refRow == nil || refNextRow == nil || interiorEnd <= interiorStart {
			interiorStart = 0
			interiorEnd = 0
		}
		for w := int32(0); w < width; w++ {
			bVal := 0
			needDecode := ltp == 0
			if ltp != 0 {
				pixels := context & 0x01ff
				bVal = int((context >> 4) & 1)
				needDecode = pixels != 0 && pixels != 0x01ff
			}
			if needDecode {
				if decoder.IsComplete() {
					return nil, errors.New("decoder complete prematurely")
				}
				bVal = decoder.Decode(&contexts[context])
			}
			if bVal != 0 {
				setPixelInRow(row, w)
			}
			var previousNext, refPreviousNext, refNext, refNextNext uint32
			referenceNext := referenceX + w + 2
			if uint32(w-interiorStart) < uint32(interiorEnd-interiorStart) {
				previousNext = getPixelFromRowUnchecked(previousRow, w+2)
				refPreviousNext = getPixelFromRowUnchecked(refPreviousRow, referenceNext)
				refNext = getPixelFromRowUnchecked(refRow, referenceNext)
				refNextNext = getPixelFromRowUnchecked(refNextRow, referenceNext)
			} else {
				previousNext = getPixelFromRow(previousRow, w+2, width)
				refPreviousNext = getPixelFromRow(refPreviousRow, referenceNext, referenceWidth)
				refNext = getPixelFromRow(refRow, referenceNext, referenceWidth)
				refNextNext = getPixelFromRow(refNextRow, referenceNext, referenceWidth)
			}
			context = ((context << 1) & 0x19b6) | previousNext<<10 | uint32(bVal)<<9 |
				refPreviousNext<<6 | refNext<<3 | refNextNext
		}
	}
	return grReg, nil
}

// decodeTemplate1Opt 模板1优化解码
// 入参: decoder 算术解码器, contexts 上下文, reuse 复用图像
// 返回: *Image 图像, error 错误信息
func (g *GRRDProc) decodeTemplate1Opt(decoder *ArithDecoder, contexts []ArithCtx, reuse *Image) (*Image, error) {
	grReg := reuseImage(reuse, int32(g.GRW), int32(g.GRH))
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

// decodeTemplate0Custom 模板0自适应位置解码
// 入参: decoder 算术解码器, contexts 上下文, reuse 复用图像
// 返回: *Image 图像, error 错误信息
func (g *GRRDProc) decodeTemplate0Custom(decoder *ArithDecoder, contexts []ArithCtx, reuse *Image) (*Image, error) {
	grReg := reuseImage(reuse, int32(g.GRW), int32(g.GRH))
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
			if decoder.Decode(&contexts[0x0010]) != 0 {
				ltp ^= 1
			}
		}
		row := grReg.row(h)
		previousRow := grReg.row(h - 1)
		adaptiveRow := grReg.row(h + int32(g.GRAT[1]))
		refY := h - g.GRREFERENCEDY
		refPreviousRow := g.GRREFERENCE.row(refY - 1)
		refRow := g.GRREFERENCE.row(refY)
		refNextRow := g.GRREFERENCE.row(refY + 1)
		refAdaptiveRow := g.GRREFERENCE.row(refY + int32(g.GRAT[3]))
		line1 := getPixelFromRow(previousRow, 1, width)
		line1 |= getPixelFromRow(previousRow, 0, width) << 1
		var line2 uint32
		line3 := getPixelFromRow(refPreviousRow, referenceX+1, referenceWidth)
		line3 |= getPixelFromRow(refPreviousRow, referenceX, referenceWidth) << 1
		line4 := getPixelFromRow(refRow, referenceX+1, referenceWidth)
		line4 |= getPixelFromRow(refRow, referenceX, referenceWidth) << 1
		line4 |= getPixelFromRow(refRow, referenceX-1, referenceWidth) << 2
		line5 := getPixelFromRow(refNextRow, referenceX+1, referenceWidth)
		line5 |= getPixelFromRow(refNextRow, referenceX, referenceWidth) << 1
		line5 |= getPixelFromRow(refNextRow, referenceX-1, referenceWidth) << 2
		for w := int32(0); w < width; w++ {
			bVal := 0
			needDecode := ltp == 0
			if ltp != 0 {
				value, predictable := typicalPixelFromRows(refPreviousRow, refRow, refNextRow, referenceX+w, referenceWidth)
				bVal = int(value)
				needDecode = !predictable
			}
			if needDecode {
				context := line5 | line4<<3 | line3<<6 | line2<<9 | line1<<10
				context |= getPixelFromRow(refAdaptiveRow, referenceX+w+int32(g.GRAT[2]), referenceWidth) << 8
				context |= getPixelFromRow(adaptiveRow, w+int32(g.GRAT[0]), width) << 12
				if decoder.IsComplete() {
					return nil, errors.New("decoder complete prematurely")
				}
				bVal = decoder.Decode(&contexts[context])
			}
			if bVal != 0 {
				setPixelInRow(row, w)
			}
			line1 = ((line1 << 1) | getPixelFromRow(previousRow, w+2, width)) & 0x03
			line2 = uint32(bVal)
			line3 = ((line3 << 1) | getPixelFromRow(refPreviousRow, referenceX+w+2, referenceWidth)) & 0x03
			line4 = ((line4 << 1) | getPixelFromRow(refRow, referenceX+w+2, referenceWidth)) & 0x07
			line5 = ((line5 << 1) | getPixelFromRow(refNextRow, referenceX+w+2, referenceWidth)) & 0x07
		}
	}
	return grReg, nil
}
