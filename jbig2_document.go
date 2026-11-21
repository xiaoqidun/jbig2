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
	"errors"
	"image"
	"image/color"
)

const (
	// ResultSuccess 成功
	ResultSuccess Result = 0
	// ResultFailure 失败
	ResultFailure Result = 1
	// ResultEndReached 到达终点
	ResultEndReached Result = 2
	// ResultDecodeToBeContinued 继续解码
	ResultDecodeToBeContinued Result = 3
	// ResultPageCompleted 页面完成
	ResultPageCompleted Result = 4
)

// Result 段解析的状态，包括成功、失败、继续解码及页面或数据结束
type Result int

// Document 文档上下文
type Document struct {
	stream           *BitStream
	globalContext    *Document
	segmentList      []*Segment
	segmentIndex     map[uint32]*Segment
	groupedLastRef   map[uint32]int
	page             *Image
	colorPage        *image.NRGBA64
	referenceColors  bool
	pageInfoList     []*PageInfo
	segment          *Segment
	offset           uint32
	groupedOffset    uint32
	groupedIndex     int
	groupedParsed    bool
	inPage           bool
	pageWritten      bool
	randomAccess     bool
	Grouped          bool
	OrgMode          int
	ctx              context.Context
	budget           *decodeBudget
	err              error
	pageMemBytes     uint64
	currentPageAssoc uint32
}

// PageInfo 页面信息段中的元数据
// Width和Height以像素为单位，Height为0xFFFFFFFF表示页面头未指定高度
// ResolutionX和ResolutionY保留页面头中的分辨率原始值
// DefaultPixelValue为黑白位图的默认像素值，true表示黑色
// IsStriped表示是否分条带，MaxStripeSize为页面头声明的最大条带高度
type PageInfo struct {
	Width             uint32
	Height            uint32
	ResolutionX       uint32
	ResolutionY       uint32
	DefaultPixelValue bool
	IsStriped         bool
	MaxStripeSize     uint16
}

// Globals 预解析的只读全局段集合，可由多个解码器并发复用
type Globals struct {
	segmentList  []*Segment
	segmentIndex map[uint32]*Segment
	memBytes     uint64
	littleEndian bool
	hasData      bool
}

// attachDocument 为页面文档构造共享只读全局段索引的轻量上下文外壳
// 入参: ctx 上下文, budget 页面解码预算
// 返回: *Document 轻量全局上下文
func (g *Globals) attachDocument(ctx context.Context, budget *decodeBudget) *Document {
	if g == nil || !g.hasData {
		return nil
	}
	return &Document{
		segmentList:  g.segmentList,
		segmentIndex: g.segmentIndex,
		ctx:          ctx,
		budget:       budget,
	}
}

// decodeGlobalSegments 顺序解析当前文档中的全部全局段
// 返回: error 错误信息
func (d *Document) decodeGlobalSegments() error {
	for {
		if err := checkContext(d.ctx); err != nil {
			return err
		}
		res := d.DecodeSequential()
		if res == ResultEndReached {
			return nil
		}
		if res == ResultFailure {
			if d.err != nil {
				return d.err
			}
			return errors.New("failed to parse global segments")
		}
	}
}

// GetSegments 获取段列表
// 返回当前保留段的内部切片而非副本，后续解码或释放页面段会改变列表及段数据
// 返回: []*Segment 段列表
func (d *Document) GetSegments() []*Segment {
	return d.segmentList
}

// GetGlobalContext 获取全局上下文
// 返回内部对象而非副本，不含全局段时返回nil
// 返回: *Document 全局上下文
func (d *Document) GetGlobalContext() *Document {
	return d.globalContext
}

// GetPageInfo 获取已解析的页面信息
// 按页面信息段的解析顺序查询，不推进解码，返回值为副本
// 页面头未指定高度时保留0xFFFFFFFF，实际尺寸以解码图像为准
// 入参: index 从0开始的页面索引，不是段中的页面编号
// 返回: PageInfo 页面信息, bool 是否存在，索引无效或尚未解析时返回零值和false
func (d *Document) GetPageInfo(index int) (PageInfo, bool) {
	if index < 0 || index >= len(d.pageInfoList) {
		return PageInfo{}, false
	}
	return *d.pageInfoList[index], true
}

// NewDocument 创建文档对象
// data为不含文件头的段数据，不进行格式探测或自动解析全局段，完整文件使用NewDecoder
// 直接引用data和globalData，不复制段数据
// 入参: data 数据流, globalData 全局数据, randomAccess 是否允许前向段引用, littleEndian 小端序
// 返回: *Document 文档对象
func NewDocument(data []byte, globalData []byte, randomAccess bool, littleEndian bool) *Document {
	stream := NewBitStream(data, 0)
	stream.SetLittleEndian(littleEndian)
	doc := &Document{
		stream:       stream,
		segmentIndex: make(map[uint32]*Segment),
		randomAccess: randomAccess,
	}
	if len(globalData) > 0 {
		globalStream := NewBitStream(globalData, 0)
		globalStream.SetLittleEndian(littleEndian)
		doc.globalContext = &Document{
			stream:       globalStream,
			segmentIndex: make(map[uint32]*Segment),
		}
	}
	return doc
}

// fail 记录结构化错误并返回失败状态
// 入参: err 错误信息
// 返回: Result 失败状态
func (d *Document) fail(err error) Result {
	if d.err == nil && err != nil {
		d.err = err
	}
	return ResultFailure
}

// setPageMemory 更新当前页面在预算中的内存占用
// 入参: newBytes 新字节数
// 返回: error 错误信息
func (d *Document) setPageMemory(newBytes uint64) error {
	if d.budget == nil {
		d.pageMemBytes = newBytes
		return nil
	}
	if newBytes > d.pageMemBytes {
		if err := d.budget.reserveMemory(newBytes - d.pageMemBytes); err != nil {
			return err
		}
	} else if d.pageMemBytes > newBytes {
		d.budget.releaseMemory(d.pageMemBytes - newBytes)
	}
	d.pageMemBytes = newBytes
	return nil
}

// resizePageMemory 校验页面尺寸并更新当前页面内存预算
// 入参: width 宽度, height 高度, colorPage 是否包含彩色页面
// 返回: error 错误信息
func (d *Document) resizePageMemory(width, height int32, colorPage bool) error {
	totalBytes, err := d.budget.pageBytes(width, height, colorPage)
	if err != nil {
		return err
	}
	return d.setPageMemory(totalBytes)
}

// estimateSegmentMemory 估算段长期保留的数据内存字节数
// 入参: seg 段
// 返回: uint64 字节数
func estimateSegmentMemory(seg *Segment) uint64 {
	var total uint64
	if seg.Image != nil {
		total += uint64(len(seg.Image.data))
	}
	if seg.colorImage != nil {
		total += uint64(len(seg.colorImage.Pix))
	}
	if seg.SymbolDict != nil {
		for _, img := range seg.SymbolDict.Images {
			if img != nil {
				total += uint64(len(img.data))
			}
		}
	}
	if seg.PatternDict != nil {
		for _, img := range seg.PatternDict.HDPATS {
			if img != nil {
				total += uint64(len(img.data))
			}
		}
	}
	total += uint64(len(seg.ColorPalette)) * 8
	total += uint64(len(seg.GBContexts)) + uint64(len(seg.GRContexts))
	return total
}

// chargeSegmentMemory 将段保留数据计入内存预算
// 入参: seg 段
// 返回: error 错误信息
func (d *Document) chargeSegmentMemory(seg *Segment) error {
	mem := estimateSegmentMemory(seg)
	if mem == seg.memBytes {
		return nil
	}
	if d.budget != nil {
		if mem > seg.memBytes {
			if err := d.budget.reserveMemory(mem - seg.memBytes); err != nil {
				return err
			}
		} else {
			d.budget.releaseMemory(seg.memBytes - mem)
		}
	}
	seg.memBytes = mem
	return nil
}

// releaseSegmentData 清空段内解码数据并归还内存预算
// 入参: seg 段
func (d *Document) releaseSegmentData(seg *Segment) {
	if seg == nil || seg.released {
		return
	}
	seg.Image = nil
	seg.colorImage = nil
	seg.ColorPalette = nil
	seg.PatternDict = nil
	seg.SymbolDict = nil
	seg.HuffmanTable = nil
	seg.GBContexts = nil
	seg.GRContexts = nil
	seg.released = true
	if d.budget != nil && seg.memBytes > 0 {
		d.budget.releaseMemory(seg.memBytes)
	}
	seg.memBytes = 0
}

