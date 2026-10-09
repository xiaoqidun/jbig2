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
	"encoding/binary"
	"fmt"
	"image"
	"reflect"
)

// 编码时每页的默认像素数和输出字节数上限
const (
	defaultEncodeMaxPixels    = 64 << 20
	defaultEncodeMaxPageBytes = 128 << 20
)

// normalizeEncodeOptions 复制编码选项并应用默认限制
// 入参: opts 编码参数
// 返回: Options 生效参数
func normalizeEncodeOptions(opts *Options) Options {
	var result Options
	if opts != nil {
		result = *opts
	}
	if result.MaxPixels == 0 {
		result.MaxPixels = defaultEncodeMaxPixels
	}
	if result.MaxPageBytes == 0 {
		result.MaxPageBytes = defaultEncodeMaxPageBytes
	}
	result.MaxPageBytes = min(result.MaxPageBytes, uint64(^uint32(0))-1, uint64(^uint(0)>>1))
	return result
}

// encodeImageBounds 检查输入图像及像素上限
// 入参: src 图像, maxPixels 像素上限
// 返回: image.Rectangle 图像边界, error 错误信息
func encodeImageBounds(src image.Image, maxPixels uint64) (image.Rectangle, error) {
	if src == nil {
		return image.Rectangle{}, ErrInvalidImage
	}
	value := reflect.ValueOf(src)
	if value.Kind() == reflect.Pointer && value.IsNil() {
		return image.Rectangle{}, ErrInvalidImage
	}
	bounds := src.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if bounds.Empty() || w <= 0 || h <= 0 {
		return image.Rectangle{}, ErrInvalidImage
	}
	if w > JBig2MaxImageSize || h > JBig2MaxImageSize || uint64(w)*uint64(h) > maxPixels {
		return image.Rectangle{}, ErrLimitExceeded
	}
	return bounds, nil
}

// packEncodeImage 获取只读二值位图
// 打包图像借用原数据，其他图像验证全部像素后转换，不修改输入
// 入参: src 图像, maxPixels 像素上限
// 返回: *Image 位图, error 错误信息
func packEncodeImage(src image.Image, maxPixels uint64) (*Image, error) {
	bounds, err := encodeImageBounds(src, maxPixels)
	if err != nil {
		return nil, err
	}
	w, h := bounds.Dx(), bounds.Dy()
	if packed, ok := src.(*Image); ok {
		if packed.stride < (packed.width+7)/8 || int64(packed.stride)*int64(packed.height) > int64(len(packed.data)) {
			return nil, ErrInvalidImage
		}
		return packed, nil
	}
	img := NewImage(int32(w), int32(h))
	if img == nil {
		return nil, ErrInvalidImage
	}
	if gray, ok := src.(*image.Gray); ok {
		if !validPixelBuffer(len(gray.Pix), gray.Stride, w, h) {
			return nil, ErrInvalidImage
		}
		for y := 0; y < h; y++ {
			row := img.row(int32(y))
			for x, pixel := range gray.Pix[y*gray.Stride : y*gray.Stride+w] {
				switch pixel {
				case 0:
					setPixelInRow(row, int32(x))
				case 255:
				default:
					return nil, fmt.Errorf("%w at (%d, %d)", ErrNonBinaryImage, bounds.Min.X+x, bounds.Min.Y+y)
				}
			}
		}
		return img, nil
	}
	switch src := src.(type) {
	case *image.RGBA:
		return packRGBAPixels(img, src.Pix, src.Stride, bounds)
	case *image.NRGBA:
		return packRGBAPixels(img, src.Pix, src.Stride, bounds)
	case *image.Paletted:
		return packPalettedPixels(img, src)
	}
	for y := 0; y < h; y++ {
		row := img.row(int32(y))
		for x := 0; x < w; x++ {
			r, g, b, a := src.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			if a != 0xffff || r != g || r != b || r != 0 && r != 0xffff {
				return nil, fmt.Errorf("%w at (%d, %d)", ErrNonBinaryImage, bounds.Min.X+x, bounds.Min.Y+y)
			}
			if r == 0 {
				setPixelInRow(row, int32(x))
			}
		}
	}
	return img, nil
}

