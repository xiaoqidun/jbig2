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
	"image"
	"image/color"
	"image/draw"
)

// colorRun 颜色游程
type colorRun struct {
	length uint32
	value  color.NRGBA64
}

// defaultColorPalette 默认调色板
var defaultColorPalette = func() []color.NRGBA64 {
	values := [...]uint32{
		0x000000, 0x808080, 0xc0c0c0, 0xffffff,
		0xff0000, 0x00ff00, 0x0000ff, 0xffff00,
		0x00ffff, 0xff00ff, 0x800000, 0x008000,
		0x000080, 0x808000, 0x008080, 0x800080,
		0xffa500, 0xcccc00, 0x990000, 0x00cc00,
		0x009900, 0xcccc00, 0x999900, 0x660000,
		0x0000cc, 0x000099, 0xcc00cc, 0x990099,
		0x00cccc, 0x009999, 0x666666, 0x999999,
	}
	palette := make([]color.NRGBA64, len(values))
	for i, value := range values {
		palette[i] = color.NRGBA64{uint16(value>>16) * 257, uint16((value>>8)&255) * 257, uint16(value&255) * 257, 65535}
	}
	return palette
}()

// newColorImage 创建彩色图像
// 入参: width 宽度, height 高度
// 返回: *image.NRGBA64 图像对象
func newColorImage(width, height int32) *image.NRGBA64 {
	if width <= 0 || height <= 0 || uint64(width)*uint64(height) > 2147483647/8 {
		return nil
	}
	return image.NewNRGBA64(image.Rect(0, 0, int(width), int(height)))
}

// readColorComponent 读取颜色分量
// 入参: stream 位流, length 分量字节数
// 返回: uint16 分量值, error 错误信息
func readColorComponent(stream *BitStream, length byte) (uint16, error) {
	switch length {
	case 1:
		value, err := stream.Read1Byte()
		return uint16(value) * 257, err
	case 2:
		return stream.ReadShortInteger()
	case 4:
		value, err := stream.ReadInteger()
		return uint16((uint64(value)*65535 + 2147483647) / 4294967295), err
	}
	return 0, errors.New("unsupported color component length")
}

// parseColorPalette 解析调色板段
// 入参: segment 段对象
// 返回: Result 解析结果
func (d *Document) parseColorPalette(segment *Segment) Result {
	flags, err := d.stream.Read1Byte()
	if err != nil || (flags>>1)&15 > 1 {
		return ResultFailure
	}
	for flags&1 != 0 {
		flags, err = d.stream.Read1Byte()
		if err != nil || flags&0xfe != 0 {
			return ResultFailure
		}
	}
	components, err := d.stream.Read1Byte()
	if err != nil || components != 3 {
		return ResultFailure
	}
	length, err := d.stream.Read1Byte()
	if err != nil || (length != 1 && length != 2 && length != 4) {
		return ResultFailure
	}
	count, err := d.stream.ReadInteger()
	if err != nil || uint64(count)*3*uint64(length) > uint64(d.stream.GetByteLeft()) {
		return ResultFailure
	}
	palette := make([]color.NRGBA64, count)
	for i := range palette {
		r, errR := readColorComponent(d.stream, length)
		g, errG := readColorComponent(d.stream, length)
		b, errB := readColorComponent(d.stream, length)
		if errR != nil || errG != nil || errB != nil {
			return ResultFailure
		}
		palette[i] = color.NRGBA64{r, g, b, 65535}
	}
	segment.ColorPalette = palette
	segment.ResultType = JBig2ColorPalettePointer
	return ResultSuccess
}

// getColorPalette 获取区域调色板
// 入参: segment 段对象
// 返回: []color.NRGBA64 颜色集合, error 错误信息
func (d *Document) getColorPalette(segment *Segment) ([]color.NRGBA64, error) {
	palette := defaultColorPalette
	for _, number := range segment.ReferredToSegmentNumbers {
		ref := d.FindSegmentByNumber(number)
		if ref == nil {
			return nil, errors.New("color palette reference not found")
		}
		if ref.Flags.Type == 54 {
			palette = append(palette, ref.ColorPalette...)
		}
	}
	return palette, nil
}

// decodeColorRuns 解码文本颜色游程
// 入参: stream 位流, count 实例数, palette 调色板, directRGB 是否允许直接RGB
// 返回: []colorRun 颜色游程, bool 是否直接RGB, error 错误信息
func decodeColorRuns(stream *BitStream, count uint32, palette []color.NRGBA64, directRGB bool) ([]colorRun, bool, error) {
	components, err := stream.Read1Byte()
	if err != nil || (components != 1 && !(directRGB && components == 3)) {
		return nil, false, errors.New("unsupported text color components")
	}
	length, err := stream.Read1Byte()
	if err != nil || length != 1 {
		return nil, false, errors.New("unsupported text color component length")
	}
	values, err := stream.ReadInteger()
	if err != nil || values != count {
		return nil, false, errors.New("text color count mismatch")
	}
	var runs []colorRun
	for remaining := count; remaining > 0; {
		shortRun, err := stream.Read1Byte()
		if err != nil {
			return nil, false, err
		}
		run := uint32(shortRun)
		if run == 0 {
			longRun, err := stream.ReadShortInteger()
			if err != nil {
				return nil, false, err
			}
			run = uint32(longRun)
		}
		if run > remaining {
			return nil, false, errors.New("invalid text color run")
		}
		var value color.NRGBA64
		if components == 1 {
			id, err := stream.Read1Byte()
			if err != nil || int(id) >= len(palette) {
				return nil, false, errors.New("text color index out of bounds")
			}
			value = palette[id]
		} else {
			r, errR := readColorComponent(stream, 1)
			g, errG := readColorComponent(stream, 1)
			b, errB := readColorComponent(stream, 1)
			if errR != nil || errG != nil || errB != nil {
				return nil, false, errors.New("truncated text color")
			}
			value = color.NRGBA64{r, g, b, 65535}
		}
		if run > 0 {
			runs = append(runs, colorRun{run, value})
			remaining -= run
		}
	}
	if stream.GetByteLeft() != 0 {
		return nil, false, errors.New("unexpected text color data")
	}
	return runs, components == 3, nil
}