// addSegment 追加段并维护按编号查找索引
// 入参: seg 段
func (d *Document) addSegment(seg *Segment) {
	d.segmentList = append(d.segmentList, seg)
	if d.segmentIndex == nil {
		d.segmentIndex = make(map[uint32]*Segment)
	}
	if _, exists := d.segmentIndex[seg.Number]; !exists {
		d.segmentIndex[seg.Number] = seg
	}
}

// releaseDeferredNonRetained 按保留标志释放不再被引用的延迟释放段
// 入参: seg 当前段, currentIndex 分组模式下的段索引，顺序模式为-1
func (d *Document) releaseDeferredNonRetained(seg *Segment, currentIndex int) {
	if seg == nil || len(seg.retainBits) == 0 || d.segmentIndex == nil {
		return
	}
	for i, refNum := range seg.ReferredToSegmentNumbers {
		if i >= len(seg.retainBits) || seg.retainBits[i] {
			continue
		}
		refSeg := d.segmentIndex[refNum]
		if refSeg == nil || !refSeg.Flags.DeferredNonRetain || refSeg.released {
			continue
		}
		if currentIndex >= 0 && d.groupedLastRef != nil {
			if lastIdx, ok := d.groupedLastRef[refNum]; ok && currentIndex < lastIdx {
				continue
			}
		}
		d.releaseSegmentData(refSeg)
		delete(d.segmentIndex, refNum)
	}
}

// ParseSegmentHeader 解析段头
// 入参: segment 段
// 返回: Result 解析状态
func (d *Document) ParseSegmentHeader(segment *Segment) Result {
	if val, err := d.stream.ReadInteger(); err != nil {
		return ResultFailure
	} else {
		segment.Number = val
	}
	var flags byte
	if val, err := d.stream.Read1Byte(); err != nil {
		return ResultFailure
	} else {
		flags = val
	}
	segment.Flags.Type = flags & 0x3F
	segment.Flags.PageAssociationSize = (flags & 0x40) != 0
	segment.Flags.DeferredNonRetain = (flags & 0x80) != 0
	cTemp := d.stream.GetCurByte()
	if (cTemp >> 5) == 7 {
		var count uint32
		if val, err := d.stream.ReadInteger(); err != nil {
			return ResultFailure
		} else {
			count = val
		}
		count &= 0x1FFFFFFF
		segment.ReferredToSegmentCount = int32(count)
		if segment.ReferredToSegmentCount > 1024 {
			return ResultFailure
		}
		retentionBits := segment.ReferredToSegmentCount + 1
		bytesToSkip := (retentionBits + 7) / 8
		if uint32(bytesToSkip) > d.stream.GetByteLeft() {
			return ResultFailure
		}
		retainBytes := make([]byte, bytesToSkip)
		for i := int32(0); i < bytesToSkip; i++ {
			val, err := d.stream.Read1Byte()
			if err != nil {
				return ResultFailure
			}
			retainBytes[i] = val
		}
		if segment.ReferredToSegmentCount > 0 {
			segment.retainBits = make([]bool, segment.ReferredToSegmentCount)
			for i := int32(0); i < segment.ReferredToSegmentCount; i++ {
				bitIdx := i + 1
				segment.retainBits[i] = (retainBytes[bitIdx/8] & (1 << uint(bitIdx%8))) != 0
			}
		}
	} else {
		if val, err := d.stream.Read1Byte(); err != nil {
			return ResultFailure
		} else {
			cTemp = val
		}
		segment.ReferredToSegmentCount = int32(cTemp >> 5)
		if segment.ReferredToSegmentCount > 0 && segment.ReferredToSegmentCount <= 4 {
			segment.retainBits = make([]bool, segment.ReferredToSegmentCount)
			for i := int32(0); i < segment.ReferredToSegmentCount; i++ {
				segment.retainBits[i] = (cTemp & (1 << uint(i+1))) != 0
			}
		}
	}
	cSSize := 1
	if segment.Number > 65536 {
		cSSize = 4
	} else if segment.Number > 256 {
		cSSize = 2
	}
	cPSize := 1
	if segment.Flags.PageAssociationSize {
		cPSize = 4
	}
	if segment.ReferredToSegmentCount > 0 {
		segment.ReferredToSegmentNumbers = make([]uint32, segment.ReferredToSegmentCount)
		for i := int32(0); i < segment.ReferredToSegmentCount; i++ {
			switch cSSize {
			case 1:
				if val, err := d.stream.Read1Byte(); err != nil {
					return ResultFailure
				} else {
					segment.ReferredToSegmentNumbers[i] = uint32(val)
				}
			case 2:
				if val, err := d.stream.ReadShortInteger(); err != nil {
					return ResultFailure
				} else {
					segment.ReferredToSegmentNumbers[i] = uint32(val)
				}
			case 4:
				if val, err := d.stream.ReadInteger(); err != nil {
					return ResultFailure
				} else {
					segment.ReferredToSegmentNumbers[i] = val
				}
			}
			if !d.randomAccess && segment.ReferredToSegmentNumbers[i] >= segment.Number {
				return ResultFailure
			}
		}
	}
	if cPSize == 1 {
		if val, err := d.stream.Read1Byte(); err != nil {
			return ResultFailure
		} else {
			segment.PageAssociation = uint32(val)
		}
	} else {
		if val, err := d.stream.ReadInteger(); err != nil {
			return ResultFailure
		} else {
			segment.PageAssociation = val
		}
	}
	if val, err := d.stream.ReadInteger(); err != nil {
		return ResultFailure
	} else {
		segment.DataLength = val
	}
	segment.Key = d.stream.GetKey()
	segment.DataOffset = d.stream.GetOffset()
	segment.State = JBig2SegmentDataUnparsed
	return ResultSuccess
}

// FindSegmentByNumber 按编号查找段
// 优先查询全局段，返回内部对象而非副本，未找到时返回nil
// 入参: number 段编号
// 返回: *Segment 段
func (d *Document) FindSegmentByNumber(number uint32) *Segment {
	if d.globalContext != nil {
		if seg := d.globalContext.FindSegmentByNumber(number); seg != nil {
			return seg
		}
	}
	return d.segmentIndex[number]
}

// ParseSegmentData 解析段数据
// 入参: segment 段
// 返回: Result 解析状态
func (d *Document) ParseSegmentData(segment *Segment) Result {
	if err := checkContext(d.ctx); err != nil {
		return d.fail(err)
	}
	if err := d.budget.checkSegmentLength(segment.DataLength); err != nil {
		return d.fail(err)
	}
	if segment.DataLength != 0xFFFFFFFF && segment.DataLength > d.stream.GetByteLeft() {
		return ResultFailure
	}
	if segment.DataLength != 0xFFFFFFFF {
		data := d.stream.data
		d.stream.data = data[:d.stream.GetOffset()+segment.DataLength]
		defer func() { d.stream.data = data }()
	}
	var ret Result
	switch segment.Flags.Type {
	case 0:
		ret = d.parseSymbolDict(segment)
	case 4, 6, 7:
		if !d.inPage {
			return ResultFailure
		}
		ret = d.parseTextRegion(segment)
	case 16:
		ret = d.parsePatternDict(segment)
	case 20, 22, 23:
		if !d.inPage {
			return ResultFailure
		}
		ret = d.parseHalftoneRegion(segment)
	case 36, 38, 39:
		if !d.inPage {
			return ResultFailure
		}
		ret = d.parseGenericRegion(segment)
	case 40, 42, 43:
		if !d.inPage {
			return ResultFailure
		}
		ret = d.parseGenericRefinementRegion(segment)
	case 48:
		ret = d.parsePageInfo(segment)
	case 49:
		d.inPage = false
		return ResultPageCompleted
	case 50:
		ret = d.parseEndOfStripe()
	case 51:
		return ResultEndReached
	case 52:
		d.stream.AddOffset(segment.DataLength)
		ret = ResultSuccess
	case 53:
		ret = d.parseTable(segment)
	case 54:
		ret = d.parseColorPalette(segment)
	case 62:
		d.stream.AddOffset(segment.DataLength)
		ret = ResultSuccess
	default:
		d.stream.AddOffset(segment.DataLength)
		ret = ResultSuccess
	}
	if ret == ResultSuccess {
		if err := d.chargeSegmentMemory(segment); err != nil {
			return d.fail(err)
		}
	}
	return ret
}