// validPixelBuffer 检查像素缓冲是否容纳全部有效行
// 入参: length 缓冲长度, stride 行步长, rowBytes 每行有效字节数, height 行数
// 返回: bool 存储是否有效
func validPixelBuffer(length, stride, rowBytes, height int) bool {
	return stride >= rowBytes && length >= rowBytes && (height == 1 || stride <= (length-rowBytes)/(height-1))
}

// packRGBAPixels 验证并打包完全不透明的四通道黑白像素
// 入参: dst 目标位图, pixels 像素缓冲, stride 行步长, bounds 源图像边界
// 返回: *Image 黑白位图, error 错误信息
func packRGBAPixels(dst *Image, pixels []byte, stride int, bounds image.Rectangle) (*Image, error) {
	w, h := bounds.Dx(), bounds.Dy()
	if !validPixelBuffer(len(pixels), stride, w*4, h) {
		return nil, ErrInvalidImage
	}
	for y := 0; y < h; y++ {
		row := dst.row(int32(y))
		src := pixels[y*stride : y*stride+w*4]
		for x := 0; x < w; x++ {
			switch binary.LittleEndian.Uint32(src[x*4:]) {
			case 0xff000000:
				setPixelInRow(row, int32(x))
			case 0xffffffff:
			default:
				return nil, fmt.Errorf("%w at (%d, %d)", ErrNonBinaryImage, bounds.Min.X+x, bounds.Min.Y+y)
			}
		}
	}
	return dst, nil
}

// packPalettedPixels 按调色板映射验证并打包黑白像素
// 入参: dst 目标位图, src 调色板图像
// 返回: *Image 黑白位图, error 错误信息
func packPalettedPixels(dst *Image, src *image.Paletted) (*Image, error) {
	w, h := src.Rect.Dx(), src.Rect.Dy()
	if !validPixelBuffer(len(src.Pix), src.Stride, w, h) {
		return nil, ErrInvalidImage
	}
	var colors [256]byte
	for y := 0; y < h; y++ {
		row := dst.row(int32(y))
		for x, index := range src.Pix[y*src.Stride : y*src.Stride+w] {
			value := colors[index]
			if value == 0 {
				if int(index) >= len(src.Palette) || src.Palette[index] == nil {
					return nil, ErrInvalidImage
				}
				r, g, b, a := src.Palette[index].RGBA()
				if a != 0xffff || r != g || r != b || r != 0 && r != 0xffff {
					return nil, fmt.Errorf("%w at (%d, %d)", ErrNonBinaryImage, src.Rect.Min.X+x, src.Rect.Min.Y+y)
				}
				value = 1
				if r == 0 {
					value = 2
				}
				colors[index] = value
			}
			if value == 2 {
				setPixelInRow(row, int32(x))
			}
		}
	}
	return dst, nil
}

// Binarize 将图像转换为黑白位图
// 在白色背景合成透明像素，亮度小于threshold为黑色，其余为白色，阈值0全部为白色
// 返回独立图像且原点为0，不修改src，转换会丢失灰度、颜色和透明度
// 使用默认64Mi像素上限，编码时对转换后的位图保持无损
// 入参: src 图像, threshold 灰度阈值
// 返回: *Image 黑白位图, error 错误信息
func Binarize(src image.Image, threshold uint8) (*Image, error) {
	bounds, err := encodeImageBounds(src, defaultEncodeMaxPixels)
	if err != nil {
		return nil, err
	}
	img := NewImage(int32(bounds.Dx()), int32(bounds.Dy()))
	if img == nil {
		return nil, ErrInvalidImage
	}
	switch src := src.(type) {
	case *Image:
		if src.stride < img.stride || int64(src.stride)*int64(src.height) > int64(len(src.data)) {
			return nil, ErrInvalidImage
		}
		if threshold != 0 {
			for y := int32(0); y < img.height; y++ {
				copy(img.row(y), src.row(y))
				if tail := uint(img.width & 7); tail != 0 {
					img.row(y)[img.stride-1] &= 0xff << (8 - tail)
				}
			}
		}
		return img, nil
	case *image.Gray:
		if !validPixelBuffer(len(src.Pix), src.Stride, int(img.width), int(img.height)) {
			return nil, ErrInvalidImage
		}
		for y := int32(0); y < img.height; y++ {
			row := img.row(y)
			for x, value := range src.Pix[int(y)*src.Stride : int(y)*src.Stride+int(img.width)] {
				if value < threshold {
					setPixelInRow(row, int32(x))
				}
			}
		}
		return img, nil
	case *image.RGBA:
		return binarizeRGBAPixels(img, src.Pix, src.Stride, threshold, true)
	case *image.NRGBA:
		return binarizeRGBAPixels(img, src.Pix, src.Stride, threshold, false)
	case *image.Paletted:
		return binarizePalettedPixels(img, src, threshold)
	}
	for y := int32(0); y < img.height; y++ {
		row := img.row(y)
		for x := int32(0); x < img.width; x++ {
			r, g, b, a := src.At(bounds.Min.X+int(x), bounds.Min.Y+int(y)).RGBA()
			if binaryLuminance(r, g, b, a) < uint64(threshold) {
				setPixelInRow(row, x)
			}
		}
	}
	return img, nil
}

