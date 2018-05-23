// Copyright (c) 2018 Uber Technologies, Inc.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package result

import (
	"fmt"
	"time"

	"github.com/m3db/m3db/src/dbnode/storage/namespace"
	"github.com/m3db/m3ninx/index/segment"
	"github.com/m3db/m3ninx/index/segment/mem"
	xtime "github.com/m3db/m3x/time"
)

// NewDefaultMutableSegmentAllocator returns a default mutable segment
// allocator.
func NewDefaultMutableSegmentAllocator() MutableSegmentAllocator {
	return func() (segment.MutableSegment, error) {
		return mem.NewSegment(0, mem.NewOptions())
	}
}

type indexBootstrapResult struct {
	results     IndexResults
	unfulfilled ShardTimeRanges
}

// NewIndexBootstrapResult returns a new index bootstrap result.
func NewIndexBootstrapResult() IndexBootstrapResult {
	return &indexBootstrapResult{
		results:     make(IndexResults),
		unfulfilled: make(ShardTimeRanges),
	}
}

func (r *indexBootstrapResult) IndexResults() IndexResults {
	return r.results
}

func (r *indexBootstrapResult) Unfulfilled() ShardTimeRanges {
	return r.unfulfilled
}

func (r *indexBootstrapResult) SetUnfulfilled(unfulfilled ShardTimeRanges) {
	r.unfulfilled = unfulfilled
}

func (r *indexBootstrapResult) Add(block IndexBlock, unfulfilled ShardTimeRanges) {
	r.results.Add(block)
	r.unfulfilled.AddRanges(unfulfilled)
}

// Add will add an index block to the collection, merging if one already
// exists.
func (r IndexResults) Add(block IndexBlock) {
	if block.BlockStart().IsZero() {
		return
	}

	// Merge results
	blockStart := xtime.ToUnixNano(block.BlockStart())
	existing, ok := r[blockStart]
	if !ok {
		r[blockStart] = block
		return
	}

	r[blockStart] = existing.Merged(block)

	if len(r[blockStart].segments) != 1 {
		fmt.Printf(
			"expected 1 segment for blockStart: %d, but got %d during merge\n",
			blockStart.ToTime().Unix(),
			len(r[blockStart].segments),
		)
		for _, currSegment := range r[blockStart].segments {
			_, ok := currSegment.(segment.MutableSegment)
			if !ok {
				fmt.Printf("(Add method): mutable segment size: %d\n", currSegment.Size())
			} else {
				fmt.Printf("(Add method): immutable segment size: %d\n", currSegment.Size())
			}
		}
	}
}

// AddResults will add another set of index results to the collection, merging
// if index blocks already exists.
func (r IndexResults) AddResults(other IndexResults) {
	for _, block := range other {
		r.Add(block)
	}
}

// GetOrAddSegment get or create a new mutable segment.
func (r IndexResults) GetOrAddSegment(
	t time.Time,
	idxopts namespace.IndexOptions,
	opts Options,
) (segment.MutableSegment, error) {
	// NB(r): The reason we can align by the retention block size and guarantee
	// there is only one entry for this time is because index blocks must be a
	// positive multiple of the data block size, making it easy to map a data
	// block entry to at most one index block entry.
	blockStart := t.Truncate(idxopts.BlockSize())
	blockStartNanos := xtime.ToUnixNano(blockStart)

	block, exists := r[blockStartNanos]
	if !exists {
		block = NewIndexBlock(blockStart, nil, nil)
		r[blockStartNanos] = block
	}

	foundImmutable := false
	for _, seg := range block.Segments() {
		if mutable, ok := seg.(segment.MutableSegment); ok {
			return mutable, nil
		}
		foundImmutable = true
	}

	if foundImmutable {
		fmt.Printf("encountered immutable segment for blockStart: %d , will have to allocate and merge \n", t.Unix())
		for _, currSegment := range block.Segments() {
			_, ok := currSegment.(segment.MutableSegment)
			if !ok {
				fmt.Printf("(GetOrAddSegment method): mutable segment size: %d\n", currSegment.Size())
			} else {
				fmt.Printf("(GetOrAddSegment method): immutable segment size: %d\n", currSegment.Size())
			}
		}
	}

	alloc := opts.IndexMutableSegmentAllocator()
	mutable, err := alloc()
	if err != nil {
		return nil, err
	}

	segments := []segment.Segment{mutable}
	r[blockStartNanos] = block.Merged(NewIndexBlock(blockStart, segments, nil))
	return mutable, nil
}