// DecodeSequential 按顺序解析段，直到当前页完成或遇到错误
// 返回: Result 解析状态
func (d *Document) DecodeSequential() Result {
	if err := checkContext(d.ctx); err != nil {
		return d.fail(err)
	}
	if d.stream.GetByteLeft() <= 0 {
		return ResultEndReached
	}
	if d.Grouped {
		return d.decodeGrouped()
	}
	for d.stream.GetByteLeft() > 0 {
		if err := checkContext(d.ctx); err != nil {
			return d.fail(err)
		}
		if d.segment == nil {
			d.segment = NewSegment()
			ret := d.ParseSegmentHeader(d.segment)
			if ret != ResultSuccess {
				d.segment = nil
				return ret
			}
			d.offset = d.stream.GetOffset()
		}
		ret := d.ParseSegmentData(d.segment)
		if ret == ResultEndReached || ret == ResultPageCompleted {
			d.addSegment(d.segment)
			d.segment = nil
			return ret
		}
		if ret != ResultSuccess {
			d.segment = nil
			return ret
		}
		if d.segment.DataLength != 0xFFFFFFFF {
			d.stream.SetOffset(d.offset + d.segment.DataLength)
		} else {
			d.stream.AddOffset(4)
		}
		seg := d.segment
		d.addSegment(seg)
		d.releaseDeferredNonRetained(seg, -1)
		d.segment = nil
	}
	return ResultSuccess
}

// decodeGrouped 先解析段头，再按段偏移解码当前页数据
// 返回: Result 解析状态
func (d *Document) decodeGrouped() Result {
	if !d.groupedParsed {
		d.groupedLastRef = make(map[uint32]int)
		for d.stream.GetByteLeft() > 0 {
			if len(d.segmentList)&63 == 0 {
				if err := checkContext(d.ctx); err != nil {
					return d.fail(err)
				}
			}
			seg := NewSegment()
			ret := d.ParseSegmentHeader(seg)
			if ret != ResultSuccess {
				return ret
			}
			idx := len(d.segmentList)
			d.addSegment(seg)
			for _, refNum := range seg.ReferredToSegmentNumbers {
				d.groupedLastRef[refNum] = idx
			}
			if seg.Flags.Type == 51 {
				break
			}
		}
		d.groupedOffset = d.stream.GetOffset()
		d.groupedParsed = true
	}
	for d.groupedIndex < len(d.segmentList) {
		if err := checkContext(d.ctx); err != nil {
			return d.fail(err)
		}
		seg := d.segmentList[d.groupedIndex]
		if seg.Flags.Type == 51 {
			d.groupedIndex++
			return ResultEndReached
		}
		if seg.DataLength == 0 && seg.Flags.Type != 49 {
			d.groupedIndex++
			continue
		}
		d.stream.SetOffset(d.groupedOffset)
		d.segment = seg
		d.offset = d.groupedOffset
		ret := d.ParseSegmentData(seg)
		if ret == ResultFailure {
			return ResultFailure
		}
		if seg.DataLength != 0xFFFFFFFF {
			d.groupedOffset += seg.DataLength
			d.stream.SetOffset(d.groupedOffset)
		} else {
			d.stream.AddOffset(4)
			d.groupedOffset = d.stream.GetOffset()
		}
		d.releaseDeferredNonRetained(seg, d.groupedIndex)
		d.segment = nil
		d.groupedIndex++
		if ret == ResultPageCompleted || ret == ResultEndReached {
			return ret
		}
	}
	return ResultEndReached
}

// parseSymbolDict 解析符号字典段
// 入参: segment 段
// 返回: Result 解析结果
func (d *Document) parseSymbolDict(segment *Segment) Result {
	refNumbers := segment.ReferredToSegmentNumbers
	var flags uint16
	if val, err := d.stream.ReadShortInteger(); err != nil {
		return ResultFailure
	} else {
		flags = val
	}
	sdd := NewSDDProc()
	sdd.SDHUFF = (flags & 0x0001) != 0
	sdd.SDREFAGG = (flags & 0x0002) != 0
	sdd.SDTEMPLATE = uint8((flags >> 10) & 0x0003)
	sdd.SDRTEMPLATE = (flags & 0x1000) != 0
	if sdd.SDREFAGG && len(refNumbers) == 0 {
		if ref := d.findImplicitSymbolDict(segment.PageAssociation); ref != nil {
			refNumbers = []uint32{ref.Number}
			sdd.implicitRefinement = true
		}
	}
	if !sdd.SDHUFF {
		dwTemp := 2
		if sdd.SDTEMPLATE == 0 {
			dwTemp = 8
		}
		for i := 0; i < dwTemp; i++ {
			if val, err := d.stream.Read1Byte(); err != nil {
				return ResultFailure
			} else {
				sdd.SDAT[i] = int8(val)
			}
		}
	}
	if sdd.SDREFAGG && !sdd.SDRTEMPLATE {
		for i := 0; i < 4; i++ {
			if val, err := d.stream.Read1Byte(); err != nil {
				return ResultFailure
			} else {
				sdd.SDRAT[i] = int8(val)
			}
		}
	}
	if val, err := d.stream.ReadInteger(); err != nil {
		return ResultFailure
	} else {
		sdd.SDNUMEXSYMS = val
	}
	if val, err := d.stream.ReadInteger(); err != nil {
		return ResultFailure
	} else {
		sdd.SDNUMNEWSYMS = val
	}
	if sdd.SDNUMNEWSYMS > JBig2MaxNewSymbols || sdd.SDNUMEXSYMS > JBig2MaxExportSymbols {
		return ResultFailure
	}
	var inputSymbols []*Image
	if len(refNumbers) > 0 {
		for _, refNum := range refNumbers {
			seg := d.FindSegmentByNumber(refNum)
			if seg == nil {
				return ResultFailure
			}
			if seg.Flags.Type == 0 && seg.SymbolDict != nil {
				inputSymbols = append(inputSymbols, seg.SymbolDict.Images...)
			}
		}
	}
	sdd.SDINSYMS = inputSymbols
	sdd.SDNUMINSYMS = uint32(len(inputSymbols))
	if sdd.SDNUMEXSYMS > sdd.SDNUMINSYMS+sdd.SDNUMNEWSYMS {
		return ResultFailure
	}
	if sdd.SDHUFF {
		cSDHUFFDH := (flags >> 2) & 0x0003
		cSDHUFFDW := (flags >> 4) & 0x0003
		cSDHUFFBMSIZE := (flags >> 6) & 0x0001
		cSDHUFFAGGINST := (flags >> 7) & 0x0001
		if cSDHUFFDH == 2 || cSDHUFFDW == 2 {
			return ResultFailure
		}
		var tableSegments []*Segment
		for _, refNum := range refNumbers {
			seg := d.FindSegmentByNumber(refNum)
			if seg != nil && seg.Flags.Type == 53 {
				tableSegments = append(tableSegments, seg)
			}
		}
		tableIdx := 0
		if cSDHUFFDH == 0 {
			sdd.SDHUFFDH = NewStandardTable(4)
		} else if cSDHUFFDH == 1 {
			sdd.SDHUFFDH = NewStandardTable(5)
		} else {
			if tableIdx < len(tableSegments) {
				sdd.SDHUFFDH = tableSegments[tableIdx].HuffmanTable
				tableIdx++
			} else {
				return ResultFailure
			}
		}
		if cSDHUFFDW == 0 {
			sdd.SDHUFFDW = NewStandardTable(2)
		} else if cSDHUFFDW == 1 {
			sdd.SDHUFFDW = NewStandardTable(3)
		} else {
			if tableIdx < len(tableSegments) {
				sdd.SDHUFFDW = tableSegments[tableIdx].HuffmanTable
				tableIdx++
			} else {
				return ResultFailure
			}
		}
		if cSDHUFFBMSIZE == 0 {
			sdd.SDHUFFBMSIZE = NewStandardTable(1)
		} else {
			if tableIdx < len(tableSegments) {
				sdd.SDHUFFBMSIZE = tableSegments[tableIdx].HuffmanTable
				tableIdx++
			} else {
				return ResultFailure
			}
		}
		if sdd.SDREFAGG {
			if cSDHUFFAGGINST == 0 {
				sdd.SDHUFFAGGINST = NewStandardTable(1)
			} else {
				if tableIdx < len(tableSegments) {
					sdd.SDHUFFAGGINST = tableSegments[tableIdx].HuffmanTable
					tableIdx++
				} else {
					return ResultFailure
				}
			}
		}
	}
	gbContextSize := 0
	grContextSize := 0
	if !sdd.SDHUFF {
		if sdd.SDTEMPLATE == 0 {
			gbContextSize = 65536
		} else {
			gbContextSize = 8192
		}
	}
	if sdd.SDREFAGG {
		if sdd.SDRTEMPLATE {
			grContextSize = 1024
		} else {
			grContextSize = 8192
		}
	}
	var gbContexts, grContexts []ArithCtx
	contextUsed := (flags & 0x0100) != 0
	if contextUsed {
		for i := len(refNumbers) - 1; i >= 0; i-- {
			refSeg := d.FindSegmentByNumber(refNumbers[i])
			if refSeg == nil || refSeg.Flags.Type != 0 {
				continue
			}
			if len(refSeg.GBContexts) == gbContextSize {
				gbContexts = append([]ArithCtx(nil), refSeg.GBContexts...)
			}
			if len(refSeg.GRContexts) == grContextSize {
				grContexts = append([]ArithCtx(nil), refSeg.GRContexts...)
			}
			break
		}
	}
	if gbContexts == nil {
		gbContexts = make([]ArithCtx, gbContextSize)
	}
	if grContexts == nil {
		grContexts = make([]ArithCtx, grContextSize)
	}
	sdd.ctx = d.ctx
	sdd.budget = d.budget
	var err error
	if sdd.SDHUFF {
		segment.SymbolDict, err = sdd.DecodeHuffman(d.stream, gbContexts, grContexts)
		d.stream.AlignByte()
	} else {
		arithDecoder := NewArithDecoder(d.stream)
		segment.SymbolDict, err = sdd.DecodeArith(arithDecoder, gbContexts, grContexts)
		d.stream.AlignByte()
		d.stream.AddOffset(2)
	}
	if err != nil {
		return d.fail(err)
	}
	if (flags & 0x0200) != 0 {
		segment.GBContexts = gbContexts
		segment.GRContexts = grContexts
	}
	segment.ResultType = JBig2SymbolDictPointer
	return ResultSuccess
}

