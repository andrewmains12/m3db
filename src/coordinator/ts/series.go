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

package ts

import (
	"time"

	"github.com/m3db/m3db/src/coordinator/models"
)

// A Series is the public interface to a block of timeseries values.  Each block has a start time,
// a logical number of steps, and a step size indicating the number of milliseconds represented by each point.
type Series struct {
	name      string
	startTime time.Time
	vals      Values
	Tags      models.Tags
}

// NewSeries creates a new Series at a given start time, backed by the provided values
func NewSeries(name string, startTime time.Time, vals Values, tags models.Tags) *Series {
	return &Series{
		name:      name,
		startTime: startTime,
		vals:      vals,
		Tags:      tags,
	}
}

// StartTime returns the time the block starts
func (b *Series) StartTime() time.Time { return b.startTime }

// Name returns the name of the timeseries block
func (b *Series) Name() string { return b.name }

func (b *Series) Values() Values { return b.vals }

// Len returns the number of values in the time series. Used for aggregation
func (b *Series) Len() int { return b.vals.Len() }

// Values returns the underlying values interface
func (b *Series) Values() Values { return b.vals }
