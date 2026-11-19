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
	"context"
	"errors"
	"io"

	"golang.org/x/image/ccitt"
)

// DecodeG4 将CCITT Group4数据解码到目标位图
// 入参: stream 位流, image 目标图像
// 返回: error 错误信息
func DecodeG4(stream *BitStream, image *Image) error {
	return decodeG4Context(nil, stream, image)
}

// decodeG4Context 在上下文约束内将CCITT Group4数据解码到目标位图
// 入参: ctx 上下文, stream 位流, image 目标图像
// 返回: error 错误信息
func decodeG4Context(ctx context.Context, stream *BitStream, image *Image) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	stream.AlignByte()
	data := stream.GetPointer()
	if data == nil {
		return errors.New("insufficient data for g4 decode")
	}
	reader := bytes.NewReader(data)
	var src io.Reader = reader
	if ctx != nil {
		src = &contextReader{ctx: ctx, r: reader}
	}
	opts := &ccitt.Options{
		Invert: true,
	}
	decoder := ccitt.NewReader(src, ccitt.MSB, ccitt.Group4, int(image.Width()), int(image.Height()), opts)
	if _, err := io.ReadFull(decoder, image.Data()); err != nil {
		return err
	}
	if err := checkContext(ctx); err != nil {
		return err
	}
	consumed := int64(len(data)) - int64(reader.Len())
	stream.AddOffset(uint32(consumed))
	return nil
}