// findImplicitSymbolDict 查找同页可隐式引用的符号字典
// 入参: page 页号
// 返回: *Segment 符号字典段
func (d *Document) findImplicitSymbolDict(page uint32) *Segment {
	for i := len(d.segmentList) - 1; i >= 0; i-- {
		seg := d.segmentList[i]
		if seg.PageAssociation == page && seg.SymbolDict != nil {
			if seg.SymbolDict.implicitReferences {
				return seg
			}
			return nil
		}
	}
	return nil
}

// ParseRegionInfo 解析区域信息
// 入参: ri 区域信息
// 返回: Result 解析状态
func (d *Document) ParseRegionInfo(ri *RegionInfo) Result {
	if val, err := d.stream.ReadInteger(); err != nil {
		return ResultFailure
	} else {
		ri.Width = int32(val)
	}
	if val, err := d.stream.ReadInteger(); err != nil {
		return ResultFailure
	} else {
		ri.Height = int32(val)
	}
	if val, err := d.stream.ReadInteger(); err != nil {
		return ResultFailure
	} else {
		ri.X = int32(val)
	}
	if val, err := d.stream.ReadInteger(); err != nil {
		return ResultFailure
	} else {
		ri.Y = int32(val)
	}
	if val, err := d.stream.Read1Byte(); err != nil {
		return ResultFailure
	} else {
		ri.Flags = val
	}
	if ri.Flags&0x08 != 0 && d.colorPage == nil {
		return ResultFailure
	}
	return ResultSuccess
}

// composeOpFromRegionFlags 从区域标志获取组合操作
// 入参: flags 区域标志
// 返回: ComposeOp 组合操作
func composeOpFromRegionFlags(flags uint8) ComposeOp {
	if (flags & 0x07) == 4 {
		return ComposeReplace
	}
	return ComposeOp(flags & 0x03)
}

// canDecodeTextRegionIntoPage 检查文本区域是否可直接解码到页面
// 入参: segment 段, ri 区域信息, trd 文本区域解码器
// 返回: bool 是否可直接解码
func (d *Document) canDecodeTextRegionIntoPage(segment *Segment, ri *RegionInfo, trd *TRDProc) bool {
	if d.colorPage != nil || d.pageWritten || segment.Flags.Type == 4 || d.page == nil || len(d.pageInfoList) == 0 {
		return false
	}
	pi := d.pageInfoList[len(d.pageInfoList)-1]
	if pi.IsStriped || pi.Height == 0xFFFFFFFF || pi.DefaultPixelValue != trd.SBDEFPIXEL ||
		ri.X != 0 || ri.Y != 0 || ri.Width != d.page.Width() || ri.Height != d.page.Height() {
		return false
	}
	op := composeOpFromRegionFlags(ri.Flags)
	if op == ComposeReplace {
		return true
	}
	if pi.DefaultPixelValue {
		return op == ComposeAnd || op == ComposeXnor
	}
	return op == ComposeOr || op == ComposeXor
}

// expandPageForRegion 根据区域扩展页面
// 入参: ri 区域信息
// 返回: error 错误信息
func (d *Document) expandPageForRegion(ri *RegionInfo) error {
	if d.page == nil || len(d.pageInfoList) == 0 {
		return nil
	}
	pi := d.pageInfoList[len(d.pageInfoList)-1]
	if pi.Height != 0xFFFFFFFF {
		return nil
	}
	newHeight := int64(ri.Y) + int64(ri.Height)
	if newHeight <= int64(d.page.Height()) {
		return nil
	}
	if newHeight > int64(1<<31-1) {
		return errors.New("expanded page height overflow")
	}
	h32 := int32(newHeight)
	if err := d.resizePageMemory(d.page.Width(), h32, d.colorPage != nil); err != nil {
		return err
	}
	d.page.Expand(h32, pi.DefaultPixelValue)
	if d.page.Height() < h32 {
		return errors.New("failed to expand page")
	}
	if d.colorPage != nil && int(h32) > d.colorPage.Rect.Dy() {
		grown := newColorImage(d.page.Width(), h32)
		if grown == nil {
			return errors.New("failed to expand color page")
		}
		copy(grown.Pix, d.colorPage.Pix)
		d.colorPage = grown
	}
	return nil
}

// GetHuffmanTable 创建标准霍夫曼表
// 与NewStandardTable相同，不查询文档中的自定义表
// 入参: idx 标准表索引，范围为1至15
// 返回: *HuffmanTable 霍夫曼表
func (d *Document) GetHuffmanTable(idx int) *HuffmanTable {
	return NewStandardTable(idx)
}

// DecodeSymbolIDHuffmanTable 解码符号ID霍夫曼表
// 推进文档位流，解码失败时返回nil
// 入参: SBNUMSYMS 符号数
// 返回: []HuffmanCode 霍夫曼编码切片
func (d *Document) DecodeSymbolIDHuffmanTable(SBNUMSYMS uint32) []HuffmanCode {
	return d.decodeSymbolIDHuffmanTable(SBNUMSYMS, false)
}

// decodeSymbolIDHuffmanTable 解码符号ID及可选的游程前缀
// 入参: SBNUMSYMS 符号数, includeRunCode 是否保留游程前缀
// 返回: []HuffmanCode 霍夫曼编码切片
func (d *Document) decodeSymbolIDHuffmanTable(SBNUMSYMS uint32, includeRunCode bool) []HuffmanCode {
	kRunCodesSize := 35
	huffmanCodes := make([]HuffmanCode, kRunCodesSize)
	for i := 0; i < kRunCodesSize; i++ {
		val, err := d.stream.ReadNBits(4)
		if err != nil {
			return nil
		}
		huffmanCodes[i].Codelen = int32(val)
	}
	runDecoder, err := newHuffmanCodeIndex(huffmanCodes)
	if err != nil {
		return nil
	}
	SBSYMCODES := make([]HuffmanCode, SBNUMSYMS)
	i := int32(0)
	for i < int32(SBNUMSYMS) {
		j, err := runDecoder.Decode(d.stream)
		if err != nil {
			return nil
		}
		runcode := int32(j)
		var run int32
		if runcode < 32 {
			SBSYMCODES[i].Codelen = runcode
			if includeRunCode {
				SBSYMCODES[i].Codelen += huffmanCodes[j].Codelen
			}
			run = 0
		} else if runcode == 32 {
			val, err := d.stream.ReadNBits(2)
			if err != nil {
				return nil
			}
			run = int32(val) + 3
		} else if runcode == 33 {
			val, err := d.stream.ReadNBits(3)
			if err != nil {
				return nil
			}
			run = int32(val) + 3
		} else if runcode == 34 {
			val, err := d.stream.ReadNBits(7)
			if err != nil {
				return nil
			}
			run = int32(val) + 11
		}
		if run > 0 {
			if i+run > int32(SBNUMSYMS) {
				return nil
			}
			for k := int32(0); k < run; k++ {
				if runcode == 32 && i > 0 {
					SBSYMCODES[i+k].Codelen = SBSYMCODES[i-1].Codelen
				} else {
					SBSYMCODES[i+k].Codelen = 0
				}
			}
			i += run
		} else {
			i++
		}
	}
	if err := HuffmanAssignCode(SBSYMCODES); err != nil {
		return nil
	}
	return SBSYMCODES
}

