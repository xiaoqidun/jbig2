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

// Package jbig2 一个高性能、零依赖的纯 Go 语言 JBIG2 解码器
package jbig2

import (
	"bytes"
	"compress/zlib"
	"errors"
	"image"
	"image/color"
	"io"
)

// Decoder JBIG2解码器
type Decoder struct {
	doc       *Document
	pageIndex uint32
}

// NewDecoder 创建解码器
// 入参: r 读取器
// 返回: *Decoder 解码器, error 错误信息
func NewDecoder(r io.Reader) (*Decoder, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	if len(data) > 8 && data[0] == 'C' && data[1] == 'W' && data[2] == 'S' {
		zr, err := zlib.NewReader(bytes.NewReader(data[8:]))
		if err != nil {
			return nil, err
		}
		defer zr.Close()
		decompressed, err := io.ReadAll(zr)
		if err != nil {
			return nil, err
		}
		data = decompressed
		if len(data) > 0 {
			nbits := int(data[0] >> 3)
			rectBits := 5 + nbits*4
			rectBytes := (rectBits + 7) / 8
			startOffset := rectBytes + 4
			if len(data) > startOffset {
				data = data[startOffset:]
				for len(data) >= 2 {
					tagCodeAndLen := int(data[0]) | (int(data[1]) << 8)
					tagCode := tagCodeAndLen >> 6
					tagLen := tagCodeAndLen & 0x3F
					headerLen := 2
					if tagLen == 0x3F {
						if len(data) >= 6 {
							tagLen = int(data[2]) | (int(data[3]) << 8) | (int(data[4]) << 16) | (int(data[5]) << 24)
							headerLen = 6
						} else {
							break
						}
					}
					if tagCode == 0 {
						break
					}
					if tagCode == 6 || tagCode == 21 || tagCode == 35 || tagCode == 90 {
						skipBytes := 2
						if tagCode == 35 || tagCode == 90 {
							skipBytes = 6
						}
						payloadOffset := headerLen + skipBytes
						if len(data) > payloadOffset {
							data = data[payloadOffset:]
							break
						}
					}
					nextOffset := headerLen + tagLen
					if len(data) >= nextOffset {
						data = data[nextOffset:]
					} else {
						break
					}
				}
			}
		}
		jbig2Signature := []byte{0x97, 0x4A, 0x42, 0x32, 0x0D, 0x0A, 0x1A, 0x0A}
		idx := bytes.Index(data, jbig2Signature)
		if idx != -1 {
			data = data[idx:]
		}
	}
	data, randomAccess, littleEndian, orgMode, grouped := probeConfigs(data)
	if data == nil {
		return nil, errors.New("no valid jbig2 configuration found")
	}
	doc := NewDocument(data, nil, randomAccess, littleEndian)
	doc.OrgMode = orgMode
	doc.Grouped = grouped
	return &Decoder{doc: doc, pageIndex: 0}, nil
}

// NewDecoderWithGlobals 创建带全局段的解码器
// 入参: r 读取器, globals 全局段数据
// 返回: *Decoder 解码器, error 错误信息
func NewDecoderWithGlobals(r io.Reader, globals []byte) (*Decoder, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	if len(data) > 8 && data[0] == 'C' && data[1] == 'W' && data[2] == 'S' {
		zr, err := zlib.NewReader(bytes.NewReader(data[8:]))
		if err != nil {
			return nil, err
		}
		defer zr.Close()
		decompressed, err := io.ReadAll(zr)
		if err != nil {
			return nil, err
		}
		data = decompressed
		if len(data) > 0 {
			nbits := int(data[0] >> 3)
			rectBits := 5 + nbits*4
			rectBytes := (rectBits + 7) / 8
			startOffset := rectBytes + 4
			if len(data) > startOffset {
				data = data[startOffset:]
				for len(data) >= 2 {
					tagCodeAndLen := int(data[0]) | (int(data[1]) << 8)
					tagCode := tagCodeAndLen >> 6
					tagLen := tagCodeAndLen & 0x3F
					headerLen := 2
					if tagLen == 0x3F {
						if len(data) >= 6 {
							tagLen = int(data[2]) | (int(data[3]) << 8) | (int(data[4]) << 16) | (int(data[5]) << 24)
							headerLen = 6
						} else {
							break
						}
					}
					if tagCode == 0 {
						break
					}
					if tagCode == 6 || tagCode == 21 || tagCode == 35 || tagCode == 90 {
						skipBytes := 2
						if tagCode == 35 || tagCode == 90 {
							skipBytes = 6
						}
						payloadOffset := headerLen + skipBytes
						if len(data) > payloadOffset {
							data = data[payloadOffset:]
							break
						}
					}
					nextOffset := headerLen + tagLen
					if len(data) >= nextOffset {
						data = data[nextOffset:]
					} else {
						break
					}
				}
			}
		}
	}
	probedData, randomAccess, littleEndian, orgMode, grouped := probeConfigs(data)
	if probedData == nil {
		if len(globals) > 0 {
			probedData = data
			randomAccess = false
			littleEndian = false
			orgMode = 0
			grouped = false
			if len(data) >= 4 {
				if data[0] != 0 && data[1] == 0 && data[2] == 0 && data[3] == 0 {
					littleEndian = true
				}
			}
		} else {
			return nil, errors.New("no valid jbig2 configuration found")
		}
	} else {
		data = probedData
	}
	doc := NewDocument(data, globals, randomAccess, littleEndian)
	doc.OrgMode = orgMode
	doc.Grouped = grouped
	for {
		res := doc.globalContext.DecodeSequential()
		if res == ResultEndReached {
			break
		}
		if res == ResultFailure {
			return nil, errors.New("failed to parse global segments")
		}
		if res == ResultPageCompleted {
			continue
		}
	}
	return &Decoder{doc: doc, pageIndex: 0}, nil
}

