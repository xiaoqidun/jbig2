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
	"errors"
	"fmt"
	"image"
	"io"
)

var (
	// ErrInvalidImage 图像为空或尺寸及存储无效
	ErrInvalidImage = errors.New("jbig2: invalid image")
	// ErrNonBinaryImage 图像包含非黑白或非完全不透明像素
	ErrNonBinaryImage = errors.New("jbig2: image is not binary")
	// ErrLimitExceeded 编码超过尺寸或资源限制
	ErrLimitExceeded = errors.New("jbig2: encoding limit exceeded")
	// ErrClosed 编码器已关闭
	ErrClosed = errors.New("jbig2: encoder is closed")
	// ErrNoPages 未提供可编码的页面
	ErrNoPages = errors.New("jbig2: no pages to encode")
)

// Options 单页无损编码参数
// 零值使用默认设置，ResolutionX和ResolutionY的单位为像素每米，0表示未知
// MaxPixels限制每页像素数，0表示64Mi像素，尺寸另受JBig2MaxImageSize限制
// MaxPageBytes限制每页编码数据及段头的总字节数，0表示128MiB，不包含文件头及文件结束段
type Options struct {
	ResolutionX  uint32
	ResolutionY  uint32
	MaxPixels    uint64
	MaxPageBytes uint64
}

// Encoder JBIG2编码器
// 通过NewEncoder创建，逐页写入同一文档，使用后不可复制或并发调用
type Encoder struct {
	w             io.Writer
	pageIndex     uint32
	segmentNumber uint64
	expectedPages uint32
	started       bool
	closed        bool
	err           error
}

// NewEncoder 创建编码器
// 创建时不写入数据，不关闭w，调用Close完成文件
// 入参: w 写入器
// 返回: *Encoder 编码器
func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{w: w}
}

// Encode 编码下一页
// src必须是完全不透明的黑白图像，opts为nil时使用默认设置
// 不修改或保留src，输入错误不提交本页，写入错误后编码器不可继续使用
// 入参: src 图像, opts 编码参数
// 返回: error 错误信息
func (e *Encoder) Encode(src image.Image, opts *Options) error {
	if err := e.checkState(); err != nil {
		return err
	}
	if e.pageIndex == ^uint32(0) || e.segmentNumber+3 > uint64(^uint32(0)) {
		return ErrLimitExceeded
	}
	pageNumber := e.pageIndex + 1
	page, err := prepareEncodedPage(src, opts, pageNumber, true)
	if err != nil {
		return fmt.Errorf("jbig2: page %d: %w", pageNumber, err)
	}
	if !e.started {
		if err := e.writeFileHeader(); err != nil {
			return err
		}
		e.started = true
	}
	if err := e.writePage(page, pageNumber, true); err != nil {
		return err
	}
	e.pageIndex = pageNumber
	e.segmentNumber += 3
	return nil
}

// EncodeAll 编码并追加全部给定页面
// opts应用于本次全部页面，空切片不写入数据，成功后仍可追加页面，调用Close完成文件
// 失败时保留已经写入的页面，输入错误可从未提交的页面继续，写入错误不可恢复
// 入参: images 图像列表, opts 编码参数
// 返回: error 错误信息
func (e *Encoder) EncodeAll(images []image.Image, opts *Options) error {
	if err := e.checkState(); err != nil {
		return err
	}
	for _, img := range images {
		if err := e.Encode(img, opts); err != nil {
			return err
		}
	}
	return nil
}

// Close 完成编码并写入文件结束段
// 不关闭或刷新调用方的w，成功后重复调用返回nil，写入失败后重复调用返回首次错误
// 未编码任何页面时返回ErrNoPages并关闭编码器，不写入空文件
// 返回: error 错误信息
func (e *Encoder) Close() error {
	if e.err != nil {
		return e.err
	}
	if e.closed {
		return nil
	}
	e.closed = true
	if !e.started {
		e.err = ErrNoPages
		return e.err
	}
	return e.writeSegmentHeader(51, 0, 0, e.segmentNumber)
}

// Encode 编码JBIG2单页文件
// 写入完整文件，不关闭w，src及opts的约定与Encoder.Encode相同
// 入参: w 写入器, src 图像, opts 编码参数
// 返回: error 错误信息
func Encode(w io.Writer, src image.Image, opts *Options) error {
	enc := NewEncoder(w)
	enc.expectedPages = 1
	if err := enc.Encode(src, opts); err != nil {
		return err
	}
	return enc.Close()
}