// binaryLuminance 计算预乘颜色在白色背景上的八位亮度
// 入参: r 红色分量, g 绿色分量, b 蓝色分量, a 透明度，分量范围为0至65535
// 返回: uint64 亮度值
func binaryLuminance(r, g, b, a uint32) uint64 {
	background := uint64(0xffff - a)
	return (19595*(uint64(r)+background) + 38470*(uint64(g)+background) + 7471*(uint64(b)+background) + 32768) >> 24
}

// binarizeRGBAPixels 将四通道像素合成到白色背景后按阈值二值化
// 入参: dst 目标位图, pixels 像素缓冲, stride 行步长, threshold 灰度阈值, premultiplied 颜色是否已预乘透明度
// 返回: *Image 黑白位图, error 错误信息
func binarizeRGBAPixels(dst *Image, pixels []byte, stride int, threshold uint8, premultiplied bool) (*Image, error) {
	w, h := int(dst.width), int(dst.height)
	if !validPixelBuffer(len(pixels), stride, w*4, h) {
		return nil, ErrInvalidImage
	}
	for y := 0; y < h; y++ {
		row := dst.row(int32(y))
		src := pixels[y*stride : y*stride+w*4]
		for x := 0; x < w; x++ {
			pixel := src[x*4 : x*4+4]
			r, g, b, a := uint32(pixel[0])*257, uint32(pixel[1])*257, uint32(pixel[2])*257, uint32(pixel[3])
			if !premultiplied {
				r, g, b = r*a/255, g*a/255, b*a/255
			}
			if binaryLuminance(r, g, b, a*257) < uint64(threshold) {
				setPixelInRow(row, int32(x))
			}
		}
	}
	return dst, nil
}

// binarizePalettedPixels 按需计算调色板颜色的二值化结果
// 入参: dst 目标位图, src 调色板图像, threshold 灰度阈值
// 返回: *Image 黑白位图, error 错误信息
func binarizePalettedPixels(dst *Image, src *image.Paletted, threshold uint8) (*Image, error) {
	w, h := src.Rect.Dx(), src.Rect.Dy()
	if !validPixelBuffer(len(src.Pix), src.Stride, w, h) {
		return nil, ErrInvalidImage
	}
	var colors [256]byte
	for y := 0; y < h; y++ {
		row := dst.row(int32(y))
		for x, index := range src.Pix[y*src.Stride : y*src.Stride+w] {
			value := colors[index]
			if value == 0 {
				if int(index) >= len(src.Palette) || src.Palette[index] == nil {
					return nil, ErrInvalidImage
				}
				r, g, b, a := src.Palette[index].RGBA()
				value = 1
				if binaryLuminance(r, g, b, a) < uint64(threshold) {
					value = 2
				}
				colors[index] = value
			}
			if value == 2 {
				setPixelInRow(row, int32(x))
			}
		}
	}
	return dst, nil
}