// parseTextRegion 解析文本区域段
// 入参: segment 段
// 返回: Result 解析结果
func (d *Document) parseTextRegion(segment *Segment) Result {
	start := *d.stream
	result := d.parseTextRegionData(segment, false)
	if result != ResultFailure || checkContext(d.ctx) != nil || errors.Is(d.err, ErrLimitExceeded) || start.GetByteLeft() < 19 ||
		readUint16(start.data[start.GetOffset()+17:], start.littleEndian)&1 == 0 {
		return result
	}
	d.err = nil
	*d.stream = start
	return d.parseTextRegionData(segment, true)
}

// parseTextRegionData 解析文本区域数据
// 入参: segment 段, includeRunCode 是否保留游程前缀
// 返回: Result 解析结果
func (d *Document) parseTextRegionData(segment *Segment, includeRunCode bool) Result {
	refNumbers := segment.ReferredToSegmentNumbers
	implicit := false
	if len(refNumbers) == 0 {
		if ref := d.findImplicitSymbolDict(segment.PageAssociation); ref != nil {
			refNumbers = []uint32{ref.Number}
			implicit = true
		}
	}
	var ri RegionInfo
	if d.ParseRegionInfo(&ri) != ResultSuccess {
		return ResultFailure
	}
	var flags uint16
	if val, err := d.stream.ReadShortInteger(); err != nil {
		return ResultFailure
	} else {
		flags = val
	}
	pTRD := NewTRDProc()
	pTRD.ctx = d.ctx
	pTRD.budget = d.budget
	pTRD.implicitRefinement = implicit
	pTRD.SBW = uint32(ri.Width)
	pTRD.SBH = uint32(ri.Height)
	pTRD.SBHUFF = (flags & 0x0001) != 0
	pTRD.SBREFINE = ((flags >> 1) & 0x0001) != 0
	dwTemp := (flags >> 2) & 0x0003
	pTRD.SBSTRIPS = 1 << dwTemp
	pTRD.REFCORNER = JBig2Corner((flags >> 4) & 0x0003)
	pTRD.TRANSPOSED = ((flags >> 6) & 0x0001) != 0
	pTRD.SBCOMBOP = ComposeOp((flags >> 7) & 0x0003)
	pTRD.SBDEFPIXEL = ((flags >> 9) & 0x0001) != 0
	pTRD.SBDSOFFSET = int8((flags >> 10) & 0x001F)
	if pTRD.SBDSOFFSET >= 0x10 {
		pTRD.SBDSOFFSET = pTRD.SBDSOFFSET - 0x20
	}
	pTRD.SBRTEMPLATE = ((flags >> 15) & 0x0001) != 0
	var huffFlags uint16
	if pTRD.SBHUFF {
		if val, err := d.stream.ReadShortInteger(); err != nil {
			return ResultFailure
		} else {
			huffFlags = val
		}
	}
	if pTRD.SBREFINE && !pTRD.SBRTEMPLATE {
		for i := 0; i < 4; i++ {
			if val, err := d.stream.Read1Byte(); err != nil {
				return ResultFailure
			} else {
				pTRD.SBRAT[i] = int8(val)
			}
		}
	}
	if val, err := d.stream.ReadInteger(); err != nil {
		return ResultFailure
	} else {
		pTRD.SBNUMINSTANCES = val
	}
	referenceColors := false
	if ri.Flags&0x08 != 0 {
		data := d.stream.data
		if segment.DataLength == 0xFFFFFFFF || d.stream.GetByteLeft() < 10 {
			return ResultFailure
		}
		size := readUint32(data[len(data)-4:], d.stream.littleEndian)
		if size < 10 || size > d.stream.GetByteLeft() {
			return ResultFailure
		}
		palette, err := d.getColorPalette(segment)
		if err != nil {
			return ResultFailure
		}
		start := len(data) - int(size)
		stream := NewBitStream(data[start:len(data)-4], 0)
		stream.SetLittleEndian(d.stream.littleEndian)
		pTRD.colorRuns, referenceColors, err = decodeColorRuns(stream, pTRD.SBNUMINSTANCES, palette, implicit)
		if err != nil {
			return ResultFailure
		}
		if _, err := d.budget.checkImage(ri.Width, ri.Height, true); err != nil {
			return d.fail(err)
		}
		pTRD.colorImage = newColorImage(ri.Width, ri.Height)
		if pTRD.colorImage == nil {
			return ResultFailure
		}
		d.stream.data = data[:start]
		defer func() { d.stream.data = data }()
	}
	if len(refNumbers) > 0 {
		for _, refNum := range refNumbers {
			if d.FindSegmentByNumber(refNum) == nil {
				return ResultFailure
			}
		}
	}
	dwNumSyms := uint32(0)
	var singleDict *SymbolDict
	dictCount := 0
	for _, refNum := range refNumbers {
		seg := d.FindSegmentByNumber(refNum)
		if seg != nil && seg.Flags.Type == 0 && seg.SymbolDict != nil {
			dwNumSyms += uint32(seg.SymbolDict.NumImages())
			singleDict = seg.SymbolDict
			dictCount++
		}
	}
	pTRD.SBNUMSYMS = dwNumSyms
	if dictCount == 1 {
		pTRD.SBSYMS = singleDict.Images
	} else {
		SBSYMS := make([]*Image, pTRD.SBNUMSYMS)
		dwNumSyms = 0
		for _, refNum := range refNumbers {
			seg := d.FindSegmentByNumber(refNum)
			if seg != nil && seg.Flags.Type == 0 && seg.SymbolDict != nil {
				dict := seg.SymbolDict
				for j := 0; j < dict.NumImages(); j++ {
					SBSYMS[dwNumSyms+uint32(j)] = dict.GetImage(j)
				}
				dwNumSyms += uint32(dict.NumImages())
			}
		}
		pTRD.SBSYMS = SBSYMS
	}
	if pTRD.SBHUFF {
		if encodedTable := d.decodeSymbolIDHuffmanTable(pTRD.SBNUMSYMS, includeRunCode); encodedTable != nil {
			d.stream.AlignByte()
			pTRD.SBSYMCODES = encodedTable
		} else {
			return ResultFailure
		}
	} else {
		pTRD.SBSYMCODELEN = ceilLog2(pTRD.SBNUMSYMS)
	}
	if pTRD.SBHUFF {
		cSBHUFFFS := huffFlags & 0x0003
		cSBHUFFDS := (huffFlags >> 2) & 0x0003
		cSBHUFFDT := (huffFlags >> 4) & 0x0003
		cSBHUFFRDW := (huffFlags >> 6) & 0x0003
		cSBHUFFRDH := (huffFlags >> 8) & 0x0003
		cSBHUFFRDX := (huffFlags >> 10) & 0x0003
		cSBHUFFRDY := (huffFlags >> 12) & 0x0003
		cSBHUFFRSIZE := (huffFlags >> 14) & 0x0001
		if !pTRD.SBREFINE {
			cSBHUFFRDW = 0
			cSBHUFFRDH = 0
			cSBHUFFRDX = 0
			cSBHUFFRDY = 0
			cSBHUFFRSIZE = 0
		}
		if cSBHUFFFS == 2 || cSBHUFFRDW == 2 || cSBHUFFRDH == 2 || cSBHUFFRDX == 2 || cSBHUFFRDY == 2 {
			return ResultFailure
		}
		tableIdx := 0
		var tableSegments []*Segment
		for _, refNum := range refNumbers {
			seg := d.FindSegmentByNumber(refNum)
			if seg != nil && seg.Flags.Type == 53 {
				tableSegments = append(tableSegments, seg)
			}
		}
		getUserTable := func() *HuffmanTable {
			if tableIdx < len(tableSegments) {
				t := tableSegments[tableIdx].HuffmanTable
				tableIdx++
				return t
			}
			return nil
		}
		if cSBHUFFFS == 0 {
			pTRD.SBHUFFFS = d.GetHuffmanTable(6)
		} else if cSBHUFFFS == 1 {
			pTRD.SBHUFFFS = d.GetHuffmanTable(7)
		} else {
			pTRD.SBHUFFFS = getUserTable()
		}
		if cSBHUFFDS == 0 {
			pTRD.SBHUFFDS = d.GetHuffmanTable(8)
		} else if cSBHUFFDS == 1 {
			pTRD.SBHUFFDS = d.GetHuffmanTable(9)
		} else if cSBHUFFDS == 2 {
			pTRD.SBHUFFDS = d.GetHuffmanTable(10)
		} else {
			pTRD.SBHUFFDS = getUserTable()
		}
		if cSBHUFFDT == 0 {
			pTRD.SBHUFFDT = d.GetHuffmanTable(11)
		} else if cSBHUFFDT == 1 {
			pTRD.SBHUFFDT = d.GetHuffmanTable(12)
		} else if cSBHUFFDT == 2 {
			pTRD.SBHUFFDT = d.GetHuffmanTable(13)
		} else {
			pTRD.SBHUFFDT = getUserTable()
		}
		if cSBHUFFRDW == 0 {
			pTRD.SBHUFFRDW = d.GetHuffmanTable(14)
		} else if cSBHUFFRDW == 1 {
			pTRD.SBHUFFRDW = d.GetHuffmanTable(15)
		} else {
			pTRD.SBHUFFRDW = getUserTable()
		}
		if cSBHUFFRDH == 0 {
			pTRD.SBHUFFRDH = d.GetHuffmanTable(14)
		} else if cSBHUFFRDH == 1 {
			pTRD.SBHUFFRDH = d.GetHuffmanTable(15)
		} else {
			pTRD.SBHUFFRDH = getUserTable()
		}
		if cSBHUFFRDX == 0 {
			pTRD.SBHUFFRDX = d.GetHuffmanTable(14)
		} else if cSBHUFFRDX == 1 {
			pTRD.SBHUFFRDX = d.GetHuffmanTable(15)
		} else {
			pTRD.SBHUFFRDX = getUserTable()
		}
		if cSBHUFFRDY == 0 {
			pTRD.SBHUFFRDY = d.GetHuffmanTable(14)
		} else if cSBHUFFRDY == 1 {
			pTRD.SBHUFFRDY = d.GetHuffmanTable(15)
		} else {
			pTRD.SBHUFFRDY = getUserTable()
		}
		if cSBHUFFRSIZE == 0 {
			pTRD.SBHUFFRSIZE = d.GetHuffmanTable(1)
		} else {
			pTRD.SBHUFFRSIZE = getUserTable()
		}
		if pTRD.SBHUFFFS == nil || pTRD.SBHUFFDS == nil || pTRD.SBHUFFDT == nil ||
			pTRD.SBHUFFRDW == nil || pTRD.SBHUFFRDH == nil || pTRD.SBHUFFRDX == nil ||
			pTRD.SBHUFFRDY == nil || pTRD.SBHUFFRSIZE == nil {
			return ResultFailure
		}
	}
	var grContexts []ArithCtx
	if pTRD.SBREFINE {
		size := 8192
		if pTRD.SBRTEMPLATE {
			size = 1024
		}
		grContexts = make([]ArithCtx, size)
	}
	segment.ResultType = JBig2ImagePointer
	directToPage := d.canDecodeTextRegionIntoPage(segment, &ri, pTRD)
	var target *Image
	if directToPage {
		target = d.page
		if includeRunCode {
			target.Fill(pTRD.SBDEFPIXEL)
		}
	}
	var err error
	if pTRD.SBHUFF {
		var img *Image
		img, err = pTRD.decodeHuffmanInto(d.stream, grContexts, target)
		if err == nil {
			segment.Image = img
			d.stream.AlignByte()
		}
	} else {
		arithDecoder := NewArithDecoder(d.stream)
		var img *Image
		img, err = pTRD.decodeArithInto(arithDecoder, grContexts, nil, target)
		if err == nil {
			segment.Image = img
			d.stream.AlignByte()
			d.stream.AddOffset(2)
		}
	}
	if err != nil || segment.Image == nil {
		return d.fail(err)
	}
	segment.colorImage = pTRD.colorImage
	d.referenceColors = d.referenceColors || referenceColors
	if segment.Flags.Type != 4 {
		if !directToPage {
			if err := d.expandPageForRegion(&ri); err != nil {
				return d.fail(err)
			}
			if d.colorPage == nil {
				d.page.ComposeFrom(ri.X, ri.Y, segment.Image, composeOpFromRegionFlags(ri.Flags))
			} else if d.composeColorRegion(segment, &ri) != ResultSuccess {
				return ResultFailure
			}
		}
		segment.Image = nil
		segment.colorImage = nil
		d.pageWritten = true
	}
	return ResultSuccess
}