// Decode 解码下一页
// 返回: image.Image 图像, error 错误信息
func (d *Decoder) Decode() (image.Image, error) {
	if d.doc == nil {
		return nil, errors.New("decoder not initialized")
	}
	for {
		res := d.doc.DecodeSequential()
		if res == ResultEndReached {
			if d.doc.inPage && d.doc.page != nil {
				d.doc.inPage = false
				d.pageIndex++
				img := d.doc.page.ToGoImage()
				d.doc.ReleasePageSegments(d.pageIndex)
				return img, nil
			}
			return nil, io.EOF
		}
		if res == ResultPageCompleted {
			if d.doc.page == nil {
				return nil, errors.New("page completed but no image found")
			}
			d.pageIndex++
			img := d.doc.page.ToGoImage()
			d.doc.ReleasePageSegments(d.pageIndex)
			return img, nil
		}
		if res == ResultFailure {
			return nil, errors.New("decoding failed")
		}
	}
}

// DecodeAll 解码所有剩余页面
// 返回: []image.Image 图像列表, error 错误信息
func (d *Decoder) DecodeAll() ([]image.Image, error) {
	var images []image.Image
	for {
		img, err := d.Decode()
		if err == io.EOF {
			break
		}
		if err != nil {
			return images, err
		}
		images = append(images, img)
	}
	return images, nil
}

// Decode 解码JBIG2数据包含的第一页
// 入参: r 读取器
// 返回: image.Image 图像, error 错误信息
func Decode(r io.Reader) (image.Image, error) {
	dec, err := NewDecoder(r)
	if err != nil {
		return nil, err
	}
	return dec.Decode()
}

// DecodeConfig 获取JBIG2图像配置
// 入参: r 读取器
// 返回: image.Config 图像配置, error 错误信息
func DecodeConfig(r io.Reader) (image.Config, error) {
	dec, err := NewDecoder(r)
	if err != nil {
		return image.Config{}, err
	}
	for {
		if len(dec.doc.pageInfoList) > 0 {
			info := dec.doc.pageInfoList[0]
			return image.Config{
				ColorModel: color.GrayModel,
				Width:      int(info.Width),
				Height:     int(info.Height),
			}, nil
		}
		res := dec.doc.DecodeSequential()
		if res == ResultEndReached {
			break
		}
		if res == ResultFailure {
			return image.Config{}, errors.New("decoding failed while looking for config")
		}
	}
	return image.Config{}, errors.New("page information not found")
}

