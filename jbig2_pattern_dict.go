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

// PatternDict 模式字典
type PatternDict struct {
	NUMPATS uint32
	HDPATS  []*Image
}

// NewPatternDict 创建模式字典
// 入参: dictSize 字典大小
// 返回: *PatternDict 模式字典
func NewPatternDict(dictSize uint32) *PatternDict {
	return &PatternDict{
		NUMPATS: dictSize,
		HDPATS:  make([]*Image, dictSize),
	}
}

// DeepCopy 复制模式字典及各模式的像素数据
// 返回: *PatternDict 模式字典副本
func (p *PatternDict) DeepCopy() *PatternDict {
	dst := NewPatternDict(p.NUMPATS)
	for i, img := range p.HDPATS {
		if img != nil {
			dst.HDPATS[i] = img.Duplicate()
		}
	}
	return dst
}