// parsePatternDict 解析模式字典段
// 入参: segment 段
// 返回: Result 解析结果
func (d *Document) parsePatternDict(segment *Segment) Result {
	var flags byte
	pPDD := NewPDDProc()
	pPDD.ctx = d.ctx
	pPDD.budget = d.budget
	if val, err := d.stream.Read1Byte(); err != nil {
		return ResultFailure
	} else {
		flags = val
	}
	if val, err := d.stream.Read1Byte(); err != nil {
		return ResultFailure
	} else {
		pPDD.HDPW = val
	}
	if val, err := d.stream.Read1Byte(); err != nil {
		return ResultFailure
	} else {
		pPDD.HDPH = val
	}
	if val, err := d.stream.ReadInteger(); err != nil {
		return ResultFailure
	} else {
		pPDD.GRAYMAX = val
	}
	if pPDD.GRAYMAX > JBig2MaxPatternIndex {
		return ResultFailure
	}
	pPDD.HDMMR = (flags & 0x01) != 0
	pPDD.HDTEMPLATE = (flags >> 1) & 0x03
	segment.ResultType = JBig2PatternDictPointer
	var err error
	if pPDD.HDMMR {
		segment.PatternDict, err = pPDD.DecodeMMR(d.stream)
		if err != nil {
			return d.fail(err)
		}
		d.stream.AlignByte()
	} else {
		size := 1024
		if pPDD.HDTEMPLATE == 0 {
			size = 65536
		} else if pPDD.HDTEMPLATE == 1 {
			size = 8192
		}
		gbContexts := make([]ArithCtx, size)
		arithDecoder := NewArithDecoder(d.stream)
		segment.PatternDict, err = pPDD.DecodeArith(arithDecoder, gbContexts)
		if err != nil {
			return d.fail(err)
		}
		d.stream.AlignByte()
		d.stream.AddOffset(2)
	}
	return ResultSuccess
}

// parseHalftoneRegion 解析半色调区域段
// 入参: segment 段
// 返回: Result 解析结果
func (d *Document) parseHalftoneRegion(segment *Segment) Result {
	var ri RegionInfo
	var flags byte
	pHRD := NewHTRDProc()
	pHRD.ctx = d.ctx
	pHRD.budget = d.budget
	if d.ParseRegionInfo(&ri) != ResultSuccess {
		return ResultFailure
	}
	if ri.Flags&0x08 != 0 {
		return ResultFailure
	}
	if val, err := d.stream.Read1Byte(); err != nil {
		return ResultFailure
	} else {
		flags = val
	}
	if val, err := d.stream.ReadInteger(); err != nil {
		return ResultFailure
	} else {
		pHRD.HGW = val
	}
	if val, err := d.stream.ReadInteger(); err != nil {
		return ResultFailure
	} else {
		pHRD.HGH = val
	}
	if val, err := d.stream.ReadInteger(); err != nil {
		return ResultFailure
	} else {
		pHRD.HGX = int32(val)
	}
	if val, err := d.stream.ReadInteger(); err != nil {
		return ResultFailure
	} else {
		pHRD.HGY = int32(val)
	}
	if val, err := d.stream.ReadShortInteger(); err != nil {
		return ResultFailure
	} else {
		pHRD.HRX = uint16(val)
	}
	if val, err := d.stream.ReadShortInteger(); err != nil {
		return ResultFailure
	} else {
		pHRD.HRY = uint16(val)
	}
	pHRD.HBW = uint32(ri.Width)
	pHRD.HBH = uint32(ri.Height)
	pHRD.HMMR = (flags & 0x01) != 0
	pHRD.HTEMPLATE = (flags >> 1) & 0x03
	pHRD.HENABLESKIP = ((flags >> 3) & 0x01) != 0
	pHRD.HCOMBOP = ComposeOp((flags >> 4) & 0x07)
	pHRD.HDEFPIXEL = ((flags >> 7) & 0x01) != 0
	if segment.ReferredToSegmentCount != 1 {
		return ResultFailure
	}
	seg := d.FindSegmentByNumber(segment.ReferredToSegmentNumbers[0])
	if seg == nil || seg.Flags.Type != 16 || seg.PatternDict == nil {
		return ResultFailure
	}
	pPatternDict := seg.PatternDict
	if pPatternDict.NUMPATS == 0 {
		return ResultFailure
	}
	pHRD.HNUMPATS = pPatternDict.NUMPATS
	pHRD.HPATS = pPatternDict.HDPATS
	pHRD.HPW = uint8(pPatternDict.HDPATS[0].Width())
	pHRD.HPH = uint8(pPatternDict.HDPATS[0].Height())
	segment.ResultType = JBig2ImagePointer
	var err error
	if pHRD.HMMR {
		d.stream.AlignByte()
		segment.Image, err = pHRD.DecodeMMR(d.stream)
		if err != nil {
			return d.fail(err)
		}
		d.stream.AlignByte()
	} else {
		size := GetHuffContextSize(pHRD.HTEMPLATE)
		gbContexts := make([]ArithCtx, size)
		arithDecoder := NewArithDecoder(d.stream)
		segment.Image, err = pHRD.DecodeArith(arithDecoder, gbContexts)
		if err != nil {
			return d.fail(err)
		}
		d.stream.AlignByte()
		d.stream.AddOffset(2)
	}
	if segment.Flags.Type != 20 {
		if err := d.expandPageForRegion(&ri); err != nil {
			return d.fail(err)
		}
		if d.colorPage == nil {
			d.page.ComposeFrom(ri.X, ri.Y, segment.Image, composeOpFromRegionFlags(ri.Flags))
		} else if d.composeColorRegion(segment, &ri) != ResultSuccess {
			return ResultFailure
		}
		segment.Image = nil
		d.pageWritten = true
	}
	return ResultSuccess
}