// probeConfigs 探测JBIG2文件的配置
// 入参: data 数据
// 返回: probed 探测后的数据, randomAccess 是否随机访问, littleEndian 是否小端序, orgMode 组织模式, grouped 是否分组
func probeConfigs(data []byte) (probed []byte, randomAccess bool, littleEndian bool, orgMode int, grouped bool) {
	jbig2Signature := []byte{0x97, 0x4A, 0x42, 0x32, 0x0D, 0x0A, 0x1A, 0x0A}
	if len(data) < 8 || !bytes.HasPrefix(data, jbig2Signature) {
		return nil, false, false, 0, false
	}
	type Config struct {
		Offset       int
		RandomAccess bool
		LittleEndian bool
		OrgMode      int
		Grouped      bool
	}
	var validConfig *Config
	bestScore := -1
	candidates := []Config{
		{9, true, false, 0, false},
		{9, false, false, 0, false},
		{9, true, false, 1, false},
		{13, false, false, 0, false},
		{13, false, true, 0, false},
		{9, false, true, 0, false},
	}
	for _, cfg := range candidates {
		if len(data) <= cfg.Offset+5 {
			continue
		}
		hasPageCount := (data[8] & 0x02) == 0
		if hasPageCount && cfg.Offset == 9 {
			continue
		}
		if !hasPageCount && cfg.Offset == 13 {
			continue
		}
		hStart := 0
		var segNum uint32
		if cfg.OrgMode == 1 || !cfg.RandomAccess {
			hStart = 4
			if len(data) <= cfg.Offset+4 {
				continue
			}
			s1, s2, s3, s4 := uint32(data[cfg.Offset]), uint32(data[cfg.Offset+1]), uint32(data[cfg.Offset+2]), uint32(data[cfg.Offset+3])
			if cfg.LittleEndian {
				segNum = s1 | (s2 << 8) | (s3 << 16) | (s4 << 24)
			} else {
				segNum = (s1 << 24) | (s2 << 16) | (s3 << 8) | s4
			}
		}
		if len(data) <= cfg.Offset+hStart {
			continue
		}
		flagsByte := data[cfg.Offset+hStart]
		hStart++
		_ = flagsByte & 0x3F
		pageAssocSize := (flagsByte & 0x40) != 0
		if len(data) <= cfg.Offset+hStart {
			continue
		}
		refByte := data[cfg.Offset+hStart]
		hStart++
		refCount := int(refByte >> 5)
		if refCount == 7 {
			continue
		}
		segNumSizeBytes := 1
		if !cfg.RandomAccess || cfg.OrgMode == 1 {
			if segNum > 65536 {
				segNumSizeBytes = 4
			} else if segNum > 256 {
				segNumSizeBytes = 2
			}
		}
		if refCount > 0 {
			hStart += refCount * segNumSizeBytes
		}
		if cfg.OrgMode == 1 || !cfg.RandomAccess {
			if pageAssocSize {
				hStart += 4
			} else {
				hStart += 1
			}
		}
		if len(data) <= cfg.Offset+hStart+3 {
			continue
		}
		dl1, dl2, dl3, dl4 := uint32(data[cfg.Offset+hStart]), uint32(data[cfg.Offset+hStart+1]), uint32(data[cfg.Offset+hStart+2]), uint32(data[cfg.Offset+hStart+3])
		var dataLen uint32
		if cfg.LittleEndian {
			dataLen = dl1 | (dl2 << 8) | (dl3 << 16) | (dl4 << 24)
		} else {
			dataLen = (dl1 << 24) | (dl2 << 16) | (dl3 << 8) | dl4
		}
		remaining := len(data) - (cfg.Offset + hStart + 4)
		score := 0
		if int(dataLen) <= remaining {
			score += 50
		} else {
			score -= 80
		}
		if dataLen > 0 {
			score += 10
		}
		declaredRandom := (data[8] & 0x01) != 0
		if cfg.RandomAccess == declaredRandom {
			score += 10
		}
		candidateGrouped := false
		hSize := hStart + 4
		gIdx := cfg.Offset + hSize
		if gIdx+5 < len(data) {
			n1, n2, n3, n4 := data[gIdx], data[gIdx+1], data[gIdx+2], data[gIdx+3]
			nSeg := uint32(n1)<<24 | uint32(n2)<<16 | uint32(n3)<<8 | uint32(n4)
			nType := data[gIdx+4] & 0x3F
			if nSeg > 0 && nSeg < 1000 && nType <= 62 && nType != 0 {
				candidateGrouped = true
				score += 40
			}
		}
		if score > bestScore {
			bestScore = score
			validConfig = &Config{cfg.Offset, cfg.RandomAccess, cfg.LittleEndian, cfg.OrgMode, candidateGrouped}
		}
	}
	if validConfig == nil {
		return nil, false, false, 0, false
	}
	return data[validConfig.Offset:], validConfig.RandomAccess, validConfig.LittleEndian, validConfig.OrgMode, validConfig.Grouped
}

func init() {
	image.RegisterFormat("jbig2", "\x97\x4A\x42\x32\x0D\x0A\x1A\x0A", Decode, DecodeConfig)
}

// ToGoImage 转换为Go标准库Image
// 返回: image.Image 图像
func (i *Image) ToGoImage() image.Image {
	if i == nil {
		return nil
	}
	rect := image.Rect(0, 0, int(i.width), int(i.height))
	img := image.NewGray(rect)
	w, h := int(i.width), int(i.height)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			bit := i.GetPixel(int32(x), int32(y))
			if bit != 0 {
				img.SetGray(x, y, color.Gray{Y: 0})
			} else {
				img.SetGray(x, y, color.Gray{Y: 255})
			}
		}
	}
	return img
}