// MarkFulfilled will mark an index block as fulfilled, either partially or
// wholly as specified by the shard time ranges passed.
func (r IndexResults) MarkFulfilled(
	t time.Time,
	fulfilled ShardTimeRanges,
	idxopts namespace.IndexOptions,
) error {
	// NB(r): The reason we can align by the retention block size and guarantee
	// there is only one entry for this time is because index blocks must be a
	// positive multiple of the data block size, making it easy to map a data
	// block entry to at most one index block entry.
	blockStart := t.Truncate(idxopts.BlockSize())
	blockStartNanos := xtime.ToUnixNano(blockStart)

	blockRange := xtime.Range{
		Start: blockStart,
		End:   blockStart.Add(idxopts.BlockSize()),
	}

	// First check fulfilled is correct
	min, max := fulfilled.MinMax()
	if min.Before(blockRange.Start) || max.After(blockRange.End) {
		return fmt.Errorf("fulfilled range %s is outside of index block range: %s",
			fulfilled.SummaryString(), blockRange.String())
	}

	block, exists := r[blockStartNanos]
	if !exists {
		block = NewIndexBlock(blockStart, nil, nil)
		r[blockStartNanos] = block
	}
	r[blockStartNanos] = block.Merged(NewIndexBlock(blockStart, nil, fulfilled))
	return nil
}

// MergedIndexBootstrapResult returns a merged result of two bootstrap results.
// It is a mutating function that mutates the larger result by adding the
// smaller result to it and then finally returns the mutated result.
func MergedIndexBootstrapResult(i, j IndexBootstrapResult) IndexBootstrapResult {
	if i == nil {
		return j
	}
	if j == nil {
		return i
	}
	sizeI, sizeJ := 0, 0
	for _, ir := range i.IndexResults() {
		sizeI += len(ir.Segments())
	}
	for _, ir := range j.IndexResults() {
		sizeJ += len(ir.Segments())
	}
	if sizeI >= sizeJ {
		i.IndexResults().AddResults(j.IndexResults())
		i.Unfulfilled().AddRanges(j.Unfulfilled())
		return i
	}
	j.IndexResults().AddResults(i.IndexResults())
	j.Unfulfilled().AddRanges(i.Unfulfilled())
	return j
}

// NewIndexBlock returns a new bootstrap index block result.
func NewIndexBlock(
	blockStart time.Time,
	segments []segment.Segment,
	fulfilled ShardTimeRanges,
) IndexBlock {
	if fulfilled == nil {
		fulfilled = ShardTimeRanges{}
	}
	return IndexBlock{
		blockStart: blockStart,
		segments:   segments,
		fulfilled:  fulfilled,
	}
}

// BlockStart returns the block start.
func (b IndexBlock) BlockStart() time.Time {
	return b.blockStart
}

// Segments returns the segments.
func (b IndexBlock) Segments() []segment.Segment {
	return b.segments
}

// Fulfilled returns the fulfilled time ranges by this index block.
func (b IndexBlock) Fulfilled() ShardTimeRanges {
	return b.fulfilled
}

// Merged returns a new merged index block, currently it just appends the
// list of segments from the other index block and the caller merges
// as they see necessary.
func (b IndexBlock) Merged(other IndexBlock) IndexBlock {
	r := b
	if len(other.segments) > 0 {
		r.segments = append(r.segments, other.segments...)
	}
	if !other.fulfilled.IsEmpty() {
		r.fulfilled = b.fulfilled.Copy()
		r.fulfilled.AddRanges(other.fulfilled)
	}
	return r
}
