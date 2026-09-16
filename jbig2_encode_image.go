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
	"fmt"
	"image"
	"reflect"
)

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
		if gray.Stride < w || len(gray.Pix) < w || h > 1 && gray.Stride > (len(gray.Pix)-w)/(h-1) {
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
	for y := int32(0); y < img.height; y++ {
		row := img.row(y)
		for x := int32(0); x < img.width; x++ {
			r, g, b, a := src.At(bounds.Min.X+int(x), bounds.Min.Y+int(y)).RGBA()
			background := uint64(0xffff - a)
			luminance := (19595*(uint64(r)+background) + 38470*(uint64(g)+background) + 7471*(uint64(b)+background) + 32768) >> 24
			if luminance < uint64(threshold) {
				setPixelInRow(row, x)
			}
		}
	}
	return img, nil
}