// EncodeAll 编码JBIG2多页文件
// 按images顺序写入完整文件，各页可具有不同尺寸，opts应用于全部页面，不关闭w
// 空切片返回ErrNoPages，失败时可能已写出部分文件，调用方应丢弃失败输出
// 入参: w 写入器, images 图像列表, opts 编码参数
// 返回: error 错误信息
func EncodeAll(w io.Writer, images []image.Image, opts *Options) error {
	if len(images) == 0 {
		return ErrNoPages
	}
	if uint64(len(images))*3+1 > uint64(^uint32(0))+1 {
		return ErrLimitExceeded
	}
	enc := NewEncoder(w)
	enc.expectedPages = uint32(len(images))
	if err := enc.EncodeAll(images, opts); err != nil {
		return err
	}
	return enc.Close()
}

// EncodeEmbedded 编码JBIG2单页嵌入数据
// 写入不含文件头、页结束段和文件结束段的页面数据，不生成全局段，不关闭w
// 页面关联为1，可用于PDF的JBIG2Decode，也可由NewDecoderWithGlobals以nil全局段解码
// 入参: w 写入器, src 图像, opts 编码参数
// 返回: error 错误信息
func EncodeEmbedded(w io.Writer, src image.Image, opts *Options) error {
	enc := NewEncoder(w)
	if err := enc.checkState(); err != nil {
		return err
	}
	page, err := prepareEncodedPage(src, opts, 1, false)
	if err != nil {
		return err
	}
	return enc.writePage(page, 1, false)
}

// checkState 检查编码器是否可继续写入
// 返回: error 错误信息
func (e *Encoder) checkState() error {
	if e.err != nil {
		return e.err
	}
	if e.closed {
		return ErrClosed
	}
	if e.w == nil {
		return errors.New("jbig2: encoder has no writer")
	}
	return nil
}

// write 写入完整数据并保留首次写入错误
// 入参: data 数据
// 返回: error 错误信息
func (e *Encoder) write(data []byte) error {
	if e.err != nil {
		return e.err
	}
	n, err := e.w.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err != nil {
		e.err = fmt.Errorf("jbig2: write: %w", err)
	}
	return e.err
}

// writeFileHeader 写入顺序组织文件头
// 返回: error 错误信息
func (e *Encoder) writeFileHeader() error {
	var header [13]byte
	copy(header[:8], jbig2Signature)
	header[8] = 3
	if e.expectedPages == 0 {
		return e.write(header[:9])
	}
	header[8] = 1
	binary.BigEndian.PutUint32(header[9:], e.expectedPages)
	return e.write(header[:])
}

// writeSegmentHeader 写入无引用段头
// 入参: kind 段类型, page 页号, length 数据长度, number 段编号
// 返回: error 错误信息
func (e *Encoder) writeSegmentHeader(kind byte, page, length uint32, number uint64) error {
	var header [14]byte
	binary.BigEndian.PutUint32(header[:4], uint32(number))
	header[4] = kind
	if page <= 255 {
		header[6] = byte(page)
		binary.BigEndian.PutUint32(header[7:11], length)
		return e.write(header[:11])
	}
	header[4] |= 0x40
	binary.BigEndian.PutUint32(header[6:10], page)
	binary.BigEndian.PutUint32(header[10:14], length)
	return e.write(header[:])
}

// writePage 写入已编码的页面段
// 入参: page 页面数据, number 页号, endPage 是否写入页结束段
// 返回: error 错误信息
func (e *Encoder) writePage(page *encodedPage, number uint32, endPage bool) error {
	var info [19]byte
	binary.BigEndian.PutUint32(info[:4], uint32(page.width))
	binary.BigEndian.PutUint32(info[4:8], uint32(page.height))
	binary.BigEndian.PutUint32(info[8:12], page.resolutionX)
	binary.BigEndian.PutUint32(info[12:16], page.resolutionY)
	info[16] = 1
	if err := e.writeSegmentHeader(48, number, uint32(len(info)), e.segmentNumber); err != nil {
		return err
	}
	if err := e.write(info[:]); err != nil {
		return err
	}
	var region [26]byte
	binary.BigEndian.PutUint32(region[:4], uint32(page.width))
	binary.BigEndian.PutUint32(region[4:8], uint32(page.height))
	copy(region[18:], []byte{3, 0xff, 0xfd, 0xff, 2, 0xfe, 0xfe, 0xfe})
	if err := e.writeSegmentHeader(39, number, uint32(len(region)+len(page.data)), e.segmentNumber+1); err != nil {
		return err
	}
	if err := e.write(region[:]); err != nil {
		return err
	}
	if err := e.write(page.data); err != nil {
		return err
	}
	if endPage {
		return e.writeSegmentHeader(49, number, 0, e.segmentNumber+2)
	}
	return nil
}