// paintColorMask 绘制彩色前景
// 入参: dst 目标图像, mask 前景掩码, x 横坐标, y 纵坐标, value 颜色
func paintColorMask(dst *image.NRGBA64, mask *Image, x, y int32, value color.NRGBA64) {
	left := max(int64(0), int64(x))
	top := max(int64(0), int64(y))
	right := min(int64(dst.Rect.Dx()), int64(x)+int64(mask.width))
	bottom := min(int64(dst.Rect.Dy()), int64(y)+int64(mask.height))
	for py := top; py < bottom; py++ {
		for px := left; px < right; px++ {
			if mask.GetPixel(int32(px-int64(x)), int32(py-int64(y))) != 0 {
				dst.SetNRGBA64(int(px), int(py), value)
			}
		}
	}
}

// paintColorSymbol 绘制文本实例颜色
// 入参: mask 实例掩码, x 横坐标, y 纵坐标
// 返回: error 错误信息
func (t *TRDProc) paintColorSymbol(mask *Image, x, y int32) error {
	if len(t.colorRuns) == 0 {
		return errors.New("too many colored symbol instances")
	}
	paintColorMask(t.colorImage, mask, x, y, t.colorRuns[0].value)
	t.colorRuns[0].length--
	if t.colorRuns[0].length == 0 {
		t.colorRuns = t.colorRuns[1:]
	}
	return nil
}

// composeMonochromeColor 将黑白区域组合彩色页面
// 入参: dst 目标图像, mask 区域掩码, ri 区域信息
func composeMonochromeColor(dst *image.NRGBA64, mask *Image, ri *RegionInfo) {
	left := max(int64(0), int64(ri.X))
	top := max(int64(0), int64(ri.Y))
	right := min(int64(dst.Rect.Dx()), int64(ri.X)+int64(mask.width))
	bottom := min(int64(dst.Rect.Dy()), int64(ri.Y)+int64(mask.height))
	op := composeOpFromRegionFlags(ri.Flags)
	for y := top; y < bottom; y++ {
		for x := left; x < right; x++ {
			value := dst.NRGBA64At(int(x), int(y))
			foreground := mask.GetPixel(int32(x-int64(ri.X)), int32(y-int64(ri.Y))) != 0
			switch op {
			case ComposeOr:
				if foreground {
					value = color.NRGBA64{A: 65535}
				}
			case ComposeAnd:
				if !foreground {
					value = color.NRGBA64{}
				}
			case ComposeXor, ComposeXnor:
				if foreground == (op == ComposeXor) {
					if value.A == 0 {
						value = color.NRGBA64{A: 65535}
					} else {
						value = color.NRGBA64{}
					}
				}
			case ComposeReplace:
				value = color.NRGBA64{}
				if foreground {
					value.A = 65535
				}
			}
			dst.SetNRGBA64(int(x), int(y), value)
		}
	}
}

// composeColorRegion 组合彩色页面区域
// 入参: segment 段对象, ri 区域信息
// 返回: Result 组合结果
func (d *Document) composeColorRegion(segment *Segment, ri *RegionInfo) Result {
	if d.colorPage.Rect.Dy() != int(d.page.Height()) {
		grown := newColorImage(d.page.Width(), d.page.Height())
		if grown == nil {
			return ResultFailure
		}
		draw.Draw(grown, d.colorPage.Bounds(), d.colorPage, image.Point{}, draw.Src)
		d.colorPage = grown
	}
	if segment.colorImage != nil {
		point := image.Pt(int(ri.X), int(ri.Y))
		draw.Draw(d.colorPage, segment.colorImage.Bounds().Add(point), segment.colorImage, image.Point{}, draw.Over)
	} else if !d.referenceColors {
		composeMonochromeColor(d.colorPage, segment.Image, ri)
	}
	return ResultSuccess
}

// pageImage 获取页面图像
// 返回: image.Image 页面图像
func (d *Document) pageImage() image.Image {
	if d.colorPage != nil {
		if d.referenceColors {
			for y := 0; y < d.colorPage.Rect.Dy(); y++ {
				for x := 0; x < d.colorPage.Rect.Dx(); x++ {
					if d.colorPage.NRGBA64At(x, y).A == 0 {
						d.colorPage.SetNRGBA64(x, y, color.NRGBA64{65535, 65535, 65535, 65535})
					}
				}
			}
		}
		return d.colorPage
	}
	return d.page.ToGoImage()
}
