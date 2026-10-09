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
	"bytes"
	"image"
)

// encodedPage 已编码的单页数据
type encodedPage struct {
	width       int32
	height      int32
	resolutionX uint32
	resolutionY uint32
	data        []byte
}

// prepareEncodedPage 准备页面并在写入前完成编码
// 入参: src 图像, opts 编码参数, number 页号, endPage 是否包含页结束段
// 返回: *encodedPage 页面数据, error 错误信息
func prepareEncodedPage(src image.Image, opts *Options, number uint32, endPage bool) (*encodedPage, error) {
	options := normalizeEncodeOptions(opts)
	headerSize := uint64(11)
	if number > 255 {
		headerSize = 14
	}
	overhead := 19 + 26 + headerSize*2
	if endPage {
		overhead += headerSize
	}
	if options.MaxPageBytes <= overhead {
		return nil, ErrLimitExceeded
	}
	img, err := packEncodeImage(src, options.MaxPixels)
	if err != nil {
		return nil, err
	}
	data, err := encodeGenericRegion(img, int(options.MaxPageBytes-overhead))
	if err != nil {
		return nil, err
	}
	return &encodedPage{
		width:       img.width,
		height:      img.height,
		resolutionX: options.ResolutionX,
		resolutionY: options.ResolutionY,
		data:        data,
	}, nil
}

// encodeGenericRegion 使用模板0和典型行预测编码通用区域
// 入参: img 打包位图, limit 最大编码字节数
// 返回: []byte 算术编码数据, error 错误信息
func encodeGenericRegion(img *Image, limit int) ([]byte, error) {
	contexts := make([]ArithCtx, 1<<16)
	encoder := newArithEncoder(limit)
	var ltp uint8
	for y := int32(0); y < img.height; y++ {
		row := img.row(y)
		previous := img.row(y - 1)
		var predicted uint8
		if equalGenericRows(row, previous, img.width) {
			predicted = 1
		}
		encoder.encode(&contexts[0x9b25], predicted^ltp)
		ltp = predicted
		if encoder.err != nil {
			return nil, encoder.err
		}
		if ltp != 0 {
			continue
		}
		older := img.row(y - 2)
		previousBits := getPixelFromRow(previous, 3, img.width) |
			getPixelFromRow(previous, 2, img.width)<<1 |
			getPixelFromRow(previous, 1, img.width)<<2 |
			getPixelFromRow(previous, 0, img.width)<<3
		olderBits := getPixelFromRow(older, 2, img.width) |
			getPixelFromRow(older, 1, img.width)<<1 |
			getPixelFromRow(older, 0, img.width)<<2
		context := previousBits<<4 | olderBits<<11
		interiorEnd := max(img.width-4, 0) &^ 7
		if previous == nil || older == nil {
			interiorEnd = 0
		}
		x := int32(0)
		for ; x < interiorEnd; x += 8 {
			index := x >> 3
			pixels := row[index]
			previousWindow := uint32(previous[index])<<8 | uint32(previous[index+1])
			olderWindow := uint32(older[index])<<8 | uint32(older[index+1])
			for shift := 7; shift >= 0; shift-- {
				bit := pixels >> uint(shift) & 1
				cx := &contexts[context]
				if !encoder.tryEncodeFast(cx, bit) {
					encoder.encode(cx, bit)
				}
				context = (context<<1)&0xf7ee | uint32(bit) |
					(previousWindow>>uint(shift+4)&1)<<4 | (olderWindow>>uint(shift+5)&1)<<11
			}
		}
		for ; x < img.width; x++ {
			bit := getPixelFromRowUnchecked(row, x)
			encoder.encode(&contexts[context], uint8(bit))
			context = (context<<1)&0xf7ee | bit |
				getPixelFromRow(previous, x+4, img.width)<<4 | getPixelFromRow(older, x+3, img.width)<<11
		}
		if encoder.err != nil {
			return nil, encoder.err
		}
	}
	return encoder.finish()
}

// equalGenericRows 比较两行的有效像素，缺失的上一行按全白处理
// 入参: row 当前行, previous 上一行, width 有效像素宽度
// 返回: bool 两行像素是否相同
func equalGenericRows(row, previous []byte, width int32) bool {
	fullBytes := width >> 3
	tailMask := byte(0xff << (8 - uint(width&7)))
	if previous == nil {
		for _, value := range row[:fullBytes] {
			if value != 0 {
				return false
			}
		}
		return tailMask == 0 || row[fullBytes]&tailMask == 0
	}
	return bytes.Equal(row[:fullBytes], previous[:fullBytes]) &&
		(tailMask == 0 || (row[fullBytes]^previous[fullBytes])&tailMask == 0)
}