// canDecodeGenericRegionIntoPage 检查通用区域是否可直接写入空白页面
// 入参: segment 段, ri 区域信息
// 返回: bool 是否可直接解码
func (d *Document) canDecodeGenericRegionIntoPage(segment *Segment, ri *RegionInfo) bool {
	if d.colorPage != nil || d.pageWritten || segment.Flags.Type == 36 || ri.Flags&0x08 != 0 || d.page == nil || len(d.pageInfoList) == 0 {
		return false
	}
	pi := d.pageInfoList[len(d.pageInfoList)-1]
	if pi.IsStriped || pi.Height == 0xFFFFFFFF || pi.DefaultPixelValue || ri.X != 0 || ri.Y != 0 || ri.Width != d.page.Width() || ri.Height != d.page.Height() {
		return false
	}
	op := composeOpFromRegionFlags(ri.Flags)
	return op == ComposeOr || op == ComposeXor || op == ComposeReplace
}

// parseGenericRegion 解析通用区域段
// 入参: segment 段
// 返回: Result 解析结果
func (d *Document) parseGenericRegion(segment *Segment) Result {
	var ri RegionInfo
	var flags byte
	if d.ParseRegionInfo(&ri) != ResultSuccess {
		return ResultFailure
	}
	var foreground color.NRGBA64
	if ri.Flags&0x08 != 0 {
		data := d.stream.data
		if segment.DataLength == 0xFFFFFFFF || d.stream.GetByteLeft() < 4 {
			return ResultFailure
		}
		palette, err := d.getColorPalette(segment)
		if err != nil {
			return ResultFailure
		}
		id := readUint32(data[len(data)-4:], d.stream.littleEndian)
		if uint64(id) >= uint64(len(palette)) {
			return ResultFailure
		}
		foreground = palette[id]
		d.stream.data = data[:len(data)-4]
		defer func() { d.stream.data = data }()
	}
	if val, err := d.stream.Read1Byte(); err != nil {
		return ResultFailure
	} else {
		flags = val
	}
	pGRD := NewGRDProc()
	pGRD.ctx = d.ctx
	pGRD.budget = d.budget
	pGRD.GBW = uint32(ri.Width)
	pGRD.GBH = uint32(ri.Height)
	pGRD.MMR = (flags & 0x01) != 0
	pGRD.GBTEMPLATE = (flags >> 1) & 0x03
	pGRD.TPGDON = ((flags >> 3) & 0x01) != 0
	pGRD.EXTTEMPLATE = (flags & 0x10) != 0
	if !pGRD.MMR {
		if pGRD.GBTEMPLATE == 0 {
			for i := 0; i < 8; i++ {
				if val, err := d.stream.Read1Byte(); err != nil {
					return ResultFailure
				} else {
					pGRD.GBAT[i] = int8(val)
				}
			}
			if pGRD.EXTTEMPLATE {
				for i := range pGRD.GBATEXT {
					if val, err := d.stream.Read1Byte(); err != nil {
						return ResultFailure
					} else {
						pGRD.GBATEXT[i] = int8(val)
					}
				}
			}
		} else {
			for i := 0; i < 2; i++ {
				if val, err := d.stream.Read1Byte(); err != nil {
					return ResultFailure
				} else {
					pGRD.GBAT[i] = int8(val)
				}
			}
		}
	}
	pGRD.USESKIP = false
	segment.ResultType = JBig2ImagePointer
	direct := d.canDecodeGenericRegionIntoPage(segment, &ri)
	if pGRD.MMR {
		if segment.DataLength == 0xFFFFFFFF {
			if pGRD.GBW > JBig2MaxImageSize || pGRD.GBH > JBig2MaxImageSize {
				return ResultFailure
			}
			mmrDec := NewMMRDecompressor(int(pGRD.GBW), int(pGRD.GBH), d.stream)
			mmrDec.ctx = d.ctx
			mmrDec.budget = d.budget
			img, err := mmrDec.Uncompress()
			if err != nil {
				return d.fail(err)
			}
			if direct {
				copy(d.page.Data(), img.Data())
				segment.Image = d.page
			} else {
				segment.Image = img
				pGRD.replaceRect = Rect{0, 0, img.Width(), img.Height()}
			}
		} else if direct {
			if pGRD.GBW > JBig2MaxImageSize || pGRD.GBH > JBig2MaxImageSize {
				return ResultFailure
			}
			segment.Image = d.page
			if err := decodeG4Context(d.ctx, d.stream, segment.Image); err != nil {
				return d.fail(err)
			}
		} else {
			res := pGRD.StartDecodeMMR(&segment.Image, d.stream)
			if res != JBig2SegmentParseComplete {
				return d.fail(pGRD.err)
			}
		}
		d.stream.AlignByte()
	} else {
		size := GetHuffContextSize(pGRD.GBTEMPLATE)
		gbContexts := make([]ArithCtx, size)
		arithDecoder := NewArithDecoder(d.stream)
		var err error
		if direct {
			segment.Image = d.page
		}
		segment.Image, err = pGRD.decodeArithInto(arithDecoder, gbContexts, segment.Image)
		if err != nil {
			return d.fail(err)
		}
		if segment.DataLength == 0xFFFFFFFF {
			advancePastArithTerminator(d.stream)
		} else {
			d.stream.AlignByte()
			d.stream.AddOffset(2)
		}
	}
	if ri.Flags&0x08 != 0 {
		if _, err := d.budget.checkImage(ri.Width, ri.Height, true); err != nil {
			return d.fail(err)
		}
		segment.colorImage = newColorImage(ri.Width, ri.Height)
		if segment.colorImage == nil {
			return ResultFailure
		}
		paintColorMask(segment.colorImage, segment.Image, 0, 0, foreground)
	}
	if segment.Flags.Type != 36 {
		if err := d.expandPageForRegion(&ri); err != nil {
			return d.fail(err)
		}
		if d.colorPage == nil && !direct {
			rect := pGRD.GetReplaceRect()
			d.page.ComposeFrom(ri.X+rect.Left, ri.Y+rect.Top, segment.Image, composeOpFromRegionFlags(ri.Flags))
		} else if d.colorPage != nil && d.composeColorRegion(segment, &ri) != ResultSuccess {
			return ResultFailure
		}
		segment.Image = nil
		segment.colorImage = nil
		d.pageWritten = true
	}
	return ResultSuccess
}

// GetHuffContextSize 获取通用区域算术解码上下文数量
// 模板0返回65536，模板1返回8192，模板2和3返回1024
// 入参: template 模板号，范围为0至3
// 返回: int 上下文数量
func GetHuffContextSize(template byte) int {
	if template == 0 {
		return 65536
	} else if template == 1 {
		return 8192
	}
	return 1024
}

