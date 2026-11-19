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
	"context"
	"fmt"
	"io"
	"math"
)

// 默认资源限制常量
const (
	defaultMaxInputBytes        uint64 = 1 << 30
	defaultMaxDecompressedBytes uint64 = 256 << 20
	defaultMaxSegmentBytes      uint64 = 256 << 20
	defaultMaxPixels            uint64 = 1 << 30
	defaultMaxMemoryBytes       uint64 = 512 << 20
)

// LimitError 表示资源用量超出限制
type LimitError struct {
	Resource string
	Limit    uint64
	Required uint64
}

// Error 返回错误描述
// 返回: string 错误描述
func (e *LimitError) Error() string {
	return fmt.Sprintf("%s limit exceeded: need %d, limit %d", e.Resource, e.Required, e.Limit)
}

// Unwrap 返回底层超限错误以便errors.Is判断
// 返回: error 底层错误
func (e *LimitError) Unwrap() error {
	return ErrLimitExceeded
}

// Limits 限制解码时的资源用量，零值使用默认限制
// 默认输入上限为1GiB、解压上限为256MiB、单段上限为256MiB、像素上限为2^30、内存上限为512MiB
type Limits struct {
	MaxInputBytes        uint64
	MaxDecompressedBytes uint64
	MaxSegmentBytes      uint64
	MaxPixels            uint64
	MaxMemoryBytes       uint64
}

// normalized 补充未设置的资源限制
// 返回: Limits 资源限制
func (l Limits) normalized() Limits {
	if l.MaxInputBytes == 0 {
		l.MaxInputBytes = defaultMaxInputBytes
	}
	if l.MaxDecompressedBytes == 0 {
		l.MaxDecompressedBytes = defaultMaxDecompressedBytes
	}
	if l.MaxSegmentBytes == 0 {
		l.MaxSegmentBytes = defaultMaxSegmentBytes
	}
	if l.MaxPixels == 0 {
		l.MaxPixels = defaultMaxPixels
	}
	if l.MaxMemoryBytes == 0 {
		l.MaxMemoryBytes = defaultMaxMemoryBytes
	}
	return l
}

// DecodeOptions 解码选项，零值使用默认资源限制与背景上下文
type DecodeOptions struct {
	Context context.Context
	Limits  Limits
}

// normalizeDecodeOptions 归一化解码选项
// 入参: opts 解码选项
// 返回: context.Context 上下文, Limits 资源限制
func normalizeDecodeOptions(opts *DecodeOptions) (context.Context, Limits) {
	if opts == nil {
		return context.Background(), (Limits{}).normalized()
	}
	ctx := opts.Context
	if ctx == nil {
		ctx = context.Background()
	}
	return ctx, opts.Limits.normalized()
}

// decodeBudget 追踪解码过程中的内存与像素预算
type decodeBudget struct {
	limits     Limits
	usedMemory uint64
}

// newDecodeBudget 创建资源预算控制器
// 入参: limits 资源限制
// 返回: *decodeBudget 预算控制器
func newDecodeBudget(limits Limits) *decodeBudget {
	return &decodeBudget{limits: limits.normalized()}
}

// checkProduct 检查无符号整数乘积是否溢出或超限
// 入参: resource 资源名称, a 乘数, b 乘数, limit 上限
// 返回: uint64 乘积, error 错误信息
func checkProduct(resource string, a, b, limit uint64) (uint64, error) {
	if b != 0 && a > math.MaxUint64/b {
		return 0, &LimitError{Resource: resource, Limit: limit, Required: math.MaxUint64}
	}
	n := a * b
	if n > limit {
		return 0, &LimitError{Resource: resource, Limit: limit, Required: n}
	}
	return n, nil
}

// imageBytes 检查二值或彩色图像的像素与单图内存上限并返回所需字节数
// 入参: width 宽度, height 高度, colorImage 是否为彩色图像
// 返回: uint64 所需字节数, error 错误信息
func (b *decodeBudget) imageBytes(width, height int32, colorImage bool) (uint64, error) {
	if width <= 0 || height < 0 {
		return 0, nil
	}
	if b == nil {
		if colorImage {
			return uint64(width) * uint64(height) * 8, nil
		}
		return uint64((int64(width)+7)/8) * uint64(height), nil
	}
	if _, err := checkProduct("pixels", uint64(width), uint64(height), b.limits.MaxPixels); err != nil {
		return 0, err
	}
	var stride uint64
	if colorImage {
		stride = uint64(width) * 8
	} else {
		stride = uint64((int64(width) + 7) / 8)
	}
	return checkProduct("memory", stride, uint64(height), b.limits.MaxMemoryBytes)
}

