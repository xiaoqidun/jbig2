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

import "image"

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

// encodeGenericRegion 使用模板0和名义自适应像素编码通用区域
// 入参: img 打包位图, limit 最大编码字节数
// 返回: []byte 算术编码数据, error 错误信息
func encodeGenericRegion(img *Image, limit int) ([]byte, error) {
	contexts := make([]ArithCtx, 1<<16)
	encoder := newArithEncoder(limit)
	for y := int32(0); y < img.height; y++ {
		row := img.row(y)
		previous := img.row(y - 1)
		older := img.row(y - 2)
		previousBits := getPixelFromRow(previous, 3, img.width) |
			getPixelFromRow(previous, 2, img.width)<<1 |
			getPixelFromRow(previous, 1, img.width)<<2 |
			getPixelFromRow(previous, 0, img.width)<<3
		olderBits := getPixelFromRow(older, 2, img.width) |
			getPixelFromRow(older, 1, img.width)<<1 |
			getPixelFromRow(older, 0, img.width)<<2
		currentBits := uint32(0)
		for x := int32(0); x < img.width; x++ {
			context := currentBits | previousBits<<4 | olderBits<<11
			bit := getPixelFromRowUnchecked(row, x)
			encoder.encode(&contexts[context], uint8(bit))
			currentBits = (currentBits<<1 | bit) & 0x0f
			previousBits = (previousBits<<1 | getPixelFromRow(previous, x+4, img.width)) & 0x7f
			olderBits = (olderBits<<1 | getPixelFromRow(older, x+3, img.width)) & 0x1f
		}
		if encoder.err != nil {
			return nil, encoder.err
		}
	}
	return encoder.finish()
}