// parseGenericRefinementRegion 解析通用细化区域段
// 入参: segment 段
// 返回: Result 解析结果
func (d *Document) parseGenericRefinementRegion(segment *Segment) Result {
	var ri RegionInfo
	var flags byte
	if d.ParseRegionInfo(&ri) != ResultSuccess {
		return ResultFailure
	}
	if d.colorPage != nil {
		return ResultFailure
	}
	if val, err := d.stream.Read1Byte(); err != nil {
		return ResultFailure
	} else {
		flags = val
	}
	pGRRD := NewGRRDProc()
	pGRRD.ctx = d.ctx
	pGRRD.budget = d.budget
	pGRRD.GRW = uint32(ri.Width)
	pGRRD.GRH = uint32(ri.Height)
	pGRRD.GRTEMPLATE = (flags & 0x01) != 0
	pGRRD.TPGRON = ((flags >> 1) & 0x01) != 0
	if !pGRRD.GRTEMPLATE {
		for i := 0; i < 4; i++ {
			if val, err := d.stream.Read1Byte(); err != nil {
				return ResultFailure
			} else {
				pGRRD.GRAT[i] = int8(val)
			}
		}
	}
	var pageSubImage *Image
	if segment.ReferredToSegmentCount > 0 {
		var pSeg *Segment
		for _, refNum := range segment.ReferredToSegmentNumbers {
			pSeg = d.FindSegmentByNumber(refNum)
			if pSeg == nil {
				return ResultFailure
			}
			if pSeg.Flags.Type == 4 || pSeg.Flags.Type == 20 || pSeg.Flags.Type == 36 || pSeg.Flags.Type == 40 {
				break
			}
		}
		if pSeg != nil && pSeg.Image != nil {
			pGRRD.GRREFERENCE = pSeg.Image
		} else {
			return ResultFailure
		}
	} else {
		pageSubImage = d.page.SubImage(ri.X, ri.Y, ri.Width, ri.Height)
		pGRRD.GRREFERENCE = pageSubImage
	}
	pGRRD.GRREFERENCEDX = 0
	pGRRD.GRREFERENCEDY = 0
	size := 8192
	if pGRRD.GRTEMPLATE {
		size = 1024
	}
	grContexts := make([]ArithCtx, size)
	arithDecoder := NewArithDecoder(d.stream)
	segment.ResultType = JBig2ImagePointer
	var err error
	segment.Image, err = pGRRD.Decode(arithDecoder, grContexts)
	if err != nil {
		return d.fail(err)
	}
	d.stream.AlignByte()
	d.stream.AddOffset(2)
	if segment.Flags.Type != 40 {
		if err := d.expandPageForRegion(&ri); err != nil {
			return d.fail(err)
		}
		d.page.ComposeFrom(ri.X, ri.Y, segment.Image, composeOpFromRegionFlags(ri.Flags))
		segment.Image = nil
		d.pageWritten = true
	}
	return ResultSuccess
}

// parsePageInfo 解析页面信息段
// 入参: segment 段
// 返回: Result 解析结果
func (d *Document) parsePageInfo(segment *Segment) Result {
	pi := &PageInfo{}
	if val, err := d.stream.ReadInteger(); err != nil {
		return ResultFailure
	} else {
		pi.Width = val
	}
	if val, err := d.stream.ReadInteger(); err != nil {
		return ResultFailure
	} else {
		pi.Height = val
	}
	if val, err := d.stream.ReadInteger(); err != nil {
		return ResultFailure
	} else {
		pi.ResolutionX = val
	}
	if val, err := d.stream.ReadInteger(); err != nil {
		return ResultFailure
	} else {
		pi.ResolutionY = val
	}
	var flags byte
	if val, err := d.stream.Read1Byte(); err != nil {
		return ResultFailure
	} else {
		flags = val
	}
	var striping uint16
	if val, err := d.stream.ReadShortInteger(); err != nil {
		return ResultFailure
	} else {
		striping = val
	}
	pi.DefaultPixelValue = (flags&4) != 0 && (flags&0x80) == 0
	pi.IsStriped = (striping & 0x8000) != 0
	pi.MaxStripeSize = striping & 0x7FFF
	if pi.Width == 0 || pi.Width > 0x7FFFFFFF {
		return ResultFailure
	}
	_ = d.setPageMemory(0)
	height := pi.Height
	if height == 0xFFFFFFFF {
		height = uint32(pi.MaxStripeSize)
	} else if height == 0 || height > 0x7FFFFFFF {
		return ResultFailure
	}
	if height == 0 {
		w := int32(pi.Width)
		d.page = &Image{
			width:  w,
			height: 0,
			stride: (w + 7) / 8,
		}
		d.colorPage = nil
		d.referenceColors = false
		if flags&0x80 != 0 {
			d.colorPage = newZeroHeightColorImage(w)
			if d.colorPage == nil {
				return ResultFailure
			}
		}
	} else {
		if err := d.resizePageMemory(int32(pi.Width), int32(height), flags&0x80 != 0); err != nil {
			return d.fail(err)
		}
		d.page = NewImage(int32(pi.Width), int32(height))
		if d.page == nil {
			return ResultFailure
		}
		d.colorPage = nil
		d.referenceColors = false
		if flags&0x80 != 0 {
			d.colorPage = newColorImage(int32(pi.Width), int32(height))
			if d.colorPage == nil {
				return ResultFailure
			}
		}
		if pi.DefaultPixelValue {
			d.page.Fill(true)
		}
	}
	d.pageInfoList = append(d.pageInfoList, pi)
	d.currentPageAssoc = segment.PageAssociation
	d.inPage = true
	d.pageWritten = false
	return ResultSuccess
}

// parseEndOfStripe 解析条带结束段
// 返回: Result 解析结果
func (d *Document) parseEndOfStripe() Result {
	if !d.inPage {
		return ResultFailure
	}
	line, err := d.stream.ReadInteger()
	if err != nil || line >= 0x7FFFFFFF {
		return ResultFailure
	}
	pi := d.pageInfoList[len(d.pageInfoList)-1]
	if pi.Height != 0xFFFFFFFF {
		return ResultSuccess
	}
	height := int32(line + 1)
	if err := d.resizePageMemory(d.page.Width(), height, d.colorPage != nil); err != nil {
		return d.fail(err)
	}
	d.page.Expand(height, pi.DefaultPixelValue)
	if d.page.Height() < height {
		return ResultFailure
	}
	d.page.height = height
	d.page.data = d.page.data[:d.page.stride*height]
	if d.colorPage != nil {
		if int(height) > d.colorPage.Rect.Dy() {
			grown := newColorImage(d.page.Width(), height)
			if grown == nil {
				return ResultFailure
			}
			copy(grown.Pix, d.colorPage.Pix)
			d.colorPage = grown
		} else {
			d.colorPage.Rect.Max.Y = int(height)
			d.colorPage.Pix = d.colorPage.Pix[:int(height)*d.colorPage.Stride]
		}
	}
	return ResultSuccess
}

// parseTable 解析自定义霍夫曼表段
// 入参: segment 段
// 返回: Result 解析结果
func (d *Document) parseTable(segment *Segment) Result {
	segment.ResultType = JBig2HuffmanTablePointer
	huff := NewTableFromStream(d.stream)
	if !huff.IsOK() {
		return ResultFailure
	}
	segment.HuffmanTable = huff
	d.stream.AlignByte()
	return ResultSuccess
}

// ReleasePageSegments 释放页面段数据
// 移除指定页面的段并清空段中的解码结果，已取得的段引用也会受影响
// 入参: pageNumber 页面编号
func (d *Document) ReleasePageSegments(pageNumber uint32) {
	if d.Grouped {
		for idx := 0; idx < d.groupedIndex && idx < len(d.segmentList); idx++ {
			seg := d.segmentList[idx]
			if seg == nil || seg.released || seg.PageAssociation == 0 || seg.PageAssociation != pageNumber {
				continue
			}
			if d.groupedLastRef != nil {
				if lastIdx, ok := d.groupedLastRef[seg.Number]; ok && d.groupedIndex-1 < lastIdx {
					continue
				}
			}
			d.releaseSegmentData(seg)
			if d.segmentIndex != nil && d.segmentIndex[seg.Number] == seg {
				delete(d.segmentIndex, seg.Number)
			}
		}
		return
	}
	n := 0
	for _, seg := range d.segmentList {
		if seg.PageAssociation != pageNumber || (pageNumber != 0 && seg.Flags.DeferredNonRetain) {
			d.segmentList[n] = seg
			n++
		} else {
			d.releaseSegmentData(seg)
			if d.segmentIndex != nil && d.segmentIndex[seg.Number] == seg {
				delete(d.segmentIndex, seg.Number)
			}
		}
	}
	for i := n; i < len(d.segmentList); i++ {
		d.segmentList[i] = nil
	}
	d.segmentList = d.segmentList[:n]
}