// pageBytes 检查页面尺寸并计算单页所需总字节数
// 入参: width 宽度, height 高度, colorPage 是否包含彩色页面
// 返回: uint64 所需总字节数, error 错误信息
func (b *decodeBudget) pageBytes(width, height int32, colorPage bool) (uint64, error) {
	total, err := b.imageBytes(width, height, false)
	if err != nil || !colorPage {
		return total, err
	}
	colorBytes, err := b.imageBytes(width, height, true)
	if err != nil {
		return 0, err
	}
	if b != nil && colorBytes > math.MaxUint64-total {
		return 0, &LimitError{Resource: "memory", Limit: b.limits.MaxMemoryBytes, Required: math.MaxUint64}
	}
	return total + colorBytes, nil
}

// checkImage 检查二值或彩色图像的像素及新增内存需求
// 入参: width 宽度, height 高度, colorImage 是否为彩色图像
// 返回: uint64 所需字节数, error 错误信息
func (b *decodeBudget) checkImage(width, height int32, colorImage bool) (uint64, error) {
	size, err := b.imageBytes(width, height, colorImage)
	if err != nil {
		return 0, err
	}
	if err := b.checkAddMemory(size); err != nil {
		return 0, err
	}
	return size, nil
}

// checkAddMemory 检查新增内存是否超出上限，不修改已用记录
// 入参: added 新增字节数
// 返回: error 错误信息
func (b *decodeBudget) checkAddMemory(added uint64) error {
	if b == nil || added == 0 {
		return nil
	}
	if added > math.MaxUint64-b.usedMemory {
		return &LimitError{Resource: "memory", Limit: b.limits.MaxMemoryBytes, Required: math.MaxUint64}
	}
	total := b.usedMemory + added
	if total > b.limits.MaxMemoryBytes {
		return &LimitError{Resource: "memory", Limit: b.limits.MaxMemoryBytes, Required: total}
	}
	return nil
}

// reserveMemory 预留内存配额
// 入参: added 新增字节数
// 返回: error 错误信息
func (b *decodeBudget) reserveMemory(added uint64) error {
	if b == nil || added == 0 {
		return nil
	}
	if err := b.checkAddMemory(added); err != nil {
		return err
	}
	b.usedMemory += added
	return nil
}

// releaseMemory 归还已预留的内存配额
// 入参: released 归还字节数
func (b *decodeBudget) releaseMemory(released uint64) {
	if b == nil || released == 0 {
		return
	}
	if released >= b.usedMemory {
		b.usedMemory = 0
		return
	}
	b.usedMemory -= released
}

// checkSegmentLength 检查段长度是否超出上限
// 入参: length 段长度
// 返回: error 错误信息
func (b *decodeBudget) checkSegmentLength(length uint32) error {
	if b == nil || length == 0xFFFFFFFF {
		return nil
	}
	if uint64(length) > b.limits.MaxSegmentBytes {
		return &LimitError{Resource: "segment bytes", Limit: b.limits.MaxSegmentBytes, Required: uint64(length)}
	}
	return nil
}

// checkContext 检查上下文是否已取消
// 入参: ctx 上下文
// 返回: error 错误信息
func checkContext(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

// readAllLimited 在字节上限和上下文约束内读取全部数据
// 入参: ctx 上下文, r 读取器, limit 最大字节数, resource 资源名称
// 返回: []byte 数据, error 错误信息
func readAllLimited(ctx context.Context, r io.Reader, limit uint64, resource string) ([]byte, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	var out []byte
	var buf [32768]byte
	for {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		n, err := r.Read(buf[:])
		if n > 0 {
			if uint64(len(out))+uint64(n) > limit {
				return nil, &LimitError{Resource: resource, Limit: limit, Required: uint64(len(out)) + uint64(n)}
			}
			out = append(out, buf[:n]...)
		}
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
	}
}

// contextReader 在读取过程中周期性响应Context取消
type contextReader struct {
	ctx   context.Context
	r     io.Reader
	calls uint32
}

// Read 读取数据并检查上下文取消
// 入参: p 目标缓冲
// 返回: int 读取字节数, error 错误信息
func (cr *contextReader) Read(p []byte) (int, error) {
	cr.calls++
	if cr.calls&15 == 1 {
		if err := checkContext(cr.ctx); err != nil {
			return 0, err
		}
	}
	return cr.r.Read(p)
}
